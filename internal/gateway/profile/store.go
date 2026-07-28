// Package profile persists tenant-scoped ordered model routing profiles.
package profile

import (
    "database/sql"
    "encoding/json"
    "errors"
    "fmt"
    "strings"
    "time"
)

var (
	ErrNotFound = errors.New("route profile not found")
	ErrConflict = errors.New("route profile already exists")
	ErrInvalid  = errors.New("invalid route profile")
)

type Purpose string

const (
    PurposeCoding Purpose = "coding"
    PurposeDocumentation Purpose = "documentation"
    PurposeGeneral Purpose = "general"
)

type Profile struct {
    ID int64 `json:"id"`
    UserID int64 `json:"user_id"`
    Name string `json:"name"`
    Purpose Purpose `json:"purpose"`
    Models []string `json:"models"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

type CreateInput struct { Name string; Purpose Purpose; Models []string }
type Store struct{ db *sql.DB }
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Create(userID int64, in CreateInput) (*Profile, error) {
    name, purpose, models, err := normalize(in)
    if err != nil { return nil, err }
    raw, err := json.Marshal(models)
    if err != nil { return nil, err }
    now := time.Now().UTC()
    result, err := s.db.Exec(`INSERT INTO route_profiles (user_id, name, purpose, models, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, userID, name, purpose, string(raw), now, now)
	if err != nil {
		if isUniqueConstraint(err) {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("create route profile: %w", err)
	}
    id, err := result.LastInsertId()
    if err != nil { return nil, fmt.Errorf("route profile id: %w", err) }
    return &Profile{ID: id, UserID: userID, Name: name, Purpose: purpose, Models: models, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Store) List(userID int64) ([]Profile, error) {
    rows, err := s.db.Query(`SELECT id, user_id, name, purpose, models, created_at, updated_at FROM route_profiles WHERE user_id = ? ORDER BY id DESC`, userID)
    if err != nil { return nil, fmt.Errorf("list route profiles: %w", err) }
    defer rows.Close()
    profiles := make([]Profile, 0)
    for rows.Next() { profile, err := scan(rows); if err != nil { return nil, err }; profiles = append(profiles, *profile) }
    return profiles, rows.Err()
}

func (s *Store) Get(userID, id int64) (*Profile, error) {
    profile, err := scan(s.db.QueryRow(`SELECT id, user_id, name, purpose, models, created_at, updated_at FROM route_profiles WHERE id = ? AND user_id = ?`, id, userID))
    if errors.Is(err, sql.ErrNoRows) { return nil, ErrNotFound }; return profile, err
}

func (s *Store) GetByName(userID int64, name string) (*Profile, error) {
    profile, err := scan(s.db.QueryRow(`SELECT id, user_id, name, purpose, models, created_at, updated_at FROM route_profiles WHERE user_id = ? AND name = ?`, userID, strings.TrimSpace(name)))
    if errors.Is(err, sql.ErrNoRows) { return nil, ErrNotFound }; return profile, err
}

func (s *Store) Update(userID, id int64, in CreateInput) (*Profile, error) {
    name, purpose, models, err := normalize(in); if err != nil { return nil, err }
    raw, err := json.Marshal(models); if err != nil { return nil, err }
    result, err := s.db.Exec(`UPDATE route_profiles SET name = ?, purpose = ?, models = ?, updated_at = ? WHERE id = ? AND user_id = ?`, name, purpose, string(raw), time.Now().UTC(), id, userID)
	if err != nil {
		if isUniqueConstraint(err) {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("update route profile: %w", err)
	}
    affected, err := result.RowsAffected(); if err != nil { return nil, err }; if affected == 0 { return nil, ErrNotFound }
    return s.Get(userID, id)
}

func (s *Store) Delete(userID, id int64) error {
    result, err := s.db.Exec(`DELETE FROM route_profiles WHERE id = ? AND user_id = ?`, id, userID); if err != nil { return fmt.Errorf("delete route profile: %w", err) }
    affected, err := result.RowsAffected(); if err != nil { return err }; if affected == 0 { return ErrNotFound }; return nil
}

type scanner interface{ Scan(...any) error }
func scan(row scanner) (*Profile, error) {
    var profile Profile; var raw string
    if err := row.Scan(&profile.ID, &profile.UserID, &profile.Name, &profile.Purpose, &raw, &profile.CreatedAt, &profile.UpdatedAt); err != nil { return nil, err }
    if err := json.Unmarshal([]byte(raw), &profile.Models); err != nil { return nil, fmt.Errorf("decode route profile models: %w", err) }
    return &profile, nil
}
func normalize(in CreateInput) (string, Purpose, []string, error) {
	name := strings.TrimSpace(in.Name); if name == "" { return "", "", nil, fmt.Errorf("%w: name is required", ErrInvalid) }
	if in.Purpose != PurposeCoding && in.Purpose != PurposeDocumentation && in.Purpose != PurposeGeneral { return "", "", nil, fmt.Errorf("%w: purpose is invalid", ErrInvalid) }
    seen := make(map[string]struct{}); models := make([]string, 0, len(in.Models))
    for _, model := range in.Models { model = strings.TrimSpace(model); if model == "" { continue }; key := strings.ToLower(model); if _, exists := seen[key]; exists { continue }; seen[key] = struct{}{}; models = append(models, model) }
	if len(models) == 0 { return "", "", nil, fmt.Errorf("%w: at least one model is required", ErrInvalid) }
    return name, in.Purpose, models, nil
}

func isUniqueConstraint(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}
