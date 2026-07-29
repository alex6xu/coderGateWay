package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/alex/codegateway/internal/account"
	agenttask "github.com/alex/codegateway/internal/agent/task"
	"github.com/alex/codegateway/internal/db"
	"github.com/alex/codegateway/internal/gateway/profile"
	"github.com/gin-gonic/gin"
	_ "modernc.org/sqlite"
)

func TestCreateAgentTaskReturnsAcceptedOnlyForOwnedWorkspaceAndProfile(t *testing.T) {
	database := openAgentTaskTestDB(t)
	seedAgentTaskSchema(t, database)
	seedAgentTaskWorkspace(t, database, "owned-workspace", 7)
	seedAgentTaskWorkspace(t, database, "other-workspace", 8)
	profiles := profile.NewStore(database.DB)
	ownedProfile, err := profiles.Create(7, profile.CreateInput{Name: "owned-profile", Purpose: profile.PurposeCoding, Models: []string{"model-a"}})
	if err != nil {
		t.Fatalf("create owned profile: %v", err)
	}
	if _, err := profiles.Create(8, profile.CreateInput{Name: "other-profile", Purpose: profile.PurposeCoding, Models: []string{"model-b"}}); err != nil {
		t.Fatalf("create other profile: %v", err)
	}

	response := runAgentTaskHandler(t, handleCreateAgentTask(database), http.MethodPost, "/v1/agent/tasks", 7, `{"workspace_id":"owned-workspace","route_profile":"owned-profile","type":"code_change","prompt":"Add a health check."}`)
	if response.Code != http.StatusAccepted {
		t.Fatalf("create status = %d, body = %s", response.Code, response.Body.String())
	}
	var body struct{ Task agenttask.Task `json:"task"` }
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if body.Task.Status != agenttask.StatusQueued || body.Task.RouteProfileID != ownedProfile.ID {
		t.Fatalf("created task = %#v, want queued task for owned profile", body.Task)
	}

	for _, payload := range []string{
		`{"workspace_id":"other-workspace","route_profile":"owned-profile","type":"code_change","prompt":"Do work"}`,
		`{"workspace_id":"owned-workspace","route_profile":"other-profile","type":"code_change","prompt":"Do work"}`,
	} {
		response := runAgentTaskHandler(t, handleCreateAgentTask(database), http.MethodPost, "/v1/agent/tasks", 7, payload)
		if response.Code != http.StatusNotFound {
			t.Fatalf("cross-account resource status = %d, body = %s", response.Code, response.Body.String())
		}
	}
}

func TestAgentTaskListAndGetDoNotExposeAnotherAccountsTasks(t *testing.T) {
	database := openAgentTaskTestDB(t)
	seedAgentTaskSchema(t, database)
	store := agenttask.NewStore(database.DB)
	owned, err := store.Create(7, agenttask.CreateInput{WorkspaceID: "owned-workspace", RouteProfileID: 1, Type: agenttask.TypeDocumentation, Prompt: "Document API"})
	if err != nil {
		t.Fatalf("create owned task: %v", err)
	}
	other, err := store.Create(8, agenttask.CreateInput{WorkspaceID: "other-workspace", RouteProfileID: 2, Type: agenttask.TypeCodeChange, Prompt: "Change code"})
	if err != nil {
		t.Fatalf("create other task: %v", err)
	}

	list := runAgentTaskHandler(t, handleListAgentTasks(database), http.MethodGet, "/v1/agent/tasks", 7, "")
	if list.Code != http.StatusOK || !bytes.Contains(list.Body.Bytes(), []byte(owned.ID)) || bytes.Contains(list.Body.Bytes(), []byte(other.ID)) {
		t.Fatalf("list status/body = %d/%s", list.Code, list.Body.String())
	}
	getOwn := runAgentTaskHandler(t, handleGetAgentTask(database), http.MethodGet, "/v1/agent/tasks/"+owned.ID, 7, "")
	if getOwn.Code != http.StatusOK {
		t.Fatalf("get own status = %d, body = %s", getOwn.Code, getOwn.Body.String())
	}
	getOther := runAgentTaskHandler(t, handleGetAgentTask(database), http.MethodGet, "/v1/agent/tasks/"+other.ID, 7, "")
	if getOther.Code != http.StatusNotFound {
		t.Fatalf("get another account status = %d, body = %s", getOther.Code, getOther.Body.String())
	}
}

func openAgentTaskTestDB(t *testing.T) *db.DB {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "agent-tasks.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return &db.DB{DB: sqlDB}
}

func seedAgentTaskSchema(t *testing.T, database *db.DB) {
	t.Helper()
	if _, err := database.Exec(`
		CREATE TABLE workspaces (id TEXT PRIMARY KEY, user_id INTEGER NOT NULL);
		CREATE TABLE route_profiles (
			id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL, name TEXT NOT NULL,
			purpose TEXT NOT NULL, models TEXT NOT NULL, created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL, UNIQUE(user_id, name)
		);
		CREATE TABLE agent_tasks (
			id TEXT PRIMARY KEY, user_id INTEGER NOT NULL, workspace_id TEXT NOT NULL, route_profile_id INTEGER NOT NULL,
			type TEXT NOT NULL, prompt TEXT NOT NULL, status TEXT NOT NULL, result TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '', tool_steps TEXT NOT NULL DEFAULT '[]', created_at DATETIME NOT NULL,
			started_at DATETIME, finished_at DATETIME
		);
	`); err != nil {
		t.Fatalf("create agent task schema: %v", err)
	}
}

func seedAgentTaskWorkspace(t *testing.T, database *db.DB, id string, userID int64) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO workspaces (id, user_id) VALUES (?, ?)`, id, userID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
}

func runAgentTaskHandler(t *testing.T, handler gin.HandlerFunc, method, path string, userID int64, payload string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, bytes.NewBufferString(payload))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(account.ContextKey, userID)
	if method == http.MethodGet && len(path) > len("/v1/agent/tasks/") {
		c.Params = gin.Params{{Key: "id", Value: path[len("/v1/agent/tasks/"):]}}
	}
	handler(c)
	return w
}
