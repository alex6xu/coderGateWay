package task

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/alex/codegateway/internal/model"
	"github.com/google/uuid"
)

// TaskRegistry manages tasks
type TaskRegistry struct {
	db *sql.DB
}

// NewTaskRegistry creates a new task registry
func NewTaskRegistry(db *sql.DB) *TaskRegistry {
	return &TaskRegistry{db: db}
}

// Create creates a new task
func (r *TaskRegistry) Create(summary string, parentID *string, sessionID *string) (*model.Task, error) {
	task := &model.Task{
		ID:        uuid.New().String(),
		ParentID:  parentID,
		SessionID: sessionID,
		Summary:   summary,
		Status:    model.TaskStatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	_, err := r.db.Exec(
		"INSERT INTO tasks (id, parent_id, session_id, summary, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		task.ID, task.ParentID, task.SessionID, task.Summary, task.Status, task.CreatedAt, task.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create task: %w", err)
	}

	return task, nil
}

// Get returns a task by ID
func (r *TaskRegistry) Get(id string) (*model.Task, error) {
	var task model.Task
	err := r.db.QueryRow(
		"SELECT id, parent_id, session_id, summary, status, event_summary, created_at, updated_at FROM tasks WHERE id = ?",
		id,
	).Scan(
		&task.ID, &task.ParentID, &task.SessionID, &task.Summary,
		&task.Status, &task.EventSummary, &task.CreatedAt, &task.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}

	return &task, nil
}

// List returns all tasks
func (r *TaskRegistry) List(parentID *string, status string, limit, offset int) ([]*model.Task, error) {
	query := "SELECT id, parent_id, session_id, summary, status, event_summary, created_at, updated_at FROM tasks WHERE 1=1"
	args := []interface{}{}

	if parentID != nil {
		query += " AND parent_id = ?"
		args = append(args, *parentID)
	} else {
		query += " AND parent_id IS NULL"
	}

	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}

	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]*model.Task, 0)
	for rows.Next() {
		var task model.Task
		err := rows.Scan(
			&task.ID, &task.ParentID, &task.SessionID, &task.Summary,
			&task.Status, &task.EventSummary, &task.CreatedAt, &task.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan task: %w", err)
		}
		tasks = append(tasks, &task)
	}

	return tasks, nil
}

// Start marks a task as in progress
func (r *TaskRegistry) Start(id string) error {
	_, err := r.db.Exec(
		"UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?",
		model.TaskStatusInProgress, time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("failed to start task: %w", err)
	}

	return nil
}

// Done marks a task as done
func (r *TaskRegistry) Done(id string, eventSummary string) error {
	_, err := r.db.Exec(
		"UPDATE tasks SET status = ?, event_summary = ?, updated_at = ? WHERE id = ?",
		model.TaskStatusDone, eventSummary, time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("failed to complete task: %w", err)
	}

	return nil
}

// Block marks a task as blocked
func (r *TaskRegistry) Block(id string, eventSummary string) error {
	_, err := r.db.Exec(
		"UPDATE tasks SET status = ?, event_summary = ?, updated_at = ? WHERE id = ?",
		model.TaskStatusBlocked, eventSummary, time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("failed to block task: %w", err)
	}

	return nil
}

// Unblock marks a task as open
func (r *TaskRegistry) Unblock(id string) error {
	_, err := r.db.Exec(
		"UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?",
		model.TaskStatusOpen, time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("failed to unblock task: %w", err)
	}

	return nil
}

// Abandon marks a task as abandoned
func (r *TaskRegistry) Abandon(id string, eventSummary string) error {
	_, err := r.db.Exec(
		"UPDATE tasks SET status = ?, event_summary = ?, updated_at = ? WHERE id = ?",
		model.TaskStatusAbandoned, eventSummary, time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("failed to abandon task: %w", err)
	}

	return nil
}

// Update updates a task
func (r *TaskRegistry) Update(task *model.Task) error {
	task.UpdatedAt = time.Now()

	_, err := r.db.Exec(
		"UPDATE tasks SET summary = ?, status = ?, event_summary = ?, updated_at = ? WHERE id = ?",
		task.Summary, task.Status, task.EventSummary, task.UpdatedAt, task.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update task: %w", err)
	}

	return nil
}

// Delete deletes a task
func (r *TaskRegistry) Delete(id string) error {
	// Delete subtasks first
	_, err := r.db.Exec("DELETE FROM tasks WHERE parent_id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete subtasks: %w", err)
	}

	// Delete task
	_, err = r.db.Exec("DELETE FROM tasks WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete task: %w", err)
	}

	return nil
}

// GetTree returns a task tree
func (r *TaskRegistry) GetTree(rootID string) ([]*model.Task, error) {
	query := `
		WITH RECURSIVE task_tree AS (
			SELECT id, parent_id, session_id, summary, status, event_summary, created_at, updated_at
			FROM tasks
			WHERE id = ?
			UNION ALL
			SELECT t.id, t.parent_id, t.session_id, t.summary, t.status, t.event_summary, t.created_at, t.updated_at
			FROM tasks t
			INNER JOIN task_tree tt ON t.parent_id = tt.id
		)
		SELECT * FROM task_tree
	`

	rows, err := r.db.Query(query, rootID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task tree: %w", err)
	}
	defer rows.Close()

	tasks := make([]*model.Task, 0)
	for rows.Next() {
		var task model.Task
		err := rows.Scan(
			&task.ID, &task.ParentID, &task.SessionID, &task.Summary,
			&task.Status, &task.EventSummary, &task.CreatedAt, &task.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan task: %w", err)
		}
		tasks = append(tasks, &task)
	}

	return tasks, nil
}

// GetStats returns task statistics
func (r *TaskRegistry) GetStats() (map[string]int, error) {
	stats := make(map[string]int)

	rows, err := r.db.Query("SELECT status, COUNT(*) FROM tasks GROUP BY status")
	if err != nil {
		return nil, fmt.Errorf("failed to get task stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("failed to scan stats: %w", err)
		}
		stats[status] = count
	}

	return stats, nil
}
