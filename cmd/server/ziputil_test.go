package server

import "testing"

func TestZipPathHasHiddenSegment(t *testing.T) {
	cases := map[string]bool{
		"proj/src/a.ts":     false,
		"proj/.git/config":  true,
		"proj/.env":         true,
		".hidden/file.txt":  true,
		"proj/node/.cache/x": true,
		"__MACOSX/foo":      false, // filtered separately
	}
	for path, want := range cases {
		if got := zipPathHasHiddenSegment(path); got != want {
			t.Fatalf("%s: got %v want %v", path, got, want)
		}
	}
}
