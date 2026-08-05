package server

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestUnzipIntoStripsSingleRootAndKeepsDirs(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "proj.zip")
	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0755); err != nil {
		t.Fatal(err)
	}

	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	// Explicit empty directory under the wrapper root.
	if _, err := zw.Create("MyApp/emptydir/"); err != nil {
		t.Fatal(err)
	}
	w, err := zw.Create("MyApp/src/main.go")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("package main"))
	w2, err := zw.Create("MyApp/README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w2.Write([]byte("# app"))
	// Hidden should be skipped for upload unzip.
	wh, err := zw.Create("MyApp/.env")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = wh.Write([]byte("SECRET=1"))
	_ = zw.Close()
	_ = f.Close()

	if err := unzipInto(zipPath, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "MyApp")); err == nil {
		t.Fatal("expected wrapper MyApp/ to be stripped")
	}
	if _, err := os.Stat(filepath.Join(dest, "src", "main.go")); err != nil {
		t.Fatalf("expected src/main.go at workspace root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "emptydir")); err != nil {
		t.Fatalf("expected empty directory preserved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".env")); err == nil {
		t.Fatal("expected hidden .env skipped")
	}
}

func TestUnzipIntoNoStripWhenMixedRoots(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "mixed.zip")
	dest := filepath.Join(dir, "out")
	_ = os.MkdirAll(dest, 0755)

	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("a/one.txt")
	_, _ = w.Write([]byte("1"))
	w2, _ := zw.Create("b/two.txt")
	_, _ = w2.Write([]byte("2"))
	_ = zw.Close()
	_ = f.Close()

	if err := unzipInto(zipPath, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "a", "one.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "b", "two.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestStripCommonRootPrefix(t *testing.T) {
	root, out := stripCommonRootPrefix([]string{"App/src/a.go", "App/src/b.go", "App/README.md"})
	if root != "App" {
		t.Fatalf("root=%q", root)
	}
	if out[0] != "src/a.go" || out[2] != "README.md" {
		t.Fatalf("out=%v", out)
	}
	root, out = stripCommonRootPrefix([]string{"flat.go", "App/x.go"})
	if root != "" || out[0] != "flat.go" {
		t.Fatalf("expected no strip, got root=%q out=%v", root, out)
	}
}
