package profile

import (
	"errors"
	"reflect"
	"testing"
)

func TestCreateRejectsDuplicateNameWithinAccount(t *testing.T) {
	store := openStore(t)
	input := CreateInput{Name: "coding", Purpose: PurposeCoding, Models: []string{"gpt-5"}}
	if _, err := store.Create(1, input); err != nil {
		t.Fatalf("create first profile: %v", err)
	}
	if _, err := store.Create(1, input); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate create error = %v, want ErrConflict", err)
	}
	if _, err := store.Create(2, input); err != nil {
		t.Fatalf("same name must be valid for another account: %v", err)
	}
}

func TestUpdateDeleteAndListRemainTenantScoped(t *testing.T) {
	store := openStore(t)
	created, err := store.Create(1, CreateInput{Name: "first", Purpose: PurposeGeneral, Models: []string{"one"}})
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.Create(1, CreateInput{Name: "second", Purpose: PurposeDocumentation, Models: []string{"two"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(2, CreateInput{Name: "first", Purpose: PurposeGeneral, Models: []string{"three"}}); err != nil {
		t.Fatal(err)
	}

	updated, err := store.Update(1, created.ID, CreateInput{Name: "renamed", Purpose: PurposeCoding, Models: []string{"gpt-5", "gpt-5", "claude"}})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Purpose != PurposeCoding || !reflect.DeepEqual(updated.Models, []string{"gpt-5", "claude"}) {
		t.Fatalf("updated profile = %#v", updated)
	}
	if _, err := store.Update(2, created.ID, CreateInput{Name: "stolen", Purpose: PurposeGeneral, Models: []string{"x"}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-account update error = %v, want ErrNotFound", err)
	}
	if err := store.Delete(1, other.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := store.Delete(1, other.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("repeat delete error = %v, want ErrNotFound", err)
	}
	profiles, err := store.List(1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(profiles) != 1 || profiles[0].ID != created.ID {
		t.Fatalf("profiles = %#v", profiles)
	}
}
