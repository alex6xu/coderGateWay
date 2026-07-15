package server

import (
	"testing"

	"github.com/alex/codegateway/internal/model"
	"github.com/gin-gonic/gin"
)

func TestParseModelsJSON(t *testing.T) {
	got := parseModelsJSON(`["gpt-4o"," mimo-auto "]`)
	if len(got) != 2 || got[0] != "gpt-4o" || got[1] != "mimo-auto" {
		t.Fatalf("unexpected parse result: %#v", got)
	}

	got = parseModelsJSON("glm-5.2")
	if len(got) != 1 || got[0] != "glm-5.2" {
		t.Fatalf("unexpected single model parse: %#v", got)
	}

	if parseModelsJSON("") != nil {
		t.Fatal("expected nil for empty models")
	}
}

func TestOwnedByForChannelType(t *testing.T) {
	cases := map[int]string{
		model.ChannelTypeOpenAI:   "openai",
		model.ChannelTypeClaude:   "anthropic",
		model.ChannelTypeGemini:   "google",
		model.ChannelTypeDeepSeek: "deepseek",
		model.ChannelTypeMiMoFree: "mimo",
		model.ChannelTypeCustom:   "custom",
		999:                       "codegateway",
	}
	for chType, want := range cases {
		if got := ownedByForChannelType(chType); got != want {
			t.Fatalf("type %d: got %s want %s", chType, got, want)
		}
	}
}

func TestOpenAIAPIErrorShape(t *testing.T) {
	err := openaiAPIError("The model 'x' does not exist", "invalid_request_error", "model", "model_not_found")
	raw, ok := err["error"].(gin.H)
	if !ok {
		t.Fatalf("error field type: %T", err["error"])
	}
	if raw["code"] != "model_not_found" || raw["param"] != "model" || raw["type"] != "invalid_request_error" {
		t.Fatalf("unexpected error payload: %#v", raw)
	}
}

func TestSupportsUpstreamModelList(t *testing.T) {
	if !supportsUpstreamModelList(model.ChannelTypeOpenAI) {
		t.Fatal("openai should support upstream model list")
	}
	if supportsUpstreamModelList(model.ChannelTypeClaude) {
		t.Fatal("claude should not use openai-style upstream model list by default")
	}
}
