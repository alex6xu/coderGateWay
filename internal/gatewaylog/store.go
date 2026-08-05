package gatewaylog

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const maxBodyBytes = 1 << 20 // 1MB

// Entry is a gateway chat/completions audit record.
type Entry struct {
	ID               string    `json:"id"`
	UserID           int64     `json:"user_id"`
	ChannelID        int64     `json:"channel_id,omitempty"`
	ChannelName      string    `json:"channel_name,omitempty"`
	Model            string    `json:"model"`
	Stream           bool      `json:"stream"`
	StatusCode       int       `json:"status_code"`
	Error            string    `json:"error,omitempty"`
	RequestBody      string    `json:"request_body,omitempty"`
	ResponseBody     string    `json:"response_body,omitempty"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	CachedTokens     int       `json:"cached_tokens"`
	LatencyMs        int64     `json:"latency_ms"`
	CreatedAt        time.Time `json:"created_at"`
}

// ListFilter filters list queries.
type ListFilter struct {
	Model      string
	StatusCode int // 0 = any
	Limit      int
	Offset     int
}

// Store persists gateway request logs.
type Store struct {
	db *sql.DB
}

// NewStore creates a gateway log store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Insert writes a log entry (bodies truncated/redacted).
func (s *Store) Insert(e *Entry) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	e.RequestBody = prepareBody(e.RequestBody)
	e.ResponseBody = prepareBody(e.ResponseBody)

	stream := 0
	if e.Stream {
		stream = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO gateway_request_logs (
			id, user_id, channel_id, channel_name, model, stream, status_code, error,
			request_body, response_body, prompt_tokens, completion_tokens, cached_tokens,
			latency_ms, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, e.ID, e.UserID, nullInt64(e.ChannelID), e.ChannelName, e.Model, stream, e.StatusCode, e.Error,
		e.RequestBody, e.ResponseBody, e.PromptTokens, e.CompletionTokens, e.CachedTokens,
		e.LatencyMs, e.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert gateway request log: %w", err)
	}
	return nil
}

// List returns summaries for an account (bodies omitted).
func (s *Store) List(userID int64, f ListFilter) ([]Entry, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	q := `
		SELECT id, user_id, COALESCE(channel_id, 0), COALESCE(channel_name, ''), COALESCE(model, ''),
		       stream, status_code, COALESCE(error, ''), prompt_tokens, completion_tokens, cached_tokens,
		       latency_ms, created_at
		FROM gateway_request_logs
		WHERE user_id = ?
	`
	args := []interface{}{userID}
	if m := strings.TrimSpace(f.Model); m != "" {
		q += ` AND model = ?`
		args = append(args, m)
	}
	if f.StatusCode > 0 {
		q += ` AND status_code = ?`
		args = append(args, f.StatusCode)
	}
	q += ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	args = append(args, f.Limit, f.Offset)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Entry
	for rows.Next() {
		var e Entry
		var stream int
		if err := rows.Scan(
			&e.ID, &e.UserID, &e.ChannelID, &e.ChannelName, &e.Model,
			&stream, &e.StatusCode, &e.Error, &e.PromptTokens, &e.CompletionTokens, &e.CachedTokens,
			&e.LatencyMs, &e.CreatedAt,
		); err != nil {
			return nil, err
		}
		e.Stream = stream != 0
		out = append(out, e)
	}
	return out, rows.Err()
}

// Get returns a full entry for an account.
func (s *Store) Get(userID int64, id string) (*Entry, error) {
	var e Entry
	var stream int
	err := s.db.QueryRow(`
		SELECT id, user_id, COALESCE(channel_id, 0), COALESCE(channel_name, ''), COALESCE(model, ''),
		       stream, status_code, COALESCE(error, ''), COALESCE(request_body, ''), COALESCE(response_body, ''),
		       prompt_tokens, completion_tokens, cached_tokens, latency_ms, created_at
		FROM gateway_request_logs
		WHERE user_id = ? AND id = ?
	`, userID, id).Scan(
		&e.ID, &e.UserID, &e.ChannelID, &e.ChannelName, &e.Model,
		&stream, &e.StatusCode, &e.Error, &e.RequestBody, &e.ResponseBody,
		&e.PromptTokens, &e.CompletionTokens, &e.CachedTokens, &e.LatencyMs, &e.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.Stream = stream != 0
	return &e, nil
}

func nullInt64(v int64) interface{} {
	if v == 0 {
		return nil
	}
	return v
}

// prepareBody redacts sensitive keys and truncates oversized payloads.
func prepareBody(raw string) string {
	if raw == "" {
		return ""
	}
	redacted := redactJSONBody(raw)
	return truncateBody(redacted)
}

func truncateBody(s string) string {
	if len(s) <= maxBodyBytes {
		return s
	}
	return s[:maxBodyBytes] + "\n/* truncated */"
}

// redactJSONBody masks api_key / authorization-like fields in JSON objects.
func redactJSONBody(raw string) string {
	var v interface{}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw
	}
	redactValue(v)
	b, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return string(b)
}

func redactValue(v interface{}) {
	switch x := v.(type) {
	case map[string]interface{}:
		for k, child := range x {
			lk := strings.ToLower(k)
			if lk == "api_key" || lk == "apikey" || lk == "authorization" || lk == "x-api-key" || lk == "access_token" || lk == "password" {
				x[k] = "****"
				continue
			}
			redactValue(child)
		}
	case []interface{}:
		for _, child := range x {
			redactValue(child)
		}
	}
}

// MarshalRequestJSON serializes a request object for storage.
func MarshalRequestJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"error":"marshal failed: %s"}`, err.Error())
	}
	return string(b)
}
