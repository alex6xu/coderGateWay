package workspace

import (
	"archive/zip"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Workspace is a cloud-side code directory owned by an account.
type Workspace struct {
	ID        string    `json:"id"`
	UserID    int64     `json:"user_id"`
	Name      string    `json:"name"`
	RootPath  string    `json:"-"`
	FileCount int       `json:"file_count"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Manager persists workspaces under a data root.
type Manager struct {
	db      *sql.DB
	dataDir string
}

// NewManager creates a workspace manager.
func NewManager(db *sql.DB, dataDir string) *Manager {
	if dataDir == "" {
		dataDir = "./data/workspaces"
	}
	return &Manager{db: db, dataDir: dataDir}
}

// RootFor returns the absolute root directory for a workspace id/account.
func (m *Manager) RootFor(accountID int64, workspaceID string) string {
	return filepath.Join(m.dataDir, fmt.Sprintf("%d", accountID), workspaceID)
}

// CreateEmpty creates metadata and root directory.
func (m *Manager) CreateEmpty(accountID int64, name string) (*Workspace, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "project"
	}
	id := uuid.NewString()
	root := m.RootFor(accountID, id)
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, fmt.Errorf("failed to create workspace dir: %w", err)
	}

	now := time.Now()
	_, err := m.db.Exec(`
		INSERT INTO workspaces (id, user_id, name, root_path, file_count, size_bytes, created_at, updated_at)
		VALUES (?, ?, ?, ?, 0, 0, ?, ?)
	`, id, accountID, name, root, now, now)
	if err != nil {
		_ = os.RemoveAll(root)
		return nil, fmt.Errorf("failed to insert workspace: %w", err)
	}

	return &Workspace{
		ID:        id,
		UserID:    accountID,
		Name:      name,
		RootPath:  root,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// Get returns a workspace owned by the account.
func (m *Manager) Get(accountID int64, id string) (*Workspace, error) {
	var ws Workspace
	err := m.db.QueryRow(`
		SELECT id, user_id, name, root_path, file_count, size_bytes, created_at, updated_at
		FROM workspaces WHERE id = ? AND user_id = ?
	`, id, accountID).Scan(
		&ws.ID, &ws.UserID, &ws.Name, &ws.RootPath, &ws.FileCount, &ws.SizeBytes, &ws.CreatedAt, &ws.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("workspace not found")
	}
	return &ws, nil
}

// List returns workspaces for an account.
func (m *Manager) List(accountID int64) ([]*Workspace, error) {
	rows, err := m.db.Query(`
		SELECT id, user_id, name, root_path, file_count, size_bytes, created_at, updated_at
		FROM workspaces WHERE user_id = ? ORDER BY updated_at DESC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*Workspace, 0)
	for rows.Next() {
		var ws Workspace
		if err := rows.Scan(
			&ws.ID, &ws.UserID, &ws.Name, &ws.RootPath, &ws.FileCount, &ws.SizeBytes, &ws.CreatedAt, &ws.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, &ws)
	}
	return out, nil
}

// Delete removes workspace files and metadata.
func (m *Manager) Delete(accountID int64, id string) error {
	ws, err := m.Get(accountID, id)
	if err != nil {
		return err
	}
	_ = os.RemoveAll(ws.RootPath)
	_, err = m.db.Exec(`DELETE FROM workspaces WHERE id = ? AND user_id = ?`, id, accountID)
	return err
}

// WriteRelativeFile writes a file under the workspace root (path is relative).
func (m *Manager) WriteRelativeFile(ws *Workspace, relPath string, r io.Reader) error {
	clean, err := SafeJoin(ws.RootPath, relPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(clean), 0755); err != nil {
		return err
	}
	f, err := os.Create(clean)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

// RefreshStats updates file_count and size_bytes.
func (m *Manager) RefreshStats(ws *Workspace) error {
	var count int
	var size int64
	_ = filepath.Walk(ws.RootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		count++
		size += info.Size()
		return nil
	})
	now := time.Now()
	_, err := m.db.Exec(`
		UPDATE workspaces SET file_count = ?, size_bytes = ?, updated_at = ? WHERE id = ?
	`, count, size, now, ws.ID)
	if err != nil {
		return err
	}
	ws.FileCount = count
	ws.SizeBytes = size
	ws.UpdatedAt = now
	return nil
}

// TreeEntry is a simple file tree node.
type TreeEntry struct {
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size,omitempty"`
}

// ListTree returns a shallow or recursive listing (max 500 entries).
func (m *Manager) ListTree(ws *Workspace, rel string, recursive bool) ([]TreeEntry, error) {
	base, err := SafeJoin(ws.RootPath, rel)
	if err != nil {
		return nil, err
	}
	entries := make([]TreeEntry, 0)
	walkFn := func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if path == base {
			return nil
		}
		relPath, err := filepath.Rel(ws.RootPath, path)
		if err != nil {
			return nil
		}
		relPath = filepath.ToSlash(relPath)
		entries = append(entries, TreeEntry{
			Path:  relPath,
			IsDir: info.IsDir(),
			Size:  info.Size(),
		})
		if !recursive && info.IsDir() && path != base {
			return filepath.SkipDir
		}
		if len(entries) >= 500 {
			return io.EOF
		}
		return nil
	}

	err = filepath.Walk(base, walkFn)
	if err == io.EOF {
		err = nil
	}
	return entries, err
}

// ZipTo writes a zip of the workspace to w.
func (m *Manager) ZipTo(ws *Workspace, w io.Writer) error {
	zw := zip.NewWriter(w)
	defer zw.Close()

	return filepath.Walk(ws.RootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(ws.RootPath, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		fw, err := zw.Create(rel)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(fw, f)
		return err
	})
}

// SafeJoin joins root and a relative path, rejecting escapes.
func SafeJoin(root, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "." {
		return root, nil
	}
	rel = strings.ReplaceAll(rel, "\\", "/")
	rel = strings.TrimPrefix(rel, "/")
	clean := filepath.Clean(filepath.Join(root, filepath.FromSlash(rel)))
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	cleanAbs, err := filepath.Abs(clean)
	if err != nil {
		return "", err
	}
	sep := string(os.PathSeparator)
	if cleanAbs != rootAbs && !strings.HasPrefix(cleanAbs, rootAbs+sep) {
		return "", fmt.Errorf("path escapes workspace: %s", rel)
	}
	return cleanAbs, nil
}
