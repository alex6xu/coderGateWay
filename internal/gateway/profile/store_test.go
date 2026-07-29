package profile


import (
    "database/sql"
	"errors"
    "path/filepath"
    "reflect"
    "testing"

    _ "modernc.org/sqlite"
)

func openStore(t *testing.T) *Store {
    t.Helper()
    db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "profiles.db"))
    if err != nil {
        t.Fatalf("open database: %v", err)
    }
    t.Cleanup(func() { _ = db.Close() })
    if _, err := db.Exec(`
        CREATE TABLE route_profiles (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            user_id INTEGER NOT NULL,
            name TEXT NOT NULL,
            purpose TEXT NOT NULL,
            models TEXT NOT NULL,
            created_at DATETIME NOT NULL,
            updated_at DATETIME NOT NULL,
            UNIQUE(user_id, name)
        )
    `); err != nil {
        t.Fatalf("create schema: %v", err)
    }
    return NewStore(db)
}

func TestCreateNormalizesModelsAndScopesProfilesToAccount(t *testing.T) {
    store := openStore(t)

    created, err := store.Create(7, CreateInput{
        Name:    " coding-auto ",
        Purpose: PurposeCoding,
        Models:  []string{"gpt-5", " gpt-5 ", "claude-code"},
    })
    if err != nil {
        t.Fatalf("create profile: %v", err)
    }
    if created.Name != "coding-auto" {
        t.Fatalf("name = %q", created.Name)
    }
    if want := []string{"gpt-5", "claude-code"}; !reflect.DeepEqual(created.Models, want) {
        t.Fatalf("models = %#v, want %#v", created.Models, want)
    }

    got, err := store.GetByName(7, "coding-auto")
    if err != nil {
        t.Fatalf("get own profile: %v", err)
    }
    if got.ID != created.ID {
        t.Fatalf("id = %d, want %d", got.ID, created.ID)
    }

    if _, err := store.GetByName(8, "coding-auto"); err != ErrNotFound {
        t.Fatalf("other account error = %v, want ErrNotFound", err)
    }
}

func TestCreateRejectsInvalidPurposeAndEmptyModels(t *testing.T) {
	store := openStore(t)

	for _, purpose := range []Purpose{"", "unknown", "Coding", "documentation "} {
		if _, err := store.Create(1, CreateInput{Name: "bad", Purpose: purpose, Models: []string{"gpt-5"}}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("purpose %q error = %v, want ErrInvalid", purpose, err)
		}
	}
	if _, err := store.Create(1, CreateInput{Name: "empty", Purpose: PurposeGeneral, Models: []string{"  "}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty models error = %v, want ErrInvalid", err)
	}
}
