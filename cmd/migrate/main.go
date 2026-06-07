package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"time"

	_ "github.com/lib/pq"
)

const migrationTimeout = 30 * time.Second

type dbConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), migrationTimeout)
	defer cancel()

	command, err := commandFromArgs(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}

	if err := run(ctx, command); err != nil {
		log.Fatalf("%s failed: %v", command, err)
	}

	log.Printf("%s completed", command)
}

func commandFromArgs(args []string) (string, error) {
	if len(args) == 0 {
		return "up", nil
	}

	switch args[0] {
	case "up", "verify":
		return args[0], nil
	default:
		return "", fmt.Errorf("unknown command: %s", args[0])
	}
}

func run(ctx context.Context, command string) error {
	cfg, err := dbConfigFromEnv()
	if err != nil {
		return err
	}

	db, err := sql.Open("postgres", postgresDSN(cfg))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	switch command {
	case "up":
		return migrate(ctx, db)
	case "verify":
		return verify(ctx, db)
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
}

func migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, createUsersTableSQL); err != nil {
		return fmt.Errorf("create users table: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}

	return nil
}

func verify(ctx context.Context, db *sql.DB) error {
	var exists bool
	if err := db.QueryRowContext(ctx, verifyUsersTableSQL).Scan(&exists); err != nil {
		return fmt.Errorf("verify users table: %w", err)
	}

	if !exists {
		return fmt.Errorf("users table does not exist")
	}

	log.Println("users table exists")
	return nil
}

func dbConfigFromEnv() (dbConfig, error) {
	cfg := dbConfig{
		Host:     os.Getenv("DB_HOST"),
		Port:     os.Getenv("DB_PORT"),
		User:     os.Getenv("DB_USER"),
		Password: os.Getenv("DB_PASSWORD"),
		Name:     os.Getenv("DB_NAME"),
	}

	if cfg.Host == "" || cfg.Port == "" || cfg.User == "" || cfg.Password == "" || cfg.Name == "" {
		return dbConfig{}, fmt.Errorf("missing database environment variable")
	}

	return cfg, nil
}

func postgresDSN(cfg dbConfig) string {
	dsn := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   net.JoinHostPort(cfg.Host, cfg.Port),
		Path:   "/" + cfg.Name,
	}

	query := dsn.Query()
	query.Set("sslmode", "require")
	dsn.RawQuery = query.Encode()

	return dsn.String()
}

const createUsersTableSQL = `
CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`

const verifyUsersTableSQL = `
SELECT EXISTS (
    SELECT 1
    FROM information_schema.tables
    WHERE table_schema = 'public'
      AND table_name = 'users'
);
`
