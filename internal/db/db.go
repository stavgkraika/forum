// Package db handles opening and initialising the SQLite database.
package db

import (
	"database/sql"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3" // SQLite driver
)

// Open opens the SQLite database at path, creates any missing parent directories,
// enables foreign key enforcement, and applies the SQL schema at schemaPath.
func Open(path, schemaPath string) (*sql.DB, error) {
	// Ensure the directory for the DB file exists.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	database, err := sql.Open("sqlite3", path+"?_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	if err := database.Ping(); err != nil {
		database.Close()
		return nil, err
	}
	// Read and execute the schema file (CREATE TABLE IF NOT EXISTS statements).
	b, err := os.ReadFile(schemaPath)
	if err != nil {
		database.Close()
		return nil, err
	}
	if _, err := database.Exec(string(b)); err != nil {
		database.Close()
		return nil, err
	}
	if err := ensurePostImageColumn(database); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

func ensurePostImageColumn(database *sql.DB) error {
	rows, err := database.Query("PRAGMA table_info(posts)")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == "image_path" {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var exists int
	if err := database.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='posts'").Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	_, err = database.Exec("ALTER TABLE posts ADD COLUMN image_path TEXT")
	return err
}
