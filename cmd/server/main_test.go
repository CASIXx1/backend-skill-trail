package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	healthHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", got)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", body["status"])
	}
}

func TestDBHealthHandlerMissingConfig(t *testing.T) {
	t.Setenv("DB_HOST", "")
	t.Setenv("DB_PORT", "")
	t.Setenv("DB_USER", "")
	t.Setenv("DB_PASSWORD", "")
	t.Setenv("DB_NAME", "")

	req := httptest.NewRequest(http.MethodGet, "/db/health", nil)
	rec := httptest.NewRecorder()

	dbHealthHandler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if body["status"] != "ng" {
		t.Fatalf("expected status ng, got %q", body["status"])
	}

	if body["error"] == "" {
		t.Fatal("expected error message")
	}
}

func TestDBConfigFromEnv(t *testing.T) {
	t.Setenv("DB_HOST", "writer.example.local")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USER", "app_user")
	t.Setenv("DB_PASSWORD", "app_password")
	t.Setenv("DB_NAME", "app_db")

	cfg, err := dbConfigFromEnv()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.Host != "writer.example.local" {
		t.Fatalf("expected host writer.example.local, got %q", cfg.Host)
	}

	if cfg.Port != "5432" {
		t.Fatalf("expected port 5432, got %q", cfg.Port)
	}
}

func TestPostgresDSN(t *testing.T) {
	cfg := dbConfig{
		Host:     "writer.example.local",
		Port:     "5432",
		User:     "app_user",
		Password: "app_password",
		Name:     "app_db",
	}

	dsn := postgresDSN(cfg)

	if !strings.HasPrefix(dsn, "postgres://app_user:app_password@writer.example.local:5432/app_db?") {
		t.Fatalf("unexpected DSN prefix: %q", dsn)
	}

	if !strings.Contains(dsn, "sslmode=require") {
		t.Fatalf("expected sslmode=require in DSN: %q", dsn)
	}
}
