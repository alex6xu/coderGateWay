package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/alex/codegateway/internal/model"
	"github.com/alex/codegateway/internal/provider"
)

func TestBackoffExponentialAndCap(t *testing.T) {
	cases := []struct {
		fails int
		want  time.Duration
	}{
		{0, 1 * time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{6, 64 * time.Second},
		{7, backoffCap},   // 128s > cap(120s)
		{8, backoffCap},   // 256s > cap
		{100, backoffCap}, // overflow guard
		{-1, 1 * time.Second},
	}
	for _, tc := range cases {
		if got := backoff(tc.fails); got != tc.want {
			t.Errorf("backoff(%d)=%v want %v", tc.fails, got, tc.want)
		}
	}
}

func provErr(status int, header http.Header) error {
	return provider.NewProviderError(status, header, []byte("boom"))
}

func TestClassifyError(t *testing.T) {
	// 429 → retryable + short cooldown (backoff, no hint)
	c := classifyError(provErr(429, nil), 0)
	if !c.retryable || c.cooldown != backoff(0) {
		t.Errorf("429: got %+v", c)
	}

	// 429 with Retry-After hint → hint wins over backoff
	h := http.Header{}
	h.Set("Retry-After", "5")
	c = classifyError(provErr(429, h), 3)
	if !c.retryable || c.cooldown != 5*time.Second {
		t.Errorf("429+Retry-After: got %+v want cooldown=5s", c)
	}

	// 503 → retryable + backoff
	c = classifyError(provErr(503, nil), 1)
	if !c.retryable || c.cooldown != backoff(1) {
		t.Errorf("503: got %+v", c)
	}

	// 401 → retryable + long config cooldown
	c = classifyError(provErr(401, nil), 0)
	if !c.retryable || c.cooldown != configErrCooldown {
		t.Errorf("401: got %+v want cooldown=%v", c, configErrCooldown)
	}

	// 400 → NOT retryable
	c = classifyError(provErr(400, nil), 0)
	if c.retryable {
		t.Errorf("400: expected non-retryable, got %+v", c)
	}

	// context.Canceled → not retryable, no cooldown
	c = classifyError(context.Canceled, 0)
	if c.retryable || c.cooldown != 0 {
		t.Errorf("canceled: got %+v", c)
	}

	// generic non-provider error → retryable + backoff
	c = classifyError(errors.New("dial tcp: connection refused"), 2)
	if !c.retryable || c.cooldown != backoff(2) {
		t.Errorf("generic: got %+v", c)
	}
}

func TestRetryAfterParsing(t *testing.T) {
	// delta-seconds
	h := http.Header{}
	h.Set("Retry-After", "12")
	pe := provider.NewProviderError(429, h, nil)
	d, ok := pe.RetryAfter()
	if !ok || d != 12*time.Second {
		t.Errorf("delta-seconds: got %v,%v", d, ok)
	}

	// X-RateLimit-Reset fallback
	h2 := http.Header{}
	h2.Set("X-RateLimit-Reset", "7")
	pe2 := provider.NewProviderError(429, h2, nil)
	d, ok = pe2.RetryAfter()
	if !ok || d != 7*time.Second {
		t.Errorf("x-ratelimit-reset: got %v,%v", d, ok)
	}

	// no hint
	pe3 := provider.NewProviderError(429, http.Header{}, nil)
	if _, ok := pe3.RetryAfter(); ok {
		t.Errorf("no hint: expected ok=false")
	}
}

// fakeProvider returns a scripted error/success sequence.
type fakeProvider struct {
	name string
	err  error
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) ChatCompletion(ctx context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &provider.ChatCompletionResponse{ID: "ok-" + f.name}, nil
}
func (f *fakeProvider) ChatCompletionStream(ctx context.Context, req *provider.ChatCompletionRequest) (<-chan *provider.ChatCompletionChunk, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeProvider) ListModels(ctx context.Context) ([]string, error) { return nil, nil }
func (f *fakeProvider) ValidateModel(m string) bool                      { return true }

func cand(id int64, name string) chatCompletionCandidate {
	return chatCompletionCandidate{model: "m", channel: &model.Channel{ID: id, Name: name}}
}

func resetBreakers() {
	reg := breakers()
	reg.mu.Lock()
	reg.breakers = map[int64]*channelBreaker{}
	reg.mu.Unlock()
}

func TestFailoverSwitchesOnError(t *testing.T) {
	resetBreakers()
	provs := map[int64]provider.Provider{
		1: &fakeProvider{name: "a", err: provErr(429, nil)},
		2: &fakeProvider{name: "b"}, // succeeds
	}
	np := func(ch *model.Channel) (provider.Provider, error) { return provs[ch.ID], nil }

	resp, sel, err := completeWithCandidates(context.Background(), []chatCompletionCandidate{cand(1, "a"), cand(2, "b")}, &provider.ChatCompletionRequest{}, np)
	if err != nil {
		t.Fatalf("expected success via failover, got %v", err)
	}
	if sel.channel.ID != 2 || resp.ID != "ok-b" {
		t.Errorf("expected channel 2 to serve, got %+v resp=%v", sel, resp.ID)
	}
	// Channel 1 should now be cooling down.
	if !breakers().isCoolingDown(1, time.Now()) {
		t.Errorf("channel 1 should be cooling down after 429")
	}
}

func TestFailoverStopsOn400(t *testing.T) {
	resetBreakers()
	tried := 0
	np := func(ch *model.Channel) (provider.Provider, error) {
		tried++
		return &fakeProvider{name: fmt.Sprintf("c%d", ch.ID), err: provErr(400, nil)}, nil
	}
	_, _, err := completeWithCandidates(context.Background(), []chatCompletionCandidate{cand(1, "a"), cand(2, "b")}, &provider.ChatCompletionRequest{}, np)
	if err == nil {
		t.Fatal("expected error on 400")
	}
	if tried != 1 {
		t.Errorf("400 must not fail over; provider built %d times, want 1", tried)
	}
}

func TestFailoverSkipsCoolingChannel(t *testing.T) {
	resetBreakers()
	// Pre-cool channel 1.
	breakers().reportFailure(1, time.Hour)

	served := int64(0)
	np := func(ch *model.Channel) (provider.Provider, error) {
		served = ch.ID
		return &fakeProvider{name: "x"}, nil
	}
	_, sel, err := completeWithCandidates(context.Background(), []chatCompletionCandidate{cand(1, "a"), cand(2, "b")}, &provider.ChatCompletionRequest{}, np)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sel.channel.ID != 2 || served != 2 {
		t.Errorf("expected cooling channel 1 skipped, channel 2 served; got sel=%d served=%d", sel.channel.ID, served)
	}
}

func TestFailoverWaitsOnceWhenAllCooled(t *testing.T) {
	resetBreakers()
	// Both channels cool down for a short, within-budget duration.
	breakers().reportFailure(1, 60*time.Millisecond)
	breakers().reportFailure(2, 40*time.Millisecond)

	np := func(ch *model.Channel) (provider.Provider, error) {
		return &fakeProvider{name: "ok"}, nil
	}
	start := time.Now()
	_, sel, err := completeWithCandidates(context.Background(), []chatCompletionCandidate{cand(1, "a"), cand(2, "b")}, &provider.ChatCompletionRequest{}, np)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("expected success after single wait, got %v", err)
	}
	if sel.channel.ID != 2 {
		t.Errorf("expected nearest-cooldown channel 2 to serve after wait, got %d", sel.channel.ID)
	}
	if elapsed < 40*time.Millisecond {
		t.Errorf("expected to wait for cooldown (~40ms), only waited %v", elapsed)
	}
}
