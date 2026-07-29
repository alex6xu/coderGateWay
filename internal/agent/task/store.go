// Package task contains durable cloud workspace Agent Task persistence.
// It intentionally coexists with the legacy generic task registry in task.go.
package task

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
	ErrNotFound     = errors.New("agent task not found")
	ErrNoQueuedTask = errors.New("no queued agent task")
	ErrInvalid      = errors.New("invalid agent task")
	ErrNotRunning   = errors.New("agent task is not running")
)

type Type string

const (
	TypeCodeChange    Type = "code_change"
	TypeDocumentation Type = "documentation"
)

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

// Task is a tenant-scoped request to operate on one cloud workspace.
type Task struct {
	ID             string              `json:"id"`
	UserID         int64               `json:"user_id"`
	WorkspaceID    string              `json:"workspace_id"`
	RouteProfileID int64               `json:"route_profile_id"`
	Type           Type                `json:"type"`
	Prompt         string              `json:"prompt"`
	Status         Status              `json:"status"`
	Result         string              `json:"result"`
	Error          string              `json:"error"`
	ToolSteps      []map[string]string `json:"tool_steps"`
	CreatedAt      time.Time           `json:"created_at"`
	StartedAt      *time.Time          `json:"started_at,omitempty"`
	FinishedAt     *time.Time          `json:"finished_at,omitempty"`
}

type CreateInput struct {
	WorkspaceID    string
	RouteProfileID int64
	Type           Type
	Prompt         string
}

// Store persists cloud Agent Tasks independently from the legacy task registry.
type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Create(userID int64, in CreateInput) (*Task, error) {
	if userID < 1 {
		return nil, fmt.Errorf("%w: user is required", ErrInvalid)
	}
	workspaceID := strings.TrimSpace(in.WorkspaceID)
	prompt := strings.TrimSpace(in.Prompt)
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace is required", ErrInvalid)
	}
	if in.RouteProfileID < 1 {
		return nil, fmt.Errorf("%w: route profile is required", ErrInvalid)
	}
	if in.Type != TypeCodeChange && in.Type != TypeDocumentation {
		return nil, fmt.Errorf("%w: type must be code_change or documentation", ErrInvalid)
	}
	if prompt == "" {
		return nil, fmt.Errorf("%w: prompt is required", ErrInvalid)
	}

	now := time.Now().UTC()
	created := &Task{
		ID:             uuid.NewString(),
		UserID:         userID,
		WorkspaceID:    workspaceID,
		RouteProfileID: in.RouteProfileID,
		Type:           in.Type,
		Prompt:         prompt,
		Status:         StatusQueued,
		ToolSteps:      make([]map[string]string, 0),
		CreatedAt:      now,
	}
	steps, err := json.Marshal(created.ToolSteps)
	if err != nil {
		return nil, fmt.Errorf("encode task tool steps: %w", err)
	}
	if _, err := s.db.Exec(`
		INSERT INTO agent_tasks (id, user_id, workspace_id, route_profile_id, type, prompt, status, result, error, tool_steps, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, '', '', ?, ?)
	`, created.ID, created.UserID, created.WorkspaceID, created.RouteProfileID, created.Type, created.Prompt, created.Status, string(steps), created.CreatedAt); err != nil {
		return nil, fmt.Errorf("create agent task: %w", err)
	}
	return created, nil
}

func (s *Store) List(userID int64) ([]Task, error) {
	rows, err := s.db.Query(`
		SELECT id, user_id, workspace_id, route_profile_id, type, prompt, status, result, error, tool_steps, created_at, started_at, finished_at
		FROM agent_tasks WHERE user_id = ? ORDER BY created_at DESC, id DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list agent tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]Task, 0)
	for rows.Next() {
		one, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, *one)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent tasks: %w", err)
	}
	return tasks, nil
}

func (s *Store) Get(userID int64, id string) (*Task, error) {
	task, err := scanTask(s.db.QueryRow(`
		SELECT id, user_id, workspace_id, route_profile_id, type, prompt, status, result, error, tool_steps, created_at, started_at, finished_at
		FROM agent_tasks WHERE id = ? AND user_id = ?
	`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return task, err
}

// ClaimNext atomically changes one queued task to running. It cannot claim an
// already-running task, even when multiple workers ask concurrently.
func (s *Store) ClaimNext() (*Task, error) {
	now := time.Now().UTC()
	task, err := scanTask(s.db.QueryRow(`
		UPDATE agent_tasks
		SET status = ?, started_at = ?
		WHERE id = (
			SELECT id FROM agent_tasks WHERE status = ? ORDER BY created_at ASC, id ASC LIMIT 1
		) AND status = ?
		RETURNING id, user_id, workspace_id, route_profile_id, type, prompt, status, result, error, tool_steps, created_at, started_at, finished_at
	`, StatusRunning, now, StatusQueued, StatusQueued))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoQueuedTask
	}
	return task, err
}

// RecoverInterrupted moves tasks left running by a stopped server into a
// terminal failed state before a worker starts accepting new work.
func (s *Store) RecoverInterrupted() (int64, error) {
	now := time.Now().UTC()
	result, err := s.db.Exec(`
		UPDATE agent_tasks
		SET status = ?, error = ?, finished_at = ?
		WHERE status = ?
	`, StatusFailed, "task interrupted by server restart", now, StatusRunning)
	if err != nil {
		return 0, fmt.Errorf("recover interrupted agent tasks: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count recovered agent tasks: %w", err)
	}
	return count, nil
}

// Complete records a successful result for a task that this worker has
// already claimed. Terminal updates deliberately require running status so a
// duplicate worker cannot overwrite a completed task.
func (s *Store) Complete(id, result string, steps []map[string]string) error {
	return s.finish(id, StatusSucceeded, result, "", steps)
}

// Fail records a terminal failure for a task that this worker has already
// claimed. Tool steps collected before the error remain available for task
// diagnostics.
func (s *Store) Fail(id, taskError string, steps []map[string]string) error {
	return s.finish(id, StatusFailed, "", taskError, steps)
}

func (s *Store) finish(id string, status Status, result, taskError string, steps []map[string]string) error {
	if status != StatusSucceeded && status != StatusFailed {
		return fmt.Errorf("%w: terminal status is required", ErrInvalid)
	}
	if steps == nil {
		steps = make([]map[string]string, 0)
	}
	rawSteps, err := json.Marshal(steps)
	if err != nil {
		return fmt.Errorf("encode task tool steps: %w", err)
	}
	updated, err := s.db.Exec(`
		UPDATE agent_tasks
		SET status = ?, result = ?, error = ?, tool_steps = ?, finished_at = ?
		WHERE id = ? AND status = ?
	`, status, result, taskError, string(rawSteps), time.Now().UTC(), id, StatusRunning)
	if err != nil {
		return fmt.Errorf("finish agent task: %w", err)
	}
	affected, err := updated.RowsAffected()
	if err != nil {
		return fmt.Errorf("count finished agent task: %w", err)
	}
	if affected == 0 {
		return ErrNotRunning
	}
	return nil
}

type taskScanner interface{ Scan(...any) error }

func scanTask(row taskScanner) (*Task, error) {
	var task Task
	var rawSteps string
	var startedAt, finishedAt sql.NullTime
	if err := row.Scan(
		&task.ID, &task.UserID, &task.WorkspaceID, &task.RouteProfileID, &task.Type, &task.Prompt,
		&task.Status, &task.Result, &task.Error, &rawSteps, &task.CreatedAt, &startedAt, &finishedAt,
	); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(rawSteps), &task.ToolSteps); err != nil {
		return nil, fmt.Errorf("decode agent task tool steps: %w", err)
	}
	if task.ToolSteps == nil {
		task.ToolSteps = make([]map[string]string, 0)
	}
	if startedAt.Valid {
		value := startedAt.Time
		task.StartedAt = &value
	}
	if finishedAt.Valid {
		value := finishedAt.Time
		task.FinishedAt = &value
	}
	return &task, nil
}
