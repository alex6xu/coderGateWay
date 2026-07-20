package provider

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	mimoFreeDefaultBase = "https://api.xiaomimimo.com"
	mimoFreeBootstrap   = "/api/free-ai/bootstrap"
	mimoFreeChatPath    = "/api/free-ai/openai/chat"
	mimoFreeModel       = "mimo-auto"
	mimoFreeTokenSkew   = 5 * time.Minute
	// mimoFreeUserAgent is pinned to the exact UA observed in the official
	// mimo-code client request log; do NOT change it dynamically.
	mimoFreeUserAgent = "mimocode/local ai-sdk/provider-utils/4.0.23 runtime/bun/1.3.14"

	// mimoFreeMinInterval throttles per-instance request cadence to avoid the
	// free endpoint's "high-frequency" (441) risk_control gate.
	mimoFreeMinInterval = 600 * time.Millisecond
	mimoFreeMaxRetries  = 5
)

// MiMoCodeSystemMarker is required by the free mimo-auto endpoint anti-abuse gate.
const MiMoCodeSystemMarker = "You are MiMoCode, an interactive CLI tool that helps users with software engineering tasks."

// MiMoFreeProvider implements the anonymous free mimo-auto flow from MiMoCode.
type MiMoFreeProvider struct {
	config *ProviderConfig
	client *http.Client

	mu       sync.Mutex
	token    *mimoFreeToken
	lastCall time.Time // throttle: timestamp of the last chat call
	affinity string    // instance-level fallback X-Session-Affinity value
}

type mimoFreeToken struct {
	jwt string
	exp time.Time
}

// NewMiMoFreeProvider creates a new MiMo free provider.
func NewMiMoFreeProvider(config *ProviderConfig) *MiMoFreeProvider {
	return &MiMoFreeProvider{
		config:   config,
		client:   &http.Client{Timeout: 180 * time.Second},
		affinity: generateSessionAffinity(),
	}
}

func (p *MiMoFreeProvider) Name() string {
	return p.config.Name
}

func (p *MiMoFreeProvider) baseURL() string {
	raw := mimoFreeDefaultBase
	if p.config.BaseURL != "" {
		raw = p.config.BaseURL
	} else if v := os.Getenv("MIMO_FREE_BASE_URL"); v != "" {
		raw = v
	}
	return normalizeMiMoFreeBaseURL(raw)
}

func normalizeMiMoFreeBaseURL(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	base = strings.TrimSuffix(base, "/v1")
	return base
}

func (p *MiMoFreeProvider) chatURL() string {
	return p.baseURL() + mimoFreeChatPath
}

func (p *MiMoFreeProvider) bootstrapURL() string {
	return p.baseURL() + mimoFreeBootstrap
}

func (p *MiMoFreeProvider) fingerprintPath() string {
	if v := os.Getenv("MIMO_FREE_FINGERPRINT_PATH"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("./data", "mimo-free-client")
	}
	return filepath.Join(home, ".local", "share", "mimocode", "mimo-free-client")
}

func (p *MiMoFreeProvider) fingerprint() (string, error) {
	path := p.fingerprintPath()
	if data, err := os.ReadFile(path); err == nil {
		if fp := strings.TrimSpace(string(data)); fp != "" {
			return fp, nil
		}
	}

	hostname, _ := os.Hostname()
	cpuModel := "unknown-cpu"
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output(); err == nil {
			if v := strings.TrimSpace(string(out)); v != "" {
				cpuModel = v
			}
		}
	} else if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "model name") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					cpuModel = strings.TrimSpace(parts[1])
					break
				}
			}
		}
	}

	platform := runtime.GOOS
	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		arch = "x64"
	case "386":
		arch = "x86"
	}

	username := "unknown-user"
	if u, err := user.Current(); err == nil && u.Username != "" {
		username = u.Username
	}

	payload := strings.Join([]string{hostname, platform, arch, cpuModel, username}, "|")
	sum := sha256.Sum256([]byte(payload))
	fp := hex.EncodeToString(sum[:])

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err == nil {
		_ = os.WriteFile(path, []byte(fp), 0o600)
	}

	return fp, nil
}

func jwtExpiry(jwt string) time.Time {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return time.Now().Add(50 * time.Minute)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Now().Add(50 * time.Minute)
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		return time.Now().Add(50 * time.Minute)
	}
	return time.Unix(claims.Exp, 0)
}

func (p *MiMoFreeProvider) bootstrap(ctx context.Context) (*mimoFreeToken, error) {
	fp, err := p.fingerprint()
	if err != nil {
		return nil, fmt.Errorf("fingerprint: %w", err)
	}

	body, err := json.Marshal(map[string]string{"client": fp})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.bootstrapURL(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bootstrap failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	log.Printf("[mimo-free] bootstrap ok url=%s fingerprint=%s…%s", p.bootstrapURL(), fp[:8], fp[len(fp)-4:])

	var result struct {
		JWT string `json:"jwt"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}
	if result.JWT == "" {
		return nil, fmt.Errorf("bootstrap response missing jwt")
	}

	return &mimoFreeToken{jwt: result.JWT, exp: jwtExpiry(result.JWT)}, nil
}

func (p *MiMoFreeProvider) getJWT(ctx context.Context, force bool) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !force && p.token != nil && time.Until(p.token.exp) > mimoFreeTokenSkew {
		return p.token.jwt, nil
	}

	token, err := p.bootstrap(ctx)
	if err != nil {
		return "", err
	}
	p.token = token
	return token.jwt, nil
}

func (p *MiMoFreeProvider) prepareRequest(req *ChatCompletionRequest) *ChatCompletionRequest {
	out := *req
	out.Model = mimoFreeModel
	out.Messages = injectMiMoCodeSystemMarker(req.Messages)
	return &out
}

func injectMiMoCodeSystemMarker(messages []Message) []Message {
	marker := MiMoCodeSystemMarker
	for _, msg := range messages {
		if msg.Role == "system" && strings.Contains(msg.Content, "You are MiMoCode") {
			return messages
		}
	}

	out := make([]Message, 0, len(messages)+1)
	for i, msg := range messages {
		if msg.Role == "system" {
			merged := msg
			if !strings.Contains(merged.Content, marker) {
				merged.Content = marker + "\n\n" + merged.Content
			}
			out = append(out, merged)
			out = append(out, messages[i+1:]...)
			return out
		}
	}

	out = append(out, Message{Role: "system", Content: marker})
	out = append(out, messages...)
	return out
}

func (p *MiMoFreeProvider) doChat(ctx context.Context, req *ChatCompletionRequest, stream bool) (*http.Response, error) {
	prepared := p.prepareRequest(req)
	prepared.Stream = stream

	body, err := json.Marshal(prepared)
	if err != nil {
		return nil, err
	}
	affinity := p.affinityFor(req)

	jwt, err := p.getJWT(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get mimo-auto token: %w", err)
	}

	for attempt := 0; attempt < mimoFreeMaxRetries; attempt++ {
		if attempt > 0 {
			// refresh token on retries (covers both auth expiry and risk_control)
			if jwt, err = p.getJWT(ctx, true); err != nil {
				return nil, fmt.Errorf("failed to refresh mimo-auto token: %w", err)
			}
		}

		p.throttle()
		resp, err := p.sendChat(ctx, body, jwt, affinity)
		if err != nil {
			return nil, err
		}

		// Auth failure: refresh token and retry immediately (no backoff).
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			resp.Body.Close()
			continue
		}

		// Risk control / rate limit: exponential backoff then retry.
		if isRiskControl(resp) {
			if attempt == mimoFreeMaxRetries-1 {
				return resp, nil // let caller surface the risk_control body
			}
			resp.Body.Close()
			delay := time.Duration(1<<uint(attempt)) * time.Second
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			continue
		}

		return resp, nil
	}

	return nil, fmt.Errorf("mimo-auto request rejected (risk_control/rate limit) after %d attempts", mimoFreeMaxRetries)
}

func (p *MiMoFreeProvider) sendChat(ctx context.Context, body []byte, jwt, affinity string) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.chatURL(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+jwt)
	httpReq.Header.Set("X-Mimo-Source", "mimocode-cli-free")
	if affinity != "" {
		httpReq.Header.Set("X-Session-Affinity", affinity)
	}
	httpReq.Header.Set("User-Agent", mimoFreeUserAgent)
	if strings.Contains(string(body), `"stream":true`) {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	log.Printf("[mimo-free] chat request url=%s stream=%v", p.chatURL(), strings.Contains(string(body), `"stream":true`))
	return p.client.Do(httpReq)
}

func (p *MiMoFreeProvider) ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	resp, err := p.doChat(ctx, req, false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mimo-auto API error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}
	log.Printf("[mimo-free] chat ok status=%d bytes=%d", resp.StatusCode, len(bodyBytes))

	var result ChatCompletionResponse
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

func (p *MiMoFreeProvider) ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan *ChatCompletionChunk, error) {
	resp, err := p.doChat(ctx, req, true)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("mimo-auto API error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	chunks := make(chan *ChatCompletionChunk, 64)
	go func() {
		defer resp.Body.Close()
		defer close(chunks)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return
			}
			var chunk ChatCompletionChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			select {
			case chunks <- &chunk:
			case <-ctx.Done():
				return
			}
		}
	}()

	return chunks, nil
}

func (p *MiMoFreeProvider) ListModels(ctx context.Context) ([]string, error) {
	return MiMoFreeAdvertisedModels(), nil
}

func (p *MiMoFreeProvider) ValidateModel(model string) bool {
	return IsMiMoAutoModel(model)
}

// MiMoFreeAdvertisedModels returns model IDs exposed via OpenAI-compatible /v1/models.
func MiMoFreeAdvertisedModels() []string {
	return []string{
		"mimo-auto",
		"mimo/mimo-auto",
		"mimo-free",
		"gpt-4o",
		"gpt-4o-mini",
		"gpt-4",
		"gpt-4-turbo",
		"gpt-3.5-turbo",
		"o1",
		"o1-mini",
		"o3-mini",
	}
}

// NormalizeModelAlias normalizes model names for channel matching.
func NormalizeModelAlias(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if strings.HasPrefix(m, "mimo/") {
		return strings.TrimPrefix(m, "mimo/")
	}
	return m
}

// IsMiMoAutoModel reports whether a model name should route to mimo-auto.
func IsMiMoAutoModel(model string) bool {
	switch NormalizeModelAlias(model) {
	case "mimo-auto", "mimo-free":
		return true
	default:
		return false
	}
}

// NormalizeModelForMiMoAuto maps OpenAI-compatible model names to mimo-auto.
func NormalizeModelForMiMoAuto(model string) string {
	return mimoFreeModel
}
