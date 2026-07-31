package claudeoauth

import (
	"testing"
)

func TestGeneratePKCE(t *testing.T) {
	v, c, err := generatePKCE()
	if err != nil {
		t.Fatal(err)
	}
	if v == "" || c == "" || v == c {
		t.Fatalf("unexpected pkce verifier=%q challenge=%q", v, c)
	}
}

func TestNormalizePastCode(t *testing.T) {
	code, state := normalizePastCode("abc#def", "")
	if code != "abc" || state != "def" {
		t.Fatalf("got %q %q", code, state)
	}
	code, state = normalizePastCode("onlycode", "st")
	if code != "onlycode" || state != "st" {
		t.Fatalf("got %q %q", code, state)
	}
}
