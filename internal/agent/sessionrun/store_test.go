package sessionrun

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/alex/codegateway/internal/config"
	"github.com/alex/codegateway/internal/db"
)

func openStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Init(config.DatabaseConfig{Driver: "sqlite", DSN: filepath.Join(dir, "t.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	_, err = database.Exec(`INSERT INTO users (id, username, password_hash, role, created_at, updated_at) VALUES (1, 'u', 'x', 'admin', ?, ?)`, time.Now(), time.Now())
	if err != nil {
		// users table may already have different schema; try minimal
		_, _ = database.Exec(`INSERT OR IGNORE INTO users (id, username, password_hash, role) VALUES (1, 'u', 'x', 'admin')`)
	}
	_, _ = database.Exec(`INSERT INTO sessions (id, user_id, title, platform, message_count, created_at, updated_at) VALUES ('s1', 1, 't', 'web', 0, ?, ?)`, time.Now(), time.Now())
	_, _ = database.Exec(`INSERT INTO messages (id, session_id, role, content, created_at) VALUES ('m1', 's1', 'user', 'hi', ?)`, time.Now())
	return NewStore(database.DB)
}

func TestAppendEventSeqMonotonic(t *testing.T) {
	st := openStore(t)
	run, err := st.CreateQueued(CreateRunInput{SessionID: "s1", UserID: 1, Mode: "coder", TriggerMessageID: "m1"})
	if err != nil {
		t.Fatal(err)
	}
	e1, err := st.AppendEvent(run.ID, EventDelta, map[string]string{"content": "a"})
	if err != nil {
		t.Fatal(err)
	}
	e2, err := st.AppendEvent(run.ID, EventDelta, map[string]string{"content": "b"})
	if err != nil {
		t.Fatal(err)
	}
	if e1.Seq != 1 || e2.Seq != 2 {
		t.Fatalf("seq got %d %d", e1.Seq, e2.Seq)
	}
	list, err := st.ListEventsAfter(run.ID, 0)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %v len=%d", err, len(list))
	}
}

func TestActiveRunAndInboxDrain(t *testing.T) {
	st := openStore(t)
	run, err := st.CreateQueued(CreateRunInput{SessionID: "s1", UserID: 1, Mode: "chat", TriggerMessageID: "m1"})
	if err != nil {
		t.Fatal(err)
	}
	active, err := st.ActiveRunForSession("s1")
	if err != nil || active == nil || active.ID != run.ID {
		t.Fatalf("active: %+v %v", active, err)
	}
	_, err = st.CreateQueued(CreateRunInput{SessionID: "s1", UserID: 1, Mode: "chat", TriggerMessageID: "m1"})
	if err != ErrActiveExists {
		t.Fatalf("expected ErrActiveExists, got %v", err)
	}

	_, _ = st.db.Exec(`INSERT INTO messages (id, session_id, role, content, created_at) VALUES ('m2', 's1', 'user', 'more', ?)`, time.Now())
	_, err = st.EnqueueInbox("s1", run.ID, "m2", "more")
	if err != nil {
		t.Fatal(err)
	}
	items, err := st.DrainPendingForRun(run.ID)
	if err != nil || len(items) != 1 || items[0].Content != "more" {
		t.Fatalf("drain: %+v %v", items, err)
	}
	items2, err := st.DrainPendingForRun(run.ID)
	if err != nil || len(items2) != 0 {
		t.Fatalf("second drain should be empty: %+v", items2)
	}
}

func TestRecoverInterrupted(t *testing.T) {
	st := openStore(t)
	run, err := st.CreateQueued(CreateRunInput{SessionID: "s1", UserID: 1, Mode: "chat", TriggerMessageID: "m1"})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := st.ClaimNext()
	if err != nil || claimed.ID != run.ID {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	n, err := st.RecoverInterrupted()
	if err != nil || n != 1 {
		t.Fatalf("recover n=%d err=%v", n, err)
	}
	got, err := st.GetByID(run.ID)
	if err != nil || got.Status != StatusFailed {
		t.Fatalf("got %+v %v", got, err)
	}
}
