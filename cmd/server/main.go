// Package main is the entry point for the forum server.
// It initialises the database, wires up dependencies and starts the HTTP server.
package main

import (
	"log"
	"net/http"
	"os"

	"forum/internal/db"
	"forum/internal/handlers"
	"forum/internal/repository"
)

func main() {
	// Open the SQLite database and apply the schema migrations.
	dbPath := envOrDefault("DB_PATH", "./forum.db")
	schemaPath := envOrDefault("SCHEMA_PATH", "migrations/schema.sql")
	database, err := db.Open(dbPath, schemaPath)
	if err != nil {
		log.Fatal("db:", err)
	}
	defer database.Close()

	// Wire repository and HTTP handler layer.
	repo := repository.New(database)
	app, err := handlers.New(repo)
	if err != nil {
		log.Fatal("handlers:", err)
	}

	log.Println("server running on :8080")
	log.Fatal(http.ListenAndServe(":8080", app.Routes()))
}

func envOrDefault(name, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}
