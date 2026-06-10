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

	"github.com/redis/go-redis/v9"
	_ "github.com/lib/pq"
)

const dbPingTimeout = 3 * time.Second
const ecsMetadataTimeout = 2 * time.Second

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
	mux.HandleFunc("/cache/health", cacheHealthHandler)
	mux.HandleFunc("/cache/set", cacheSetHandler)
	mux.HandleFunc("/cache/list", cacheListHandler)
	mux.HandleFunc("/cache/session", cacheSessionHandler)
	mux.HandleFunc("/logs/test", logTestHandler)
	mux.HandleFunc("/logs/status/ok", okLogHandler)
	mux.HandleFunc("/logs/status/error", errorLogHandler)
	mux.HandleFunc("/logs/ecs", ecsLogHandler)

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

func logTestHandler(w http.ResponseWriter, r *http.Request) {
	testID := time.Now().UTC().Format("20060102T150405.000000000Z")
	samples := []map[string]any{
		{
			"event_type": "new_relic_log_test",
			"level":      "info",
			"test_id":    testID,
			"message":    "plain application log sample",
		},
		{
			"event_type": "new_relic_log_test",
			"level":      "warn",
			"test_id":    testID,
			"message":    "warning log sample",
			"component":  "api",
		},
		{
			"event_type": "new_relic_log_test",
			"level":      "error",
			"test_id":    testID,
			"message":    "error log sample without sensitive values",
			"component":  "api",
		},
		{
			"event_type": "new_relic_log_test",
			"level":      "info",
			"test_id":    testID,
			"message":    "structured log sample",
			"attributes": map[string]any{
				"feature": "firelens-new-relic",
				"source":  "manual-test-endpoint",
			},
		},
	}

	for _, sample := range samples {
		writeLog(sample)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"testID": testID,
		"count":  len(samples),
	})
}

func okLogHandler(w http.ResponseWriter, r *http.Request) {
	testID := time.Now().UTC().Format("20060102T150405.000000000Z")
	writeLog(map[string]any{
		"event_type":  "new_relic_http_log_test",
		"level":       "info",
		"test_id":     testID,
		"status_code": http.StatusOK,
		"method":      r.Method,
		"path":        r.URL.Path,
		"message":     "200 OK log sample",
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"testID": testID,
	})
}

func errorLogHandler(w http.ResponseWriter, r *http.Request) {
	testID := time.Now().UTC().Format("20060102T150405.000000000Z")
	writeLog(map[string]any{
		"event_type":  "new_relic_http_log_test",
		"level":       "error",
		"test_id":     testID,
		"status_code": http.StatusInternalServerError,
		"method":      r.Method,
		"path":        r.URL.Path,
		"message":     "500 error log sample",
		"error":       "intentional test error",
	})

	writeJSON(w, http.StatusInternalServerError, map[string]any{
		"status": "ng",
		"testID": testID,
		"error":  "intentional test error",
	})
}

func ecsLogHandler(w http.ResponseWriter, r *http.Request) {
	testID := time.Now().UTC().Format("20060102T150405.000000000Z")
	metadata, metadataAvailable := ecsMetadata(r.Context())
	writeLog(map[string]any{
		"event_type":         "new_relic_ecs_log_test",
		"level":              "info",
		"test_id":            testID,
		"message":            "ECS metadata log sample",
		"metadata_available": metadataAvailable,
		"metadata":           metadata,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"status":            "ok",
		"testID":            testID,
		"metadataAvailable": metadataAvailable,
	})
}

func ecsMetadata(parent context.Context) (map[string]any, bool) {
	metadataURI := os.Getenv("ECS_CONTAINER_METADATA_URI_V4")
	if metadataURI == "" {
		return map[string]any{
			"reason": "ECS_CONTAINER_METADATA_URI_V4 is not set",
		}, false
	}

	ctx, cancel := context.WithTimeout(parent, ecsMetadataTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURI+"/task", nil)
	if err != nil {
		return map[string]any{"error": "failed to build metadata request"}, false
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return map[string]any{"error": "failed to get ECS metadata"}, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return map[string]any{"status_code": resp.StatusCode}, false
	}

	var metadata map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		return map[string]any{"error": "failed to decode ECS metadata"}, false
	}

	return metadata, true
}

func writeLog(fields map[string]any) {
	if err := json.NewEncoder(os.Stdout).Encode(fields); err != nil {
		log.Printf("failed to write structured log: %v", err)
	}
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
	sslmode := os.Getenv("DB_SSLMODE")
	if sslmode == "" {
		sslmode = "require"
	}

	dsn := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   net.JoinHostPort(cfg.Host, cfg.Port),
		Path:   "/" + cfg.Name,
	}

	query := dsn.Query()
	query.Set("sslmode", sslmode)
	dsn.RawQuery = query.Encode()

	return dsn.String()
}

func cacheHealthHandler(w http.ResponseWriter, r *http.Request) {
	client, err := openCache()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "ng",
			"error":  err.Error(),
		})
		return
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "ng",
			"error":  fmt.Sprintf("failed to ping cache: %v", err),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
	})
}

func cacheSetHandler(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	val := r.URL.Query().Get("value")
	if key == "" || val == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "key and value are required"})
		return
	}

	client, err := openCache()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	defer client.Close()

	ctx := r.Context()
	if err := client.Set(ctx, key, val, 10*time.Minute).Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	writeLog(map[string]any{
		"level":     "info",
		"message":   "cache set",
		"cache_key": key,
		"timestamp": time.Now().Format(time.RFC3339),
	})

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "key": key, "value": val})
}

func cacheListHandler(w http.ResponseWriter, r *http.Request) {
	client, err := openCache()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	defer client.Close()

	ctx := r.Context()
	keys, err := client.Keys(ctx, "*").Result()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	writeLog(map[string]any{
		"level":      "info",
		"message":    "cache list",
		"keys_count": len(keys),
		"timestamp":  time.Now().Format(time.RFC3339),
	})

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "keys": keys})
}

func cacheSessionHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		sessionID = fmt.Sprintf("sess_%d", time.Now().UnixNano())
	}

	client, err := openCache()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	defer client.Close()

	ctx := r.Context()
	sessionData := map[string]any{
		"user_id":    123,
		"last_login": time.Now().Format(time.RFC3339),
	}

	data, _ := json.Marshal(sessionData)
	if err := client.Set(ctx, sessionID, data, 30*time.Minute).Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	writeLog(map[string]any{
		"level":      "info",
		"message":    "session saved",
		"session_id": sessionID,
		"timestamp":  time.Now().Format(time.RFC3339),
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"session_id": sessionID,
		"data":       sessionData,
	})
}

func openCache() (*redis.Client, error) {
	host := os.Getenv("CACHE_HOST")
	port := os.Getenv("CACHE_PORT")
	password := os.Getenv("CACHE_AUTH_TOKEN")

	if host == "" || port == "" {
		return nil, fmt.Errorf("cache configuration is missing")
	}

	return redis.NewClient(&redis.Options{
		Addr:     net.JoinHostPort(host, port),
		Password: password,
		DB:       0,
	}), nil
}

func writeJSON(w http.ResponseWriter, statusCode int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("failed to write response: %v", err)
	}
}
