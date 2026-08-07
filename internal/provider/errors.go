package provider

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ProviderError is a structured error returned by providers on non-2xx HTTP
// responses. It preserves the status code and response headers so upstream
// failover logic can classify the error and honor Retry-After hints.
type ProviderError struct {
	StatusCode int
	Header     http.Header
	Body       string
	URL        string
}

func (e *ProviderError) Error() string {
	if e == nil {
		return "provider error"
	}
	body := strings.TrimSpace(e.Body)
	lower := strings.ToLower(body)
	if strings.Contains(lower, "<html") || strings.Contains(lower, "openresty") {
		hint := "upstream returned HTML 404/error page — check channel Base URL (OpenAI-compatible APIs usually need .../v1, not the bare host)"
		if e.URL != "" {
			return fmt.Sprintf("API error (status %d) url=%s: %s", e.StatusCode, e.URL, hint)
		}
		return fmt.Sprintf("API error (status %d): %s", e.StatusCode, hint)
	}
	if len(body) > 500 {
		body = body[:500] + "…"
	}
	if e.URL != "" {
		return fmt.Sprintf("API error (status %d) url=%s: %s", e.StatusCode, e.URL, body)
	}
	return fmt.Sprintf("API error (status %d): %s", e.StatusCode, body)
}

// NewProviderError builds a ProviderError from an HTTP response's status,
// headers, and already-read body bytes.
func NewProviderError(statusCode int, header http.Header, body []byte) *ProviderError {
	return &ProviderError{
		StatusCode: statusCode,
		Header:     header,
		Body:       string(body),
	}
}

func wrapUpstreamError(requestURL string, statusCode int, header http.Header, body []byte) *ProviderError {
	err := NewProviderError(statusCode, header, body)
	err.URL = requestURL
	return err
}

// RetryAfter returns the upstream-suggested cooldown parsed from the
// Retry-After or X-RateLimit-Reset headers, and whether such a hint existed.
// Retry-After may be either delta-seconds or an HTTP-date; X-RateLimit-Reset
// is treated as delta-seconds.
func (e *ProviderError) RetryAfter() (time.Duration, bool) {
	if e == nil || e.Header == nil {
		return 0, false
	}
	if v := strings.TrimSpace(e.Header.Get("Retry-After")); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second, true
		}
		if t, err := http.ParseTime(v); err == nil {
			if d := time.Until(t); d > 0 {
				return d, true
			}
			return 0, true
		}
	}
	if v := strings.TrimSpace(e.Header.Get("X-RateLimit-Reset")); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second, true
		}
	}
	return 0, false
}
