package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/alex/codegateway/internal/config"
	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
}

func Init(cfg config.DatabaseConfig) (*DB, error) {
	var db *sql.DB
	var err error

	switch cfg.Driver {
	case "sqlite":
		// Ensure directory exists
		dir := filepath.Dir(cfg.DSN)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create database directory: %w", err)
		}
		db, err = sql.Open("sqlite", cfg.DSN)
	case "postgres":
		db, err = sql.Open("postgres", cfg.DSN)
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", cfg.Driver)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Printf("Database initialized: %s", cfg.Driver)
	return &DB{db}, nil
}

func (db *DB) Close() error {
	return db.DB.Close()
}
