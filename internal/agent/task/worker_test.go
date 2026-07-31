package task

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alex/codegateway/internal/provider"
	"github.com/alex/codegateway/internal/workspace"
)

type fakeWorkerProvider struct{}

func (fakeWorkerProvider) Name() string { return "fake" }
func (fakeWorkerProvider) ChatCompletion(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
	return nil, errors.New("not used")
}
func (fakeWorkerProvider) ChatCompletionStream(context.Context, *provider.ChatCompletionRequest) (<-chan *provider.ChatCompletionChunk, error) {
	return nil, errors.New("not used")
}
func (fakeWorkerProvider) ListModels(context.Context) ([]string, error) { return nil, errors.New("not used") }
func (fakeWorkerProvider) ValidateModel(string) bool                 { return true }

func TestWorkerCompletesClaimedTaskUsingOwnedWorkspaceAndCodeChangePrompt(t *testing.T) {
	store := openAgentTaskStore(t)
	queued, err := store.Create(7, CreateInput{WorkspaceID: "workspace-7", RouteProfileID: 31, Type: TypeCodeChange, Prompt: "Add a health endpoint."})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	owned := &workspace.Workspace{ID: queued.WorkspaceID, UserID: queued.UserID, Name: "gateway", RootPath: t.TempDir()}
	var refreshed bool
	worker := NewWorker(WorkerConfig{
		Store: store,
		WorkspaceForTask: func(userID int64, workspaceID string) (*workspace.Workspace, error) {
			if userID != queued.UserID || workspaceID != queued.WorkspaceID {
				t.Fatalf("workspace lookup = (%d, %q), want task ownership", userID, workspaceID)
			}
			return owned, nil
		},
		ProviderForTask: func(task *Task) (provider.Provider, string, error) {
			if task.RouteProfileID != 31 {
				t.Fatalf("profile id = %d, want 31", task.RouteProfileID)
			}
			return fakeWorkerProvider{}, "gpt-5", nil
		},
		Run: func(_ context.Context, _ provider.Provider, modelName, systemPrompt, prompt string, ws *workspace.Workspace) (string, []map[string]string, error) {
			if ws != owned {
				t.Fatal("runner did not receive the owned workspace")
			}
			if modelName != "gpt-5" || prompt != queued.Prompt || !strings.Contains(systemPrompt, "implement requested code changes") {
				t.Fatalf("runner input = model=%q system=%q prompt=%q", modelName, systemPrompt, prompt)
			}
			return "health endpoint added", []map[string]string{{"tool": "write_file", "args": `{"api_key":"secret-value"}`, "result": "Authorization: Bearer secret-value"}}, nil
		},
		RefreshStats: func(ws *workspace.Workspace) error {
			if ws != owned {
				t.Fatal("refreshed a workspace other than the owned workspace")
			}
			refreshed = true
			return nil
		},
	})

	processed, err := worker.ProcessNext(context.Background())
	if err != nil || !processed {
		t.Fatalf("process next = (%t, %v), want claimed task completed", processed, err)
	}
	if !refreshed {
		t.Fatal("workspace stats were not refreshed")
	}
	got, err := store.Get(queued.UserID, queued.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Status != StatusSucceeded || got.Result != "health endpoint added" || got.FinishedAt == nil {
		t.Fatalf("completed task = %#v", got)
	}
	if serialized := got.ToolSteps[0]["args"] + got.ToolSteps[0]["result"]; strings.Contains(serialized, "secret-value") {
		t.Fatalf("credentials leaked into persisted tool steps: %#v", got.ToolSteps)
	}
}

func TestWorkerFailsTaskWhenOwnedWorkspaceCannotBeLoaded(t *testing.T) {
	store := openAgentTaskStore(t)
	queued, err := store.Create(4, CreateInput{WorkspaceID: "workspace-4", RouteProfileID: 12, Type: TypeDocumentation, Prompt: "Document the API."})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	worker := NewWorker(WorkerConfig{
		Store: store,
		WorkspaceForTask: func(int64, string) (*workspace.Workspace, error) {
			return nil, errors.New("workspace not found; api_key=secret-value")
		},
		ProviderForTask: func(*Task) (provider.Provider, string, error) {
			return fakeWorkerProvider{}, "gpt-5", nil
		},
		Run: func(context.Context, provider.Provider, string, string, string, *workspace.Workspace) (string, []map[string]string, error) {
			t.Fatal("runner must not be reached when workspace load fails")
			return "", nil, nil
		},
	})

	processed, err := worker.ProcessNext(context.Background())
	if err != nil || !processed {
		t.Fatalf("process next = (%t, %v), want failed task persisted", processed, err)
	}
	got, err := store.Get(queued.UserID, queued.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Status != StatusFailed || got.FinishedAt == nil || !strings.Contains(got.Error, "workspace not found") {
		t.Fatalf("failed task = %#v", got)
	}
	if strings.Contains(got.Error, "secret-value") {
		t.Fatalf("credential leaked into task error: %q", got.Error)
	}
}

func TestTaskSystemPromptIsSpecificToDocumentationWork(t *testing.T) {
	prompt, err := TaskSystemPrompt(TypeDocumentation, "gpt-5", "gateway")
	if err != nil {
		t.Fatalf("documentation prompt: %v", err)
	}
	if !strings.Contains(prompt, "documentation") || strings.Contains(prompt, "implement requested code changes") {
		t.Fatalf("documentation prompt = %q", prompt)
	}
}
