package memory

import "testing"

func TestSanitizeFTSQuery(t *testing.T) {
	q := SanitizeFTSQuery(`login!!! "drop" rate-limit 限流`)
	if q == "" {
		t.Fatal("expected non-empty query")
	}
	if SanitizeFTSQuery("!!!") != "" {
		t.Fatal("expected empty for punctuation-only")
	}
}
