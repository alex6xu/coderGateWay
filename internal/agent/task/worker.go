package task

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/alex/codegateway/internal/provider"
	"github.com/alex/codegateway/internal/workspace"
)

// WorkerConfig supplies the server-owned dependencies required to execute a
// durable task. The worker never receives channel credentials or a workspace
// path from the task record: the server resolves the provider and workspace
// from tenant-scoped stores.
type WorkerConfig struct {
	Store            *Store
	WorkspaceForTask func(userID int64, workspaceID string) (*workspace.Workspace, error)
	ProviderForTask  func(task *Task) (provider.Provider, string, error)
	Run              func(context.Context, provider.Provider, string, string, string, *workspace.Workspace) (string, []map[string]string, error)
	RefreshStats     func(*workspace.Workspace) error
	PollInterval     time.Duration
}

// Worker claims queued tasks and executes them one at a time. Processing is
// intentionally serial because each task can edit its owned workspace.
type Worker struct{ config WorkerConfig }

func NewWorker(config WorkerConfig) *Worker {
	if config.PollInterval <= 0 {
		config.PollInterval = time.Second
	}
	return &Worker{config: config}
}

// RecoverInterrupted marks stale running tasks terminal before new work is
// accepted after a server restart.
func (w *Worker) RecoverInterrupted() (int64, error) {
	if w.config.Store == nil {
		return 0, errors.New("agent task worker has no store")
	}
	return w.config.Store.RecoverInterrupted()
}

// Run polls until cancellation. RecoverInterrupted is intentionally called by
// server startup before this loop is launched, so recovery completes before
// requests can queue work for this process.
func (w *Worker) Run(ctx context.Context) error {
	if w.config.Store == nil {
		return errors.New("agent task worker has no store")
	}
	for {
		processed, err := w.ProcessNext(ctx)
		if err != nil {
			return err
		}
		if processed {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(w.config.PollInterval):
		}
	}
}

// ProcessNext claims one queued task and persists a terminal result. Expected
// task-specific errors are persisted and do not stop the worker loop.
func (w *Worker) ProcessNext(ctx context.Context) (bool, error) {
	if w.config.Store == nil {
		return false, errors.New("agent task worker has no store")
	}
	task, err := w.config.Store.ClaimNext()
	if errors.Is(err, ErrNoQueuedTask) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim agent task: %w", err)
	}
	if err := w.processClaimed(ctx, task); err != nil {
		return true, err
	}
	return true, nil
}

func (w *Worker) processClaimed(ctx context.Context, task *Task) error {
	if w.config.WorkspaceForTask == nil || w.config.ProviderForTask == nil || w.config.Run == nil {
		return w.persistFailure(task, nil, errors.New("agent task worker is not configured"))
	}
	ws, err := w.config.WorkspaceForTask(task.UserID, task.WorkspaceID)
	if err != nil {
		return w.persistFailure(task, nil, fmt.Errorf("load owned workspace: %w", err))
	}
	if ws == nil || ws.UserID != task.UserID || ws.ID != task.WorkspaceID {
		return w.persistFailure(task, nil, errors.New("owned workspace lookup returned an invalid workspace"))
	}
	prov, modelName, err := w.config.ProviderForTask(task)
	if err != nil {
		return w.persistFailure(task, nil, fmt.Errorf("resolve route profile: %w", err))
	}
	if prov == nil || strings.TrimSpace(modelName) == "" {
		return w.persistFailure(task, nil, errors.New("route profile has no eligible provider"))
	}
	systemPrompt, err := TaskSystemPrompt(task.Type, modelName, ws.Name)
	if err != nil {
		return w.persistFailure(task, nil, err)
	}
	result, steps, runErr := w.config.Run(ctx, prov, modelName, systemPrompt, task.Prompt, ws)
	steps = sanitizeToolSteps(steps)
	if w.config.RefreshStats != nil {
		if refreshErr := w.config.RefreshStats(ws); refreshErr != nil && runErr == nil {
			runErr = fmt.Errorf("refresh workspace stats: %w", refreshErr)
		}
	}
	if runErr != nil {
		return w.persistFailure(task, steps, runErr)
	}
	if err := w.config.Store.Complete(task.ID, sanitizeTaskText(result), steps); err != nil {
		return fmt.Errorf("persist task completion: %w", err)
	}
	return nil
}

func (w *Worker) persistFailure(task *Task, steps []map[string]string, cause error) error {
	if err := w.config.Store.Fail(task.ID, sanitizeTaskText(cause.Error()), sanitizeToolSteps(steps)); err != nil {
		return fmt.Errorf("persist task failure after %v: %w", cause, err)
	}
	return nil
}

// TaskSystemPrompt returns explicit, task-type-specific instructions without
// including provider configuration or any credential material.
func TaskSystemPrompt(kind Type, modelName, workspaceName string) (string, error) {
	switch kind {
	case TypeCodeChange:
		return fmt.Sprintf("You are CodeGateway's coding worker powered by %s. Work only inside the %q workspace. Use available workspace tools to inspect and implement requested code changes. Do not access paths outside the workspace. Summarize completed changes and verification.", modelName, workspaceName), nil
	case TypeDocumentation:
		return fmt.Sprintf("You are CodeGateway's documentation worker powered by %s. Work only inside the %q workspace. Use available workspace tools to inspect the project and write or update accurate documentation for the requested work. Do not access paths outside the workspace. Summarize completed documentation changes and verification.", modelName, workspaceName), nil
	default:
		return "", fmt.Errorf("%w: unsupported task type", ErrInvalid)
	}
}

func sanitizeToolSteps(steps []map[string]string) []map[string]string {
	if steps == nil {
		return make([]map[string]string, 0)
	}
	clean := make([]map[string]string, 0, len(steps))
	for _, step := range steps {
		entry := make(map[string]string, len(step))
		for key, value := range step {
			if sensitiveField(key) {
				entry[key] = "[REDACTED]"
				continue
			}
			entry[key] = sanitizeTaskText(value)
		}
		clean = append(clean, entry)
	}
	return clean
}

func sensitiveField(key string) bool {
	key = strings.ToLower(key)
	return strings.Contains(key, "key") || strings.Contains(key, "token") || strings.Contains(key, "secret") || strings.Contains(key, "authorization") || strings.Contains(key, "password")
}

var secretValue = regexp.MustCompile(`(?i)(authorization\s*[:=]\s*(?:bearer\s+)?|(?:api[_-]?key|access[_-]?token|secret|password)["']?\s*[:=]\s*["']?)([^\s,;"}]+)`)

func sanitizeTaskText(text string) string {
	return secretValue.ReplaceAllString(text, "$1[REDACTED]")
}
