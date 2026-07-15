package tags

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Service persists and aggregates question tags per account.
type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// TagRow is a stored tag with usage count.
type TagRow struct {
	ID         string    `json:"id"`
	UserID     int64     `json:"user_id"`
	Slug       string    `json:"slug"`
	Name       string    `json:"name"`
	Kind       string    `json:"kind"`
	UseCount   int       `json:"use_count"`
	UpdatedAt  time.Time `json:"updated_at"`
	CreatedAt  time.Time `json:"created_at"`
}

// TaggedMessage is a user question with its tags.
type TaggedMessage struct {
	MessageID   string    `json:"message_id"`
	SessionID   string    `json:"session_id"`
	Content     string    `json:"content"`
	Preview     string    `json:"preview"`
	Platform    string    `json:"platform"`
	SessionTitle string   `json:"session_title"`
	CreatedAt   time.Time `json:"created_at"`
	Tags        []TagHit  `json:"tags"`
}

// TagGroup is an aggregated view for one tag.
type TagGroup struct {
	Tag      TagRow          `json:"tag"`
	Messages []TaggedMessage `json:"messages"`
}

// TagMessage classifies and links tags to a user message.
func (s *Service) TagMessage(userID int64, messageID, content string) ([]TagHit, error) {
	hits := Classify(content)
	if len(hits) == 0 {
		return nil, nil
	}
	now := time.Now()
	for _, h := range hits {
		tagID, err := s.upsertTag(userID, h, now)
		if err != nil {
			return nil, err
		}
		_, err = s.db.Exec(`
			INSERT OR IGNORE INTO message_tags (message_id, tag_id, confidence, source, created_at)
			VALUES (?, ?, ?, 'auto', ?)
		`, messageID, tagID, h.Confidence, now)
		if err != nil {
			return nil, err
		}
	}
	return hits, nil
}

func (s *Service) upsertTag(userID int64, h TagHit, now time.Time) (string, error) {
	var id string
	err := s.db.QueryRow(`
		SELECT id FROM question_tags WHERE user_id = ? AND slug = ?
	`, userID, h.Slug).Scan(&id)
	if err == nil {
		_, _ = s.db.Exec(`
			UPDATE question_tags SET use_count = use_count + 1, name = ?, kind = ?, updated_at = ? WHERE id = ?
		`, h.Name, h.Kind, now, id)
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	id = uuid.NewString()
	_, err = s.db.Exec(`
		INSERT INTO question_tags (id, user_id, slug, name, kind, use_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 1, ?, ?)
	`, id, userID, h.Slug, h.Name, h.Kind, now, now)
	if err != nil {
		// race: fetch again
		if err2 := s.db.QueryRow(`SELECT id FROM question_tags WHERE user_id = ? AND slug = ?`, userID, h.Slug).Scan(&id); err2 == nil {
			_, _ = s.db.Exec(`UPDATE question_tags SET use_count = use_count + 1, updated_at = ? WHERE id = ?`, now, id)
			return id, nil
		}
		return "", err
	}
	return id, nil
}

// ListTags returns tags for an account ordered by use count.
func (s *Service) ListTags(userID int64, kind string, limit int) ([]TagRow, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `
		SELECT id, user_id, slug, name, kind, use_count, created_at, updated_at
		FROM question_tags WHERE user_id = ?
	`
	args := []interface{}{userID}
	if kind != "" {
		q += ` AND kind = ?`
		args = append(args, kind)
	}
	q += ` ORDER BY use_count DESC, updated_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]TagRow, 0)
	for rows.Next() {
		var t TagRow
		if err := rows.Scan(&t.ID, &t.UserID, &t.Slug, &t.Name, &t.Kind, &t.UseCount, &t.CreatedAt, &t.UpdatedAt); err != nil {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// GetTagBySlug loads a tag owned by the account.
func (s *Service) GetTagBySlug(userID int64, slug string) (*TagRow, error) {
	var t TagRow
	err := s.db.QueryRow(`
		SELECT id, user_id, slug, name, kind, use_count, created_at, updated_at
		FROM question_tags WHERE user_id = ? AND slug = ?
	`, userID, slug).Scan(&t.ID, &t.UserID, &t.Slug, &t.Name, &t.Kind, &t.UseCount, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ListMessagesByTag returns user questions under a tag, newest first.
func (s *Service) ListMessagesByTag(userID int64, slug string, limit int) (*TagGroup, error) {
	tag, err := s.GetTagBySlug(userID, slug)
	if err != nil {
		return nil, fmt.Errorf("tag not found")
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`
		SELECT m.id, m.session_id, m.content, m.created_at,
		       COALESCE(s.platform, ''), COALESCE(s.title, '')
		FROM message_tags mt
		JOIN question_tags t ON t.id = mt.tag_id
		JOIN messages m ON m.id = mt.message_id
		JOIN sessions s ON s.id = m.session_id
		WHERE t.user_id = ? AND t.slug = ? AND s.user_id = ? AND m.role = 'user'
		ORDER BY m.created_at DESC
		LIMIT ?
	`, userID, slug, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	msgs := make([]TaggedMessage, 0)
	ids := make([]string, 0)
	for rows.Next() {
		var m TaggedMessage
		if err := rows.Scan(&m.MessageID, &m.SessionID, &m.Content, &m.CreatedAt, &m.Platform, &m.SessionTitle); err != nil {
			continue
		}
		m.Preview = Preview(m.Content, 160)
		msgs = append(msgs, m)
		ids = append(ids, m.MessageID)
	}

	tagMap, _ := s.tagsForMessages(ids)
	for i := range msgs {
		msgs[i].Tags = tagMap[msgs[i].MessageID]
	}

	return &TagGroup{Tag: *tag, Messages: msgs}, nil
}

// Overview returns all tags with a sample of recent questions per top tags (for integration view).
func (s *Service) Overview(userID int64, topN, perTag int) ([]TagGroup, error) {
	if topN <= 0 {
		topN = 12
	}
	if perTag <= 0 {
		perTag = 5
	}
	tags, err := s.ListTags(userID, "", topN)
	if err != nil {
		return nil, err
	}
	out := make([]TagGroup, 0, len(tags))
	for _, t := range tags {
		g, err := s.ListMessagesByTag(userID, t.Slug, perTag)
		if err != nil {
			continue
		}
		out = append(out, *g)
	}
	return out, nil
}

// RetagRecent backfills tags for recent untagged user messages.
func (s *Service) RetagRecent(userID int64, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`
		SELECT m.id, m.content
		FROM messages m
		JOIN sessions s ON s.id = m.session_id
		WHERE s.user_id = ? AND m.role = 'user'
		  AND NOT EXISTS (SELECT 1 FROM message_tags mt WHERE mt.message_id = m.id)
		ORDER BY m.created_at DESC
		LIMIT ?
	`, userID, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		var id, content string
		if err := rows.Scan(&id, &content); err != nil {
			continue
		}
		if _, err := s.TagMessage(userID, id, content); err == nil {
			n++
		}
	}
	return n, nil
}

func (s *Service) tagsForMessages(messageIDs []string) (map[string][]TagHit, error) {
	out := map[string][]TagHit{}
	if len(messageIDs) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(messageIDs))
	args := make([]interface{}, 0, len(messageIDs))
	for i, id := range messageIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	q := fmt.Sprintf(`
		SELECT mt.message_id, t.slug, t.name, t.kind, mt.confidence
		FROM message_tags mt
		JOIN question_tags t ON t.id = mt.tag_id
		WHERE mt.message_id IN (%s)
		ORDER BY mt.confidence DESC
	`, strings.Join(placeholders, ","))
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var mid string
		var h TagHit
		if err := rows.Scan(&mid, &h.Slug, &h.Name, &h.Kind, &h.Confidence); err != nil {
			continue
		}
		out[mid] = append(out[mid], h)
	}
	return out, nil
}
