package sessionrun

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound     = errors.New("session run not found")
	ErrNoQueuedRun  = errors.New("no queued session run")
	ErrInvalid      = errors.New("invalid session run")
	ErrActiveExists = errors.New("session already has an active run")
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) CreateQueued(in CreateRunInput) (*Run, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("%w: store unavailable", ErrInvalid)
	}
	if in.UserID < 1 || strings.TrimSpace(in.SessionID) == "" {
		return nil, fmt.Errorf("%w: session and user required", ErrInvalid)
	}
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = "chat"
	}
	if active, err := s.ActiveRunForSession(in.SessionID); err != nil {
		return nil, err
	} else if active != nil {
		return nil, ErrActiveExists
	}

	now := time.Now().UTC()
	run := &Run{
		ID:               uuid.NewString(),
		SessionID:        in.SessionID,
		UserID:           in.UserID,
		WorkspaceID:      strings.TrimSpace(in.WorkspaceID),
		Mode:             mode,
		Model:            strings.TrimSpace(in.Model),
		Status:           StatusQueued,
		TriggerMessageID: strings.TrimSpace(in.TriggerMessageID),
		CreatedAt:        now,
	}
	_, err := s.db.Exec(`
		INSERT INTO session_runs (
			id, session_id, user_id, workspace_id, mode, model, status,
			trigger_message_id, error, last_seq, cancel_requested, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', 0, 0, ?)
	`, run.ID, run.SessionID, run.UserID, run.WorkspaceID, run.Mode, run.Model, run.Status, run.TriggerMessageID, run.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create session run: %w", err)
	}
	return run, nil
}

func (s *Store) Get(userID int64, id string) (*Run, error) {
	run, err := scanRun(s.db.QueryRow(`
		SELECT id, session_id, user_id, workspace_id, mode, model, status, trigger_message_id,
			error, last_seq, cancel_requested, created_at, started_at, finished_at
		FROM session_runs WHERE id = ? AND user_id = ?
	`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return run, err
}

func (s *Store) GetByID(id string) (*Run, error) {
	run, err := scanRun(s.db.QueryRow(`
		SELECT id, session_id, user_id, workspace_id, mode, model, status, trigger_message_id,
			error, last_seq, cancel_requested, created_at, started_at, finished_at
		FROM session_runs WHERE id = ?
	`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return run, err
}

func (s *Store) ActiveRunForSession(sessionID string) (*Run, error) {
	run, err := scanRun(s.db.QueryRow(`
		SELECT id, session_id, user_id, workspace_id, mode, model, status, trigger_message_id,
			error, last_seq, cancel_requested, created_at, started_at, finished_at
		FROM session_runs
		WHERE session_id = ? AND status IN (?, ?)
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, sessionID, StatusQueued, StatusRunning))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return run, err
}

func (s *Store) ClaimNext() (*Run, error) {
	now := time.Now().UTC()
	run, err := scanRun(s.db.QueryRow(`
		UPDATE session_runs
		SET status = ?, started_at = ?
		WHERE id = (
			SELECT id FROM session_runs WHERE status = ? ORDER BY created_at ASC, id ASC LIMIT 1
		) AND status = ?
		RETURNING id, session_id, user_id, workspace_id, mode, model, status, trigger_message_id,
			error, last_seq, cancel_requested, created_at, started_at, finished_at
	`, StatusRunning, now, StatusQueued, StatusQueued))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoQueuedRun
	}
	return run, err
}

func (s *Store) Finish(id string, status Status, errMsg string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(`
		UPDATE session_runs SET status = ?, error = ?, finished_at = ? WHERE id = ?
	`, status, errMsg, now, id)
	return err
}

func (s *Store) RequestCancel(userID int64, id string) (*Run, error) {
	run, err := s.Get(userID, id)
	if err != nil {
		return nil, err
	}
	switch run.Status {
	case StatusQueued:
		if err := s.Finish(id, StatusCancelled, "cancelled"); err != nil {
			return nil, err
		}
		run.Status = StatusCancelled
		return run, nil
	case StatusRunning:
		_, err := s.db.Exec(`UPDATE session_runs SET cancel_requested = 1 WHERE id = ?`, id)
		if err != nil {
			return nil, err
		}
		run.CancelRequested = true
		return run, nil
	default:
		return run, nil
	}
}

func (s *Store) IsCancelRequested(id string) bool {
	var v int
	_ = s.db.QueryRow(`SELECT cancel_requested FROM session_runs WHERE id = ?`, id).Scan(&v)
	return v == 1
}

func (s *Store) RecoverInterrupted() (int64, error) {
	now := time.Now().UTC()
	res, err := s.db.Exec(`
		UPDATE session_runs
		SET status = ?, error = ?, finished_at = ?
		WHERE status = ?
	`, StatusFailed, "interrupted by server restart", now, StatusRunning)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *Store) AppendEvent(runID string, typ EventType, payload any) (*Event, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		b = []byte("{}")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var last int64
	if err := tx.QueryRow(`SELECT last_seq FROM session_runs WHERE id = ?`, runID).Scan(&last); err != nil {
		return nil, err
	}
	seq := last + 1
	now := time.Now().UTC()
	if _, err := tx.Exec(`
		INSERT INTO session_run_events (run_id, seq, type, payload, created_at) VALUES (?, ?, ?, ?, ?)
	`, runID, seq, typ, string(b), now); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE session_runs SET last_seq = ? WHERE id = ?`, seq, runID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Event{RunID: runID, Seq: seq, Type: typ, Payload: b, CreatedAt: now}, nil
}

func (s *Store) ListEventsAfter(runID string, afterSeq int64) ([]Event, error) {
	rows, err := s.db.Query(`
		SELECT run_id, seq, type, payload, created_at
		FROM session_run_events WHERE run_id = ? AND seq > ? ORDER BY seq ASC
	`, runID, afterSeq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Event, 0)
	for rows.Next() {
		var e Event
		var payload string
		if err := rows.Scan(&e.RunID, &e.Seq, &e.Type, &payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Payload = json.RawMessage(payload)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) EnqueueInbox(sessionID, runID, messageID, content string) (*InboxItem, error) {
	now := time.Now().UTC()
	item := &InboxItem{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		RunID:     runID,
		MessageID: messageID,
		Content:   content,
		Status:    InboxPending,
		CreatedAt: now,
	}
	_, err := s.db.Exec(`
		INSERT INTO session_run_inbox (id, session_id, run_id, message_id, content, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.SessionID, item.RunID, item.MessageID, item.Content, item.Status, item.CreatedAt)
	if err != nil {
		return nil, err
	}
	return item, nil
}

// DrainPendingForRun returns pending inbox items for a run in FIFO order and marks them injected.
func (s *Store) DrainPendingForRun(runID string) ([]InboxItem, error) {
	rows, err := s.db.Query(`
		SELECT id, session_id, run_id, message_id, content, status, created_at
		FROM session_run_inbox
		WHERE run_id = ? AND status = ?
		ORDER BY created_at ASC, id ASC
	`, runID, InboxPending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]InboxItem, 0)
	ids := make([]string, 0)
	for rows.Next() {
		var it InboxItem
		if err := rows.Scan(&it.ID, &it.SessionID, &it.RunID, &it.MessageID, &it.Content, &it.Status, &it.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, it)
		ids = append(ids, it.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, err := s.db.Exec(`UPDATE session_run_inbox SET status = ? WHERE id = ?`, InboxInjected, id); err != nil {
			return nil, err
		}
	}
	for i := range items {
		items[i].Status = InboxInjected
	}
	return items, nil
}

func scanRun(row interface {
	Scan(dest ...any) error
}) (*Run, error) {
	var r Run
	var started, finished sql.NullTime
	var cancel int
	err := row.Scan(
		&r.ID, &r.SessionID, &r.UserID, &r.WorkspaceID, &r.Mode, &r.Model, &r.Status, &r.TriggerMessageID,
		&r.Error, &r.LastSeq, &cancel, &r.CreatedAt, &started, &finished,
	)
	if err != nil {
		return nil, err
	}
	r.CancelRequested = cancel == 1
	if started.Valid {
		t := started.Time
		r.StartedAt = &t
	}
	if finished.Valid {
		t := finished.Time
		r.FinishedAt = &t
	}
	return &r, nil
}
