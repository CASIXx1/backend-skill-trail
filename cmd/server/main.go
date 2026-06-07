package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"
)

const dbPingTimeout = 3 * time.Second

const usersTableExistsSQL = `
SELECT EXISTS (
    SELECT 1
    FROM information_schema.tables
    WHERE table_schema = 'public'
      AND table_name = 'users'
);
`

type dbConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/db/health", dbHealthHandler)
	mux.HandleFunc("/db/users-table", usersTableHandler)

	addr := ":" + port
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("server listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	log.Println("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatal(err)
	}

	log.Println("server stopped")
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func dbHealthHandler(w http.ResponseWriter, r *http.Request) {
	db, err := openDB()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "ng",
			"error":  "database configuration is missing",
		})
		return
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(r.Context(), dbPingTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "ng",
			"error":  "database ping failed",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func usersTableHandler(w http.ResponseWriter, r *http.Request) {
	db, err := openDB()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "ng",
			"error":  "database configuration is missing",
		})
		return
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(r.Context(), dbPingTimeout)
	defer cancel()

	var exists bool
	if err := db.QueryRowContext(ctx, usersTableExistsSQL).Scan(&exists); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "ng",
			"error":  "users table check failed",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"table":  "users",
		"exists": exists,
	})
}

func openDB() (*sql.DB, error) {
	cfg, err := dbConfigFromEnv()
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("postgres", postgresDSN(cfg))
	if err != nil {
		return nil, err
	}

	return db, nil
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

func writeJSON(w http.ResponseWriter, statusCode int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("failed to write response: %v", err)
	}
}
