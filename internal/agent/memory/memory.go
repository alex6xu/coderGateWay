package memory

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// MemoryEntry represents a memory entry
type MemoryEntry struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Scope   string `json:"scope"`
	ScopeID string `json:"scope_id"`
	Type    string `json:"type"`
	Content string `json:"content"`
	Score   float64 `json:"score,omitempty"`
}

// MemoryService manages memories with FTS5
type MemoryService struct {
	db *sql.DB
}

// NewMemoryService creates a new memory service
func NewMemoryService(db *sql.DB) *MemoryService {
	return &MemoryService{db: db}
}

// Write writes a memory entry
func (s *MemoryService) Write(entry *MemoryEntry) error {
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}

	_, err := s.db.Exec(
		"INSERT INTO memory_fts (path, scope, scope_id, type, content) VALUES (?, ?, ?, ?, ?)",
		entry.Path, entry.Scope, entry.ScopeID, entry.Type, entry.Content,
	)
	if err != nil {
		return fmt.Errorf("failed to write memory: %w", err)
	}

	return nil
}

// Search searches memories using FTS5
func (s *MemoryService) Search(query string, scope string, scopeID string, limit int) ([]*MemoryEntry, error) {
	if limit <= 0 {
		limit = 10
	}

	// Build query
	sqlQuery := `
		SELECT path, scope, scope_id, type, snippet(memory_fts, 4, '<b>', '</b>', '...', 32) as content, rank
		FROM memory_fts
		WHERE memory_fts MATCH ?
	`
	args := []interface{}{query}

	if scope != "" {
		sqlQuery += " AND scope = ?"
		args = append(args, scope)
	}
	if scopeID != "" {
		sqlQuery += " AND scope_id = ?"
		args = append(args, scopeID)
	}

	sqlQuery += " ORDER BY rank LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to search memories: %w", err)
	}
	defer rows.Close()

	entries := make([]*MemoryEntry, 0)
	for rows.Next() {
		var entry MemoryEntry
		err := rows.Scan(&entry.Path, &entry.Scope, &entry.ScopeID, &entry.Type, &entry.Content, &entry.Score)
		if err != nil {
			return nil, fmt.Errorf("failed to scan memory: %w", err)
		}
		entries = append(entries, &entry)
	}

	return entries, nil
}

// Recent returns recent memories
func (s *MemoryService) Recent(limit int) ([]*MemoryEntry, error) {
	if limit <= 0 {
		limit = 10
	}

	rows, err := s.db.Query(
		"SELECT path, scope, scope_id, type, content FROM memory_fts ORDER BY rowid DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get recent memories: %w", err)
	}
	defer rows.Close()

	entries := make([]*MemoryEntry, 0)
	for rows.Next() {
		var entry MemoryEntry
		err := rows.Scan(&entry.Path, &entry.Scope, &entry.ScopeID, &entry.Type, &entry.Content)
		if err != nil {
			return nil, fmt.Errorf("failed to scan memory: %w", err)
		}
		entries = append(entries, &entry)
	}

	return entries, nil
}

// Delete deletes a memory entry
func (s *MemoryService) Delete(path string) error {
	_, err := s.db.Exec("DELETE FROM memory_fts WHERE path = ?", path)
	if err != nil {
		return fmt.Errorf("failed to delete memory: %w", err)
	}

	return nil
}

// Reconcile reconciles memory index
func (s *MemoryService) Reconcile() (int, error) {
	// Count entries
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM memory_fts").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count memories: %w", err)
	}

	return count, nil
}

// MemoryStats represents memory statistics
type MemoryStats struct {
	TotalEntries int            `json:"total_entries"`
	ByScope      map[string]int `json:"by_scope"`
	ByType       map[string]int `json:"by_type"`
}

// GetStats returns memory statistics
func (s *MemoryService) GetStats() (*MemoryStats, error) {
	stats := &MemoryStats{
		ByScope: make(map[string]int),
		ByType:  make(map[string]int),
	}

	// Total entries
	err := s.db.QueryRow("SELECT COUNT(*) FROM memory_fts").Scan(&stats.TotalEntries)
	if err != nil {
		return nil, fmt.Errorf("failed to count memories: %w", err)
	}

	// By scope
	rows, err := s.db.Query("SELECT scope, COUNT(*) FROM memory_fts GROUP BY scope")
	if err != nil {
		return nil, fmt.Errorf("failed to get scope stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var scope string
		var count int
		if err := rows.Scan(&scope, &count); err != nil {
			return nil, fmt.Errorf("failed to scan scope: %w", err)
		}
		stats.ByScope[scope] = count
	}

	// By type
	rows, err = s.db.Query("SELECT type, COUNT(*) FROM memory_fts GROUP BY type")
	if err != nil {
		return nil, fmt.Errorf("failed to get type stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var typ string
		var count int
		if err := rows.Scan(&typ, &count); err != nil {
			return nil, fmt.Errorf("failed to scan type: %w", err)
		}
		stats.ByType[typ] = count
	}

	return stats, nil
}

// WriteProjectMemory writes a project memory
func (s *MemoryService) WriteProjectMemory(projectID string, content string) error {
	return s.Write(&MemoryEntry{
		Path:    fmt.Sprintf("projects/%s/memory.md", projectID),
		Scope:   "projects",
		ScopeID: projectID,
		Type:    "project",
		Content: content,
	})
}

// WriteSessionMemory writes a session memory
func (s *MemoryService) WriteSessionMemory(sessionID string, content string) error {
	return s.Write(&MemoryEntry{
		Path:    fmt.Sprintf("sessions/%s/checkpoint.md", sessionID),
		Scope:   "sessions",
		ScopeID: sessionID,
		Type:    "checkpoint",
		Content: content,
	})
}

// WriteGlobalMemory writes a global memory
func (s *MemoryService) WriteGlobalMemory(content string) error {
	return s.Write(&MemoryEntry{
		Path:    "global/MEMORY.md",
		Scope:   "global",
		ScopeID: "global",
		Type:    "global",
		Content: content,
	})
}

// Timestamp returns current timestamp
func Timestamp() string {
	return time.Now().UTC().Format(time.RFC3339)
}
