package gatewaylog

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`
		CREATE TABLE users (id INTEGER PRIMARY KEY);
		INSERT INTO users (id) VALUES (1), (2);
		CREATE TABLE gateway_request_logs (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			provider_id INTEGER,
			provider_name TEXT DEFAULT '',
			model TEXT DEFAULT '',
			stream INTEGER DEFAULT 0,
			status_code INTEGER DEFAULT 0,
			error TEXT DEFAULT '',
			request_body TEXT DEFAULT '',
			response_body TEXT DEFAULT '',
			prompt_tokens INTEGER DEFAULT 0,
			completion_tokens INTEGER DEFAULT 0,
			cached_tokens INTEGER DEFAULT 0,
			latency_ms INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestInsertListGetIsolation(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)

	e := &Entry{
		UserID:       1,
		ProviderID:    10,
		ProviderName:  "claude",
		Model:        "claude-sonnet-4",
		Stream:       false,
		StatusCode:   200,
		RequestBody:  `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hi"}],"api_key":"secret"}`,
		ResponseBody: `{"id":"r1","choices":[{"message":{"role":"assistant","content":"hello"}}]}`,
		PromptTokens: 5,
		CompletionTokens: 3,
		LatencyMs:    42,
	}
	if err := s.Insert(e); err != nil {
		t.Fatal(err)
	}
	if e.ID == "" {
		t.Fatal("expected id assigned")
	}
	if strings.Contains(e.RequestBody, "secret") {
		t.Fatalf("api_key should be redacted: %s", e.RequestBody)
	}
	if !strings.Contains(e.RequestBody, "****") {
		t.Fatalf("expected redaction marker: %s", e.RequestBody)
	}

	list, err := s.List(1, ListFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list len=%d", len(list))
	}
	if list[0].RequestBody != "" || list[0].ResponseBody != "" {
		t.Fatal("list should omit bodies")
	}

	other, err := s.List(2, ListFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Fatal("account 2 must not see account 1 logs")
	}

	got, err := s.Get(1, e.ID)
	if err != nil || got == nil {
		t.Fatalf("get: %v %#v", err, got)
	}
	if got.ResponseBody == "" || got.RequestBody == "" {
		t.Fatal("detail should include bodies")
	}
	missing, err := s.Get(2, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Fatal("cross-account get must return nil")
	}
}

func TestTruncateBody(t *testing.T) {
	big := strings.Repeat("a", maxBodyBytes+100)
	out := truncateBody(big)
	if len(out) <= maxBodyBytes {
		t.Fatalf("expected truncation marker appended, len=%d", len(out))
	}
	if !strings.HasSuffix(out, "/* truncated */") {
		t.Fatalf("missing marker: %q", out[len(out)-20:])
	}
}

func TestListFilterModelAndStatus(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	_ = s.Insert(&Entry{UserID: 1, Model: "m1", StatusCode: 200, RequestBody: `{}`})
	_ = s.Insert(&Entry{UserID: 1, Model: "m2", StatusCode: 500, RequestBody: `{}`, Error: "fail"})

	list, err := s.List(1, ListFilter{Model: "m2", StatusCode: 500, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Model != "m2" {
		t.Fatalf("%#v", list)
	}
}
