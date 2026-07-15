package memory

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MemoryEntry represents a memory entry
type MemoryEntry struct {
	ID      string  `json:"id"`
	Path    string  `json:"path"`
	Scope   string  `json:"scope"`
	ScopeID string  `json:"scope_id"`
	Type    string  `json:"type"`
	Content string  `json:"content"`
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

var ftsTokenRe = regexp.MustCompile(`[\p{L}\p{N}_]+`)

// SanitizeFTSQuery converts free text into a safe FTS5 MATCH query.
func SanitizeFTSQuery(query string) string {
	tokens := ftsTokenRe.FindAllString(query, 16)
	if len(tokens) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tokens))
	seen := map[string]bool{}
	for _, t := range tokens {
		if len([]rune(t)) < 2 {
			continue
		}
		key := strings.ToLower(t)
		if seen[key] {
			continue
		}
		seen[key] = true
		parts = append(parts, `"`+strings.ReplaceAll(t, `"`, "")+`"`)
		if len(parts) >= 10 {
			break
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " OR ")
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

	match := SanitizeFTSQuery(query)
	if match == "" {
		return []*MemoryEntry{}, nil
	}

	sqlQuery := `
		SELECT path, scope, scope_id, type, snippet(memory_fts, 4, '', '', '...', 48) as content, rank
		FROM memory_fts
		WHERE memory_fts MATCH ?
	`
	args := []interface{}{match}

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
		return []*MemoryEntry{}, nil
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

// SearchSessionRelevant searches session + global memories for a query.
func (s *MemoryService) SearchSessionRelevant(sessionID, query string, limit int) ([]*MemoryEntry, error) {
	if limit <= 0 {
		limit = 5
	}
	sessionHits, err := s.Search(query, "sessions", sessionID, limit)
	if err != nil {
		return nil, err
	}
	if len(sessionHits) >= limit {
		return sessionHits, nil
	}
	globalHits, _ := s.Search(query, "global", "", limit-len(sessionHits))
	return append(sessionHits, globalHits...), nil
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

	err := s.db.QueryRow("SELECT COUNT(*) FROM memory_fts").Scan(&stats.TotalEntries)
	if err != nil {
		return nil, fmt.Errorf("failed to count memories: %w", err)
	}

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

// SessionCheckpoint is the stable, infrequently updated session summary.
type SessionCheckpoint struct {
	Content      string
	AfterMsgID   string // history after this message id is the append-only suffix
	Raw          string
}

const checkpointCutoffPrefix = "[cutoff:"

// UpsertSessionMemory replaces the session checkpoint memory.
func (s *MemoryService) UpsertSessionMemory(sessionID string, content string) error {
	return s.UpsertSessionCheckpoint(sessionID, content, "")
}

// UpsertSessionCheckpoint stores a stable checkpoint and optional history cutoff.
// Messages at/after AfterMsgID form the append-only prompt suffix until the next fold.
func (s *MemoryService) UpsertSessionCheckpoint(sessionID, content, afterMsgID string) error {
	path := fmt.Sprintf("sessions/%s/checkpoint.md", sessionID)
	_ = s.Delete(path)
	body := strings.TrimSpace(content)
	if afterMsgID != "" {
		body = fmt.Sprintf("%s%s]\n%s", checkpointCutoffPrefix, afterMsgID, body)
	}
	return s.Write(&MemoryEntry{
		Path:    path,
		Scope:   "sessions",
		ScopeID: sessionID,
		Type:    "checkpoint",
		Content: body,
	})
}

// GetSessionCheckpoint loads the stable session checkpoint (not FTS-ranked).
func (s *MemoryService) GetSessionCheckpoint(sessionID string) (*SessionCheckpoint, error) {
	if sessionID == "" {
		return nil, nil
	}
	path := fmt.Sprintf("sessions/%s/checkpoint.md", sessionID)
	var content string
	err := s.db.QueryRow(
		`SELECT content FROM memory_fts WHERE path = ? AND type = 'checkpoint' LIMIT 1`,
		path,
	).Scan(&content)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cp := &SessionCheckpoint{Raw: content, Content: content}
	if strings.HasPrefix(content, checkpointCutoffPrefix) {
		rest := strings.TrimPrefix(content, checkpointCutoffPrefix)
		if i := strings.IndexByte(rest, ']'); i >= 0 {
			cp.AfterMsgID = rest[:i]
			cp.Content = strings.TrimSpace(rest[i+1:])
		}
	}
	return cp, nil
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
