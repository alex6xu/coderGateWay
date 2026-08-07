package provider

import (
	"net/url"
	"strings"
)

// NormalizeOpenAICompatibleBaseURL ensures an OpenAI-compatible channel base URL
// ends with an API version segment so callers can append "/chat/completions".
//
// Common misconfig: "https://proxy.example.com" (OpenResty then returns HTML 404
// for "/chat/completions"). Correct form: "https://proxy.example.com/v1".
func NormalizeOpenAICompatibleBaseURL(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return "https://api.openai.com/v1"
	}
	u = strings.TrimRight(u, "/")

	lower := strings.ToLower(u)
	if strings.HasSuffix(lower, "/chat/completions") {
		u = strings.TrimSuffix(u, "/chat/completions")
		u = strings.TrimSuffix(u, "/Chat/Completions")
		u = strings.TrimRight(u, "/")
		lower = strings.ToLower(u)
	}

	versionSuffixes := []string{
		"/v1",
		"/v1beta",
		"/v2",
		"/v3",
		"/v4",
		"/openai/v1",
		"/api/paas/v4",
		"/api/v3",
	}
	for _, suf := range versionSuffixes {
		if strings.HasSuffix(lower, suf) {
			return u
		}
	}

	// If the URL already has a non-root path that looks intentional (e.g. Azure
	// deployments), do not force /v1.
	if parsed, err := url.Parse(u); err == nil && parsed.Host != "" {
		path := strings.Trim(parsed.Path, "/")
		if path != "" && !looksLikeBareAPIHostPath(path) {
			return u
		}
	}

	return u + "/v1"
}

func looksLikeBareAPIHostPath(path string) bool {
	p := strings.ToLower(strings.Trim(path, "/"))
	return p == "" || p == "api" || p == "openai" || p == "v1" || p == "proxy"
}

// JoinOpenAIURL joins a normalized OpenAI-compatible base with a relative path
// such as "chat/completions" or "models".
func JoinOpenAIURL(base, rel string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	rel = strings.TrimLeft(strings.TrimSpace(rel), "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	if rel == "" {
		return base
	}
	return base + "/" + rel
}
