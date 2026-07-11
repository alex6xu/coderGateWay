package rag

import (
	"database/sql"
	"fmt"
	"strings"
)

// Document represents a document in the RAG system
type Document struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Source  string `json:"source"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// SearchResult represents a search result
type SearchResult struct {
	Document *Document `json:"document"`
	Score    float64   `json:"score"`
}

// RAGService manages the RAG system
type RAGService struct {
	db *sql.DB
}

// NewRAGService creates a new RAG service
func NewRAGService(db *sql.DB) *RAGService {
	return &RAGService{db: db}
}

// Index indexes a document
func (s *RAGService) Index(doc *Document) error {
	// Store document in memory_fts table
	_, err := s.db.Exec(
		"INSERT INTO memory_fts (path, scope, scope_id, type, content) VALUES (?, ?, ?, ?, ?)",
		doc.ID, "rag", doc.Source, "document", doc.Content,
	)
	if err != nil {
		return fmt.Errorf("failed to index document: %w", err)
	}

	return nil
}

// Search searches for relevant documents
func (s *RAGService) Search(query string, limit int) ([]*SearchResult, error) {
	if limit <= 0 {
		limit = 5
	}

	// Search using FTS5
	rows, err := s.db.Query(`
		SELECT path, content, rank
		FROM memory_fts
		WHERE memory_fts MATCH ? AND scope = 'rag'
		ORDER BY rank
		LIMIT ?
	`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to search: %w", err)
	}
	defer rows.Close()

	results := make([]*SearchResult, 0)
	for rows.Next() {
		var doc Document
		var score float64
		err := rows.Scan(&doc.ID, &doc.Content, &score)
		if err != nil {
			return nil, fmt.Errorf("failed to scan result: %w", err)
		}

		results = append(results, &SearchResult{
			Document: &doc,
			Score:    score,
		})
	}

	return results, nil
}

// SearchWithContext searches for relevant documents and returns context
func (s *RAGService) SearchWithContext(query string, limit int) (string, error) {
	results, err := s.Search(query, limit)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "", nil
	}

	// Build context from results
	var context strings.Builder
	context.WriteString("Relevant context:\n\n")

	for i, result := range results {
		context.WriteString(fmt.Sprintf("--- Document %d (score: %.2f) ---\n", i+1, result.Score))
		context.WriteString(result.Document.Content)
		context.WriteString("\n\n")
	}

	return context.String(), nil
}

// Delete deletes a document
func (s *RAGService) Delete(id string) error {
	_, err := s.db.Exec("DELETE FROM memory_fts WHERE path = ? AND scope = 'rag'", id)
	if err != nil {
		return fmt.Errorf("failed to delete document: %w", err)
	}

	return nil
}

// List lists all indexed documents
func (s *RAGService) List(limit, offset int) ([]*Document, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.Query(`
		SELECT path, content, scope_id
		FROM memory_fts
		WHERE scope = 'rag'
		ORDER BY rowid DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list documents: %w", err)
	}
	defer rows.Close()

	docs := make([]*Document, 0)
	for rows.Next() {
		var doc Document
		err := rows.Scan(&doc.ID, &doc.Content, &doc.Source)
		if err != nil {
			return nil, fmt.Errorf("failed to scan document: %w", err)
		}
		docs = append(docs, &doc)
	}

	return docs, nil
}

// Stats returns RAG statistics
func (s *RAGService) Stats() (map[string]interface{}, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM memory_fts WHERE scope = 'rag'").Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}

	return map[string]interface{}{
		"total_documents": count,
	}, nil
}
