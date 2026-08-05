package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListStableOrder(t *testing.T) {
	r := NewChrootedRegistry(t.TempDir())
	names1 := make([]string, 0)
	for _, tool := range r.List() {
		names1 = append(names1, tool.Name)
	}
	names2 := make([]string, 0)
	for _, tool := range r.List() {
		names2 = append(names2, tool.Name)
	}
	if len(names1) < 2 {
		t.Fatal("expected tools")
	}
	for i := range names1 {
		if names1[i] != names2[i] {
			t.Fatalf("unstable order: %v vs %v", names1, names2)
		}
		if i > 0 && names1[i-1] > names1[i] {
			t.Fatalf("not sorted: %v", names1)
		}
	}
}

func TestIsReadOnly(t *testing.T) {
	if !IsReadOnly("read_file") || IsReadOnly("write_file") || IsReadOnly("bash") {
		t.Fatal("readonly classification wrong")
	}
}

func TestReadFileDefaultWindowAndByteCap(t *testing.T) {
	dir := t.TempDir()
	var lines []string
	for i := 0; i < 600; i++ {
		lines = append(lines, strings.Repeat("x", 80))
	}
	path := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatal(err)
	}

	r := NewChrootedRegistry(dir, ToolLimits{
		ReadFileDefaultLines: 50,
		ReadFileMaxBytes:     2048,
		GrepMaxBytes:         1024,
	})
	tl, err := r.Get("read_file")
	if err != nil {
		t.Fatal(err)
	}
	out, err := tl.Handler(context.Background(), map[string]interface{}{"path": "big.txt"})
	if err != nil {
		t.Fatal(err)
	}
	bodyLines := strings.Count(out, "\n")
	if bodyLines > 60 {
		t.Fatalf("expected default line window ~50, got ~%d lines in output", bodyLines)
	}
	if !strings.Contains(out, "more lines") && !strings.Contains(out, "truncated") {
		snippet := out
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		t.Fatalf("expected truncation/continuation hint, got: %s", snippet)
	}
	if len(out) > 2048+80 {
		t.Fatalf("byte cap not applied: %d", len(out))
	}
}

func TestReadFileRespectsExplicitLimit(t *testing.T) {
	dir := t.TempDir()
	content := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\n"
	if err := os.WriteFile(filepath.Join(dir, "s.txt"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	r := NewChrootedRegistry(dir, ToolLimits{ReadFileDefaultLines: 400, ReadFileMaxBytes: 32768})
	tl, err := r.Get("read_file")
	if err != nil {
		t.Fatal(err)
	}
	out, err := tl.Handler(context.Background(), map[string]interface{}{
		"path":   "s.txt",
		"offset": float64(2),
		"limit":  float64(3),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "lines 2-4") {
		t.Fatalf("expected window header, got %q", out)
	}
	if !strings.Contains(out, "b\nc\nd") {
		t.Fatalf("expected selected lines, got %q", out)
	}
}
