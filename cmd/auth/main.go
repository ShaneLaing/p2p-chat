package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/httplog"
	_ "github.com/jackc/pgx/v5/stdlib"

	"p2p-chat/internal/authserver"
)

func main() {
	logger := httplog.NewLogger("auth", httplog.Options{JSON: false})
	db := configureDatabase()
	if db != nil {
		defer db.Close()
	}
	server := authserver.New(db)
	addr := ":8089"
	log.Printf("Auth server running at %s", addr)
	if err := http.ListenAndServe(addr, httplog.RequestLogger(logger)(server.Router())); err != nil {
		log.Fatalf("auth server stopped: %v", err)
	}
}

func configureDatabase() *sql.DB {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Print("DATABASE_URL not set; running without PostgreSQL persistence")
		return nil
	}
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	if err := db.Ping(); err != nil {
		log.Fatalf("db ping: %v", err)
	}
	if err := runMigrations(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	return db
}
func runMigrations(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id SERIAL PRIMARY KEY,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS messages (
			id SERIAL PRIMARY KEY,
			sender TEXT NOT NULL,
			receiver TEXT,
			content TEXT NOT NULL,
			timestamp TIMESTAMPTZ DEFAULT NOW()
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
