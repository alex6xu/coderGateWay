package provider

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestInjectMiMoCodeSystemMarkerAddsMarker(t *testing.T) {
	messages := []Message{{Role: "user", Content: "hi"}}
	out := injectMiMoCodeSystemMarker(messages)
	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out))
	}
	if out[0].Role != "system" || !strings.Contains(out[0].Content, "You are MiMoCode") {
		t.Fatalf("expected leading system marker, got %+v", out[0])
	}
}

func TestInjectMiMoCodeSystemMarkerIdempotent(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: MiMoCodeSystemMarker},
		{Role: "user", Content: "hi"},
	}
	out := injectMiMoCodeSystemMarker(messages)
	if len(out) != 2 {
		t.Fatalf("expected unchanged messages, got %d", len(out))
	}
}

func TestInjectMiMoCodeSystemMarkerMergesExistingSystem(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "Extra rules"},
		{Role: "user", Content: "hi"},
	}
	out := injectMiMoCodeSystemMarker(messages)
	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out))
	}
	if !strings.Contains(out[0].Content, "You are MiMoCode") || !strings.Contains(out[0].Content, "Extra rules") {
		t.Fatalf("expected merged system prompt, got %q", out[0].Content)
	}
}

func TestNormalizeMiMoFreeBaseURL(t *testing.T) {
	if got := normalizeMiMoFreeBaseURL("https://api.xiaomimimo.com/v1/"); got != "https://api.xiaomimimo.com" {
		t.Fatalf("unexpected base url: %s", got)
	}
}

func TestIsRiskControl(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       bool
	}{
		{"429 status", http.StatusTooManyRequests, "{}", true},
		{"441 in body", http.StatusBadRequest, `{"error":{"code":"441","message":"risk_control"}}`, true},
		{"risk_control keyword", http.StatusBadRequest, `{"error":{"type":"risk_control"}}`, true},
		{"429 in body", http.StatusBadRequest, `{"error":{"code":"429"}}`, true},
		{"normal 400", http.StatusBadRequest, `{"error":{"message":"bad request"}}`, false},
		{"200 ok", http.StatusOK, `{"ok":true}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tt.statusCode,
				Body:       io.NopCloser(strings.NewReader(tt.body)),
			}
			if got := isRiskControl(resp); got != tt.want {
				t.Fatalf("isRiskControl() = %v, want %v", got, tt.want)
			}
			// Verify body is still readable after check.
			b, _ := io.ReadAll(resp.Body)
			if string(b) != tt.body {
				t.Fatalf("body lost after isRiskControl: got %q", string(b))
			}
		})
	}
}

func TestRotateFingerprint(t *testing.T) {
	dir := t.TempDir()
	fpPath := filepath.Join(dir, "mimo-free-client")

	// Write a fake fingerprint.
	if err := os.WriteFile(fpPath, []byte("old-fingerprint"), 0o600); err != nil {
		t.Fatal(err)
	}

	p := &MiMoFreeProvider{
		config:          &ProviderConfig{Name: "test"},
		client:          &http.Client{},
		riskControlHits: 5,
	}
	// Override fingerprint path via env.
	t.Setenv("MIMO_FREE_FINGERPRINT_PATH", fpPath)

	// Set a fake token to verify it gets cleared.
	p.token = &mimoFreeToken{jwt: "old-jwt", exp: time.Now().Add(time.Hour)}

	p.rotateFingerprint()

	if _, err := os.Stat(fpPath); !os.IsNotExist(err) {
		t.Fatalf("fingerprint file should have been deleted")
	}
	if p.token != nil {
		t.Fatalf("token should be nil after rotation")
	}
	if p.riskControlHits != 0 {
		t.Fatalf("riskControlHits should be 0 after rotation, got %d", p.riskControlHits)
	}
}

func TestFingerprintIncludesSalt(t *testing.T) {
	dir := t.TempDir()
	fp1 := filepath.Join(dir, "fp1")
	fp2 := filepath.Join(dir, "fp2")

	p := &MiMoFreeProvider{
		config: &ProviderConfig{Name: "test"},
		client: &http.Client{},
	}

	t.Setenv("MIMO_FREE_FINGERPRINT_PATH", fp1)
	f1, err := p.fingerprint()
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("MIMO_FREE_FINGERPRINT_PATH", fp2)
	f2, err := p.fingerprint()
	if err != nil {
		t.Fatal(err)
	}

	if f1 == f2 {
		t.Fatalf("fingerprints should differ due to random salt, both got %s", f1)
	}
}

func TestSingletonRegistry(t *testing.T) {
	// Clear any existing singletons.
	singletonMu.Lock()
	singletonProviders = make(map[string]Provider)
	singletonMu.Unlock()

	cfg := &ProviderConfig{
		Name:    "test-singleton",
		Type:    ProviderTypeMiMoFree,
		BaseURL: "https://example.com",
	}

	p1, err := GetOrCreateSingleton(cfg)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := GetOrCreateSingleton(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if p1 != p2 {
		t.Fatalf("GetOrCreateSingleton should return the same instance")
	}
}

func TestGlobalLockSerializes(t *testing.T) {
	// Verify that the global lock prevents concurrent execution.
	var mu sync.Mutex
	order := []int{}

	// Simulate two goroutines competing for the lock.
	var wg sync.WaitGroup
	wg.Add(2)

	mimoFreeGlobalLock.Lock() // hold the lock to queue both goroutines

	go func() {
		defer wg.Done()
		mimoFreeGlobalLock.Lock()
		defer mimoFreeGlobalLock.Unlock()
		mu.Lock()
		order = append(order, 1)
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}()

	go func() {
		defer wg.Done()
		mimoFreeGlobalLock.Lock()
		defer mimoFreeGlobalLock.Unlock()
		mu.Lock()
		order = append(order, 2)
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}()

	time.Sleep(20 * time.Millisecond) // let goroutines queue
	mimoFreeGlobalLock.Unlock()       // release — one goroutine runs
	wg.Wait()

	if len(order) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(order))
	}
	// They must be serialized: [1,2] or [2,1], never interleaved.
	if order[0] == order[1] {
		t.Fatalf("goroutines ran concurrently: %v", order)
	}
}

func TestThrottleWithJitter(t *testing.T) {
	p := &MiMoFreeProvider{
		config: &ProviderConfig{Name: "test"},
		client: &http.Client{},
	}

	// First call should not sleep.
	start := time.Now()
	p.throttle()
	elapsed := time.Since(start)
	if elapsed > 100*time.Millisecond {
		t.Fatalf("first throttle should be instant, took %v", elapsed)
	}

	// Second call should sleep at least mimoFreeMinInterval.
	start = time.Now()
	p.throttle()
	elapsed = time.Since(start)
	if elapsed < mimoFreeMinInterval {
		t.Fatalf("second throttle should sleep >= %v, took %v", mimoFreeMinInterval, elapsed)
	}
	// Upper bound: min + jitter + some slack.
	maxExpected := mimoFreeMinInterval + mimoFreeJitterRange + 200*time.Millisecond
	if elapsed > maxExpected {
		t.Fatalf("second throttle took too long: %v (max expected %v)", elapsed, maxExpected)
	}
}

func TestAffinityFor(t *testing.T) {
	p := &MiMoFreeProvider{
		config:   &ProviderConfig{Name: "test"},
		client:   &http.Client{},
		affinity: "ses_instance",
	}

	// With session ID → derived affinity.
	req1 := &ChatCompletionRequest{SessionID: "sess-123"}
	a1 := p.affinityFor(req1)
	if !strings.HasPrefix(a1, "ses_") {
		t.Fatalf("expected ses_ prefix, got %s", a1)
	}
	if a1 == "ses_instance" {
		t.Fatalf("should derive from session ID, not use instance affinity")
	}

	// Same session → same affinity.
	a2 := p.affinityFor(req1)
	if a1 != a2 {
		t.Fatalf("same session should produce same affinity: %s vs %s", a1, a2)
	}

	// No session → instance affinity.
	req2 := &ChatCompletionRequest{}
	a3 := p.affinityFor(req2)
	if a3 != "ses_instance" {
		t.Fatalf("expected instance affinity, got %s", a3)
	}
}

func TestIsRiskControlPreservesBodyForRead(t *testing.T) {
	body := `{"error":{"code":"441","message":"Detected high-frequency non-compliant requests"}}`
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(bytes.NewReader([]byte(body))),
	}
	if !isRiskControl(resp) {
		t.Fatal("expected risk_control detection")
	}
	// Body should still be readable by the caller.
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if string(b) != body {
		t.Fatalf("body mismatch: got %q", string(b))
	}
}
