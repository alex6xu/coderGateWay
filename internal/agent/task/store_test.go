package task

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openAgentTaskStore(t *testing.T) *Store {
	t.Helper()
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "agent-tasks.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.Exec(`
		CREATE TABLE agent_tasks (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			workspace_id TEXT NOT NULL,
			route_profile_id INTEGER NOT NULL,
			type TEXT NOT NULL,
			prompt TEXT NOT NULL,
			status TEXT NOT NULL,
			result TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			tool_steps TEXT NOT NULL DEFAULT '[]',
			created_at DATETIME NOT NULL,
			started_at DATETIME,
			finished_at DATETIME
		)
	`); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return NewStore(database)
}

func TestStoreCreatesListsAndGetsOnlyOwnedTasks(t *testing.T) {
	store := openAgentTaskStore(t)
	created, err := store.Create(7, CreateInput{
		WorkspaceID:    "workspace-7",
		RouteProfileID: 31,
		Type:           TypeCodeChange,
		Prompt:         "Add a health check.",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if created.Status != StatusQueued {
		t.Fatalf("status = %q, want queued", created.Status)
	}
	if created.ID == "" {
		t.Fatal("created task has no ID")
	}

	if _, err := store.Create(8, CreateInput{WorkspaceID: "workspace-8", RouteProfileID: 32, Type: TypeDocumentation, Prompt: "Document the API."}); err != nil {
		t.Fatalf("create other task: %v", err)
	}
	list, err := store.List(7)
	if err != nil {
		t.Fatalf("list own tasks: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("list = %#v, want only %q", list, created.ID)
	}
	got, err := store.Get(7, created.ID)
	if err != nil || got.Prompt != "Add a health check." {
		t.Fatalf("get own task = %#v, %v", got, err)
	}
	if _, err := store.Get(8, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get another account's task error = %v, want ErrNotFound", err)
	}
}

func TestStoreRejectsUnsupportedTaskTypes(t *testing.T) {
	store := openAgentTaskStore(t)
	for _, kind := range []Type{"", "chat", "Code_Change", "documentation "} {
		_, err := store.Create(1, CreateInput{WorkspaceID: "workspace", RouteProfileID: 1, Type: kind, Prompt: "Do work"})
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("type %q error = %v, want ErrInvalid", kind, err)
		}
	}
}

func TestStoreClaimsAQueuedTaskOnlyOnce(t *testing.T) {
	store := openAgentTaskStore(t)
	queued, err := store.Create(1, CreateInput{WorkspaceID: "workspace", RouteProfileID: 1, Type: TypeCodeChange, Prompt: "Do work"})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	var wg sync.WaitGroup
	claimed := make(chan *Task, 2)
	claimErrors := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task, err := store.ClaimNext()
			if err != nil {
				claimErrors <- err
				return
			}
			claimed <- task
		}()
	}
	wg.Wait()
	close(claimed)
	close(claimErrors)

	var claims []*Task
	for got := range claimed {
		claims = append(claims, got)
	}
	if len(claims) != 1 || claims[0].ID != queued.ID || claims[0].Status != StatusRunning {
		t.Fatalf("claims = %#v, want queued task once and running", claims)
	}
	for err := range claimErrors {
		if !errors.Is(err, ErrNoQueuedTask) {
			t.Fatalf("second claim error = %v, want ErrNoQueuedTask", err)
		}
	}
}

func TestStoreRecoversInterruptedRunningTasks(t *testing.T) {
	store := openAgentTaskStore(t)
	queued, err := store.Create(1, CreateInput{WorkspaceID: "workspace", RouteProfileID: 1, Type: TypeDocumentation, Prompt: "Write docs"})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := store.ClaimNext(); err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if _, err := store.Create(1, CreateInput{WorkspaceID: "other-workspace", RouteProfileID: 1, Type: TypeCodeChange, Prompt: "Keep queued"}); err != nil {
		t.Fatalf("create queued task: %v", err)
	}

	recovered, err := store.RecoverInterrupted()
	if err != nil {
		t.Fatalf("recover interrupted tasks: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}
	got, err := store.Get(1, queued.ID)
	if err != nil {
		t.Fatalf("get recovered task: %v", err)
	}
	if got.Status != StatusFailed || got.Error == "" || got.FinishedAt == nil {
		t.Fatalf("recovered task = %#v, want failed task with error and finished time", got)
	}
}

func TestStoreListsNewestTasksFirst(t *testing.T) {
	store := openAgentTaskStore(t)
	first, err := store.Create(1, CreateInput{WorkspaceID: "one", RouteProfileID: 1, Type: TypeCodeChange, Prompt: "First"})
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	time.Sleep(time.Millisecond)
	second, err := store.Create(1, CreateInput{WorkspaceID: "two", RouteProfileID: 1, Type: TypeDocumentation, Prompt: "Second"})
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	list, err := store.List(1)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(list) != 2 || list[0].ID != second.ID || list[1].ID != first.ID {
		t.Fatalf("list IDs = %#v, want [%q %q]", []string{list[0].ID, list[1].ID}, second.ID, first.ID)
	}
}

func TestStorePersistsTerminalCompletionAndFailureForClaimedTasks(t *testing.T) {
	store := openAgentTaskStore(t)
	succeeded, err := store.Create(1, CreateInput{WorkspaceID: "one", RouteProfileID: 1, Type: TypeCodeChange, Prompt: "Change code"})
	if err != nil {
		t.Fatalf("create succeeded task: %v", err)
	}
	if _, err := store.ClaimNext(); err != nil {
		t.Fatalf("claim succeeded task: %v", err)
	}
	steps := []map[string]string{{"tool": "write_file", "args": `{"path":"README.md"}`, "result": "updated"}}
	if err := store.Complete(succeeded.ID, "updated README", steps); err != nil {
		t.Fatalf("complete task: %v", err)
	}

	got, err := store.Get(1, succeeded.ID)
	if err != nil {
		t.Fatalf("get completed task: %v", err)
	}
	if got.Status != StatusSucceeded || got.Result != "updated README" || got.Error != "" || got.FinishedAt == nil {
		t.Fatalf("completed task = %#v, want succeeded terminal record", got)
	}
	if !reflect.DeepEqual(got.ToolSteps, steps) {
		t.Fatalf("tool steps = %#v, want %#v", got.ToolSteps, steps)
	}

	failed, err := store.Create(1, CreateInput{WorkspaceID: "two", RouteProfileID: 1, Type: TypeDocumentation, Prompt: "Write docs"})
	if err != nil {
		t.Fatalf("create failed task: %v", err)
	}
	if _, err := store.ClaimNext(); err != nil {
		t.Fatalf("claim failed task: %v", err)
	}
	if err := store.Fail(failed.ID, "provider unavailable", nil); err != nil {
		t.Fatalf("fail task: %v", err)
	}
	got, err = store.Get(1, failed.ID)
	if err != nil {
		t.Fatalf("get failed task: %v", err)
	}
	if got.Status != StatusFailed || got.Error != "provider unavailable" || got.Result != "" || got.FinishedAt == nil {
		t.Fatalf("failed task = %#v, want failed terminal record", got)
	}
}
