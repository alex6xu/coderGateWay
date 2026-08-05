package server

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func createFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	return os.Create(path)
}

func removeFile(path string) error {
	return os.Remove(path)
}

type unzipOptions struct {
	// StripSingleRoot removes a shared top-level directory when every entry is under it
	// (webkitdirectory zips and GitHub zipballs).
	StripSingleRoot bool
	// SkipHidden drops any path with a "."-prefixed segment.
	SkipHidden bool
	// MaxFileBytes skips non-directory entries larger than this (0 = no limit).
	MaxFileBytes uint64
}

func unzipInto(zipPath, dest string) error {
	return unzipArchive(zipPath, dest, unzipOptions{
		StripSingleRoot: true,
		SkipHidden:      true,
		MaxFileBytes:    3 << 20,
	})
}

// unzipGitHubZipball extracts a GitHub zipball with the same root-strip rules as uploads.
func unzipGitHubZipball(zipPath, dest string) error {
	return unzipArchive(zipPath, dest, unzipOptions{
		StripSingleRoot: true,
		SkipHidden:      false,
		MaxFileBytes:    0,
	})
}

func unzipArchive(zipPath, dest string, opt unzipOptions) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}

	prefix := ""
	if opt.StripSingleRoot {
		prefix = detectSingleRootPrefix(r.File)
	}

	// First pass: materialize directories (including empty dir entries in the zip).
	for _, f := range r.File {
		rel, ok := normalizeZipEntry(f.Name, prefix, opt)
		if !ok {
			continue
		}
		if !f.FileInfo().IsDir() && !strings.HasSuffix(filepath.ToSlash(f.Name), "/") {
			// Ensure parent dirs exist even when the zip omits directory entries.
			parent := filepath.ToSlash(filepath.Dir(rel))
			if parent != "." && parent != "" {
				if err := mkdirUnder(destAbs, parent); err != nil {
					return err
				}
			}
			continue
		}
		if err := mkdirUnder(destAbs, rel); err != nil {
			return err
		}
	}

	for _, f := range r.File {
		rel, ok := normalizeZipEntry(f.Name, prefix, opt)
		if !ok {
			continue
		}
		if f.FileInfo().IsDir() || strings.HasSuffix(filepath.ToSlash(f.Name), "/") {
			continue
		}
		if opt.MaxFileBytes > 0 && f.UncompressedSize64 > opt.MaxFileBytes {
			continue
		}

		targetAbs, err := safeJoinUnder(destAbs, rel)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(targetAbs), 0755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(targetAbs)
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		out.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

// detectSingleRootPrefix returns "dirname/" when every meaningful zip entry lives under
// that single top-level directory; otherwise "".
func detectSingleRootPrefix(files []*zip.File) string {
	var root string
	for _, f := range files {
		name := filepath.ToSlash(f.Name)
		if name == "" || strings.HasPrefix(name, "__MACOSX/") {
			continue
		}
		name = strings.TrimPrefix(name, "./")
		parts := strings.Split(name, "/")
		if len(parts) == 0 || parts[0] == "" {
			continue
		}
		// A bare file at zip root means there is no wrapper directory to strip.
		if len(parts) == 1 && !f.FileInfo().IsDir() && !strings.HasSuffix(name, "/") {
			return ""
		}
		if root == "" {
			root = parts[0]
			continue
		}
		if parts[0] != root {
			return ""
		}
	}
	if root == "" {
		return ""
	}
	return root + "/"
}

func normalizeZipEntry(name, prefix string, opt unzipOptions) (rel string, ok bool) {
	name = filepath.ToSlash(name)
	if name == "" || strings.HasPrefix(name, "__MACOSX/") {
		return "", false
	}
	name = strings.TrimPrefix(name, "./")
	if prefix != "" {
		if !strings.HasPrefix(name, prefix) {
			return "", false
		}
		name = strings.TrimPrefix(name, prefix)
	}
	name = strings.TrimPrefix(name, "/")
	if name == "" || name == "." {
		return "", false
	}
	if strings.Contains(name, "..") {
		return "", false
	}
	if opt.SkipHidden && zipPathHasHiddenSegment(name) {
		return "", false
	}
	return name, true
}

func mkdirUnder(destAbs, rel string) error {
	targetAbs, err := safeJoinUnder(destAbs, rel)
	if err != nil {
		return err
	}
	return os.MkdirAll(targetAbs, 0755)
}

func safeJoinUnder(destAbs, rel string) (string, error) {
	target := filepath.Join(destAbs, filepath.FromSlash(rel))
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if targetAbs != destAbs && !strings.HasPrefix(targetAbs, destAbs+string(os.PathSeparator)) {
		return "", fmt.Errorf("zip path escapes destination: %s", rel)
	}
	return targetAbs, nil
}

func zipPathHasHiddenSegment(name string) bool {
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			continue
		}
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

// stripCommonRootPrefix removes a shared first path segment from relative paths
// (used for multipart file uploads that still carry webkitdirectory prefixes).
func stripCommonRootPrefix(paths []string) (root string, stripped []string) {
	var common string
	for _, p := range paths {
		p = filepath.ToSlash(strings.TrimSpace(p))
		p = strings.TrimPrefix(p, "./")
		if p == "" {
			continue
		}
		parts := strings.Split(p, "/")
		if len(parts) < 2 {
			// File at "root" of the selection without a folder wrapper.
			return "", paths
		}
		if common == "" {
			common = parts[0]
			continue
		}
		if parts[0] != common {
			return "", paths
		}
	}
	if common == "" {
		return "", paths
	}
	out := make([]string, len(paths))
	for i, p := range paths {
		p = filepath.ToSlash(strings.TrimSpace(p))
		p = strings.TrimPrefix(p, "./")
		out[i] = strings.TrimPrefix(p, common+"/")
	}
	return common, out
}
