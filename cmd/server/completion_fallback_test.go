package server

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/alex/codegateway/internal/db"
	"github.com/alex/codegateway/internal/gateway/profile"
	"github.com/alex/codegateway/internal/model"
	"github.com/alex/codegateway/internal/provider"
	_ "modernc.org/sqlite"
)

func TestResolveChatCompletionCandidatesUsesOwnedProfileInDeclaredOrder(t *testing.T) {
	database := openCompletionTestDB(t)
	seedCompletionSchema(t, database)
	seedCompletionChannel(t, database, 1, "first", "model-first")
	seedCompletionChannel(t, database, 1, "second", "model-second")
	seedCompletionChannel(t, database, 2, "other-account", "model-other")

	profiles := profile.NewStore(database.DB)
	if _, err := profiles.Create(1, profile.CreateInput{
		Name: "coding-route", Purpose: profile.PurposeCoding, Models: []string{"model-first", "model-second"},
	}); err != nil {
		t.Fatalf("create profile: %v", err)
	}
	if _, err := profiles.Create(2, profile.CreateInput{
		Name: "other-route", Purpose: profile.PurposeCoding, Models: []string{"model-other"},
	}); err != nil {
		t.Fatalf("create other profile: %v", err)
	}

	candidates, err := resolveChatCompletionCandidates(database, 1, "coding-route")
	if err != nil {
		t.Fatalf("resolve profile candidates: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("candidate count=%d want 2", len(candidates))
	}
	if candidates[0].model != "model-first" || candidates[1].model != "model-second" {
		t.Fatalf("candidate models=%q, %q want declared order", candidates[0].model, candidates[1].model)
	}
	if candidates[0].channel.Name != "first" || candidates[1].channel.Name != "second" {
		t.Fatalf("candidate channels=%q, %q want account-owned channels", candidates[0].channel.Name, candidates[1].channel.Name)
	}

	direct, err := resolveChatCompletionCandidates(database, 1, "model-first")
	if err != nil {
		t.Fatalf("resolve direct model: %v", err)
	}
	if len(direct) != 1 || direct[0].model != "model-first" || direct[0].channel.Name != "first" {
		t.Fatalf("direct candidate=%#v want one unchanged model-first candidate", direct)
	}

	if _, err := resolveChatCompletionCandidates(database, 1, "other-route"); err == nil {
		t.Fatal("other account's route profile must not resolve")
	}
}

func TestResolveAgentTaskProviderUsesFirstEligibleOwnedProfileCandidate(t *testing.T) {
	database := openCompletionTestDB(t)
	seedCompletionSchema(t, database)
	seedCompletionChannel(t, database, 1, "first", "model-first")
	seedCompletionChannel(t, database, 1, "second", "model-second")
	profiles := profile.NewStore(database.DB)
	selected, err := profiles.Create(1, profile.CreateInput{
		Name: "task-route", Purpose: profile.PurposeCoding, Models: []string{"model-first", "model-second"},
	})
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}

	prov, modelName, err := resolveAgentTaskProvider(database, 1, selected.ID)
	if err != nil || prov == nil || modelName != "model-first" {
		t.Fatalf("resolve task provider = (%T, %q, %v), want first candidate", prov, modelName, err)
	}
	if _, _, err := resolveAgentTaskProvider(database, 2, selected.ID); err == nil {
		t.Fatal("another account must not resolve a task profile")
	}
}

func TestCompleteWithCandidatesRetriesNextCandidateAndReturnsSuccessfulModel(t *testing.T) {
	resetBreakers()
	first := &model.Channel{ID: 1, Name: "first"}
	second := &model.Channel{ID: 2, Name: "second"}
	candidates := []chatCompletionCandidate{
		{model: "model-first", channel: first},
		{model: "model-second", channel: second},
	}

	var attempted []string
	response, selected, err := completeWithCandidates(context.Background(), candidates, &provider.ChatCompletionRequest{}, func(channel *model.Channel) (provider.Provider, error) {
		attempted = append(attempted, channel.Name)
		if channel.ID == first.ID {
			return completionTestProvider{err: errors.New("first provider failed")}, nil
		}
		return completionTestProvider{response: &provider.ChatCompletionResponse{Model: "upstream-model"}}, nil
	})
	if err != nil {
		t.Fatalf("complete with fallback: %v", err)
	}
	if len(attempted) != 2 || attempted[0] != "first" || attempted[1] != "second" {
		t.Fatalf("attempted=%#v want [first second]", attempted)
	}
	if selected.model != "model-second" || selected.channel != second {
		t.Fatalf("selected=%#v want second candidate", selected)
	}
	if response.Model != "model-second" {
		t.Fatalf("response model=%q want successful candidate model", response.Model)
	}
}

func TestFirstStreamCandidateNeverFallsBack(t *testing.T) {
	first := chatCompletionCandidate{model: "model-first", channel: &model.Channel{ID: 1, Name: "first"}}
	second := chatCompletionCandidate{model: "model-second", channel: &model.Channel{ID: 2, Name: "second"}}
	selected := firstStreamCandidate([]chatCompletionCandidate{first, second})
	if selected.channel != first.channel || selected.model != first.model {
		t.Fatalf("stream selected %#v, want first candidate %#v", selected, first)
	}
}

func TestCompleteWithCandidatesContinuesAfterProviderConstructionError(t *testing.T) {
	resetBreakers()
	first := &model.Channel{ID: 1, Name: "first"}
	second := &model.Channel{ID: 2, Name: "second"}
	var attempted []string
	response, selected, err := completeWithCandidates(context.Background(), []chatCompletionCandidate{
		{model: "model-first", channel: first},
		{model: "model-second", channel: second},
	}, &provider.ChatCompletionRequest{}, func(channel *model.Channel) (provider.Provider, error) {
		attempted = append(attempted, channel.Name)
		if channel == first {
			return nil, errors.New("first provider cannot be created")
		}
		return completionTestProvider{response: &provider.ChatCompletionResponse{}}, nil
	})
	if err != nil {
		t.Fatalf("complete with constructor fallback: %v", err)
	}
	if response == nil || selected.channel != second || len(attempted) != 2 {
		t.Fatalf("response=%#v selected=%#v attempted=%#v", response, selected, attempted)
	}
}

func openCompletionTestDB(t *testing.T) *db.DB {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "completion.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return &db.DB{DB: sqlDB}
}

func seedCompletionSchema(t *testing.T, database *db.DB) {
	t.Helper()
	_, err := database.Exec(`
		CREATE TABLE providers (
			id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL, name TEXT NOT NULL,
			type INTEGER NOT NULL, key TEXT NOT NULL, base_url TEXT, models TEXT,
			weight INTEGER DEFAULT 1, priority INTEGER DEFAULT 0, status INTEGER DEFAULT 1,
			is_default INTEGER DEFAULT 0, auth_mode TEXT DEFAULT 'api_key'
		);
		CREATE TABLE route_profiles (
			id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL, name TEXT NOT NULL,
			purpose TEXT NOT NULL, models TEXT NOT NULL, created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL, UNIQUE(user_id, name)
		);
	`)
	if err != nil {
		t.Fatalf("create completion test schema: %v", err)
	}
}

func seedCompletionChannel(t *testing.T, database *db.DB, userID int64, name, modelName string) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO providers (user_id, name, type, key, models, status) VALUES (?, ?, 1, 'test-key', ?, 1)`, userID, name, `["`+modelName+`"]`); err != nil {
		t.Fatalf("insert channel %q: %v", name, err)
	}
}

type completionTestProvider struct {
	response *provider.ChatCompletionResponse
	err      error
}

func (p completionTestProvider) Name() string { return "test" }
func (p completionTestProvider) ChatCompletion(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
	return p.response, p.err
}
func (p completionTestProvider) ChatCompletionStream(context.Context, *provider.ChatCompletionRequest) (<-chan *provider.ChatCompletionChunk, error) {
	return nil, errors.New("not used")
}
func (p completionTestProvider) ListModels(context.Context) ([]string, error) { return nil, nil }
func (p completionTestProvider) ValidateModel(string) bool                 { return true }
