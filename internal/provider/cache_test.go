package provider

import "testing"

func TestApplyPromptCache(t *testing.T) {
	req := &ChatCompletionRequest{
		Messages: []Message{
			{Role: "system", Content: "sys"},
			{Role: "system", Content: "checkpoint"},
			{Role: "user", Content: "hi"},
		},
	}
	ApplyPromptCache(req, "sess-1")
	if req.PromptCacheKey != "sess-1" || !req.EnablePromptCache {
		t.Fatal("cache key not set")
	}
	if req.Messages[1].CacheControl == nil || req.Messages[1].CacheControl.Type != "ephemeral" {
		t.Fatal("expected cache_control on last system message")
	}
	if req.Messages[2].CacheControl != nil {
		t.Fatal("user message should not be marked")
	}
}

func TestUsageNormalizeAndAdd(t *testing.T) {
	u := Usage{PromptTokens: 10, CompletionTokens: 5, PromptTokensDetails: &PromptTokenDetails{CachedTokens: 7}}
	u.Normalize()
	if u.CachedTokens != 7 || u.TotalTokens != 15 {
		t.Fatalf("normalize failed: %+v", u)
	}
	var sum Usage
	sum.Add(u)
	sum.Add(Usage{PromptTokens: 3, CompletionTokens: 2, CachedTokens: 1})
	if sum.CachedTokens != 8 || sum.TotalTokens != 20 {
		t.Fatalf("add failed: %+v", sum)
	}
}
