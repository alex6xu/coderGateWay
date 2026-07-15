package server

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestUnzipGitHubZipballStripsPrefix(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "repo.zip")
	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0755); err != nil {
		t.Fatal(err)
	}

	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("owner-repo-abc123/README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("# hello"))
	w2, err := zw.Create("owner-repo-abc123/src/main.go")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w2.Write([]byte("package main"))
	_ = zw.Close()
	_ = f.Close()

	if err := unzipGitHubZipball(zipPath, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil {
		t.Fatalf("expected stripped README: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "src", "main.go")); err != nil {
		t.Fatalf("expected stripped src/main.go: %v", err)
	}
}
