package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"testing"

	"github.com/alex/codegateway/internal/db"
	"github.com/alex/codegateway/internal/gatewaylog"
	"github.com/alex/codegateway/internal/model"
	"github.com/alex/codegateway/internal/provider"
	_ "modernc.org/sqlite"
)

type stubLogProvider struct {
	resp *provider.ChatCompletionResponse
	err  error
}

func (s stubLogProvider) Name() string { return "stub" }
func (s stubLogProvider) ListModels(context.Context) ([]string, error) {
	return nil, nil
}
func (s stubLogProvider) ValidateModel(string) bool { return true }
func (s stubLogProvider) ChatCompletion(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
	return s.resp, s.err
}
func (s stubLogProvider) ChatCompletionStream(context.Context, *provider.ChatCompletionRequest) (<-chan *provider.ChatCompletionChunk, error) {
	return nil, errors.New("no stream")
}

func openGatewayLogTestDB(t *testing.T) *db.DB {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if _, err := sqlDB.Exec(`
		CREATE TABLE gateway_request_logs (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			channel_id INTEGER,
			channel_name TEXT,
			model TEXT,
			stream INTEGER NOT NULL DEFAULT 0,
			status_code INTEGER NOT NULL,
			error TEXT,
			request_body TEXT,
			response_body TEXT,
			prompt_tokens INTEGER NOT NULL DEFAULT 0,
			completion_tokens INTEGER NOT NULL DEFAULT 0,
			cached_tokens INTEGER NOT NULL DEFAULT 0,
			latency_ms INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL
		)
	`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	return &db.DB{DB: sqlDB}
}

func TestLoggingProviderRecordsChatCompletion(t *testing.T) {
	database := openGatewayLogTestDB(t)
	channel := &model.Channel{ID: 7, Name: "test-channel"}
	inner := stubLogProvider{
		resp: &provider.ChatCompletionResponse{
			ID:    "cmpl-1",
			Model: "gpt-test",
			Usage: provider.Usage{PromptTokens: 3, CompletionTokens: 5},
		},
	}
	prov := wrapProviderWithRequestLog(database, 42, channel, inner)

	_, err := prov.ChatCompletion(context.Background(), &provider.ChatCompletionRequest{
		Model:    "gpt-test",
		Messages: []provider.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}

	logs, err := gatewaylog.NewStore(database.DB).List(42, gatewaylog.ListFilter{Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
	if logs[0].StatusCode != http.StatusOK {
		t.Fatalf("status=%d", logs[0].StatusCode)
	}
	if logs[0].ChannelName != "test-channel" || logs[0].Model != "gpt-test" {
		t.Fatalf("unexpected log: %+v", logs[0])
	}
	if logs[0].PromptTokens != 3 || logs[0].CompletionTokens != 5 {
		t.Fatalf("tokens=%d/%d", logs[0].PromptTokens, logs[0].CompletionTokens)
	}
}

func TestLoggingProviderRecordsErrors(t *testing.T) {
	database := openGatewayLogTestDB(t)
	channel := &model.Channel{ID: 1, Name: "bad"}
	prov := wrapProviderWithRequestLog(database, 9, channel, stubLogProvider{err: errors.New("upstream failed")})

	_, err := prov.ChatCompletion(context.Background(), &provider.ChatCompletionRequest{Model: "m"})
	if err == nil {
		t.Fatal("expected error")
	}
	logs, err := gatewaylog.NewStore(database.DB).List(9, gatewaylog.ListFilter{Limit: 5})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(logs) != 1 || logs[0].StatusCode != http.StatusInternalServerError {
		t.Fatalf("unexpected logs: %+v", logs)
	}
	if logs[0].Error != "upstream failed" {
		t.Fatalf("error=%q", logs[0].Error)
	}
}
