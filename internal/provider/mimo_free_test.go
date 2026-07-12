package provider

import (
	"strings"
	"testing"
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
