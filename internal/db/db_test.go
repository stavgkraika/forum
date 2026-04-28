package db

import (
	"os"
	"path/filepath"
	"testing"
)

const testSchema = `
CREATE TABLE IF NOT EXISTS test_table (id INTEGER PRIMARY KEY, name TEXT);
`

func TestOpen_Success(t *testing.T) {
	dir := t.TempDir()
	schemaFile := filepath.Join(dir, "schema.sql")
	if err := os.WriteFile(schemaFile, []byte(testSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "test.db")
	database, err := Open(dbPath, schemaFile)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer database.Close()
	if err := database.Ping(); err != nil {
		t.Fatalf("Ping() error: %v", err)
	}
}

func TestOpen_BadSchema(t *testing.T) {
	dir := t.TempDir()
	schemaFile := filepath.Join(dir, "schema.sql")
	if err := os.WriteFile(schemaFile, []byte("NOT VALID SQL!!!"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Open(filepath.Join(dir, "test.db"), schemaFile)
	if err == nil {
		t.Fatal("expected error for invalid schema, got nil")
	}
}

func TestOpen_MissingSchema(t *testing.T) {
	dir := t.TempDir()
	_, err := Open(filepath.Join(dir, "test.db"), filepath.Join(dir, "nonexistent.sql"))
	if err == nil {
		t.Fatal("expected error for missing schema file, got nil")
	}
}

func TestOpen_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	schemaFile := filepath.Join(dir, "schema.sql")
	if err := os.WriteFile(schemaFile, []byte(testSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	// DB path inside a subdirectory that doesn't exist yet.
	dbPath := filepath.Join(dir, "subdir", "test.db")
	database, err := Open(dbPath, schemaFile)
	if err != nil {
		t.Fatalf("Open() should create missing directories, got error: %v", err)
	}
	database.Close()
}
