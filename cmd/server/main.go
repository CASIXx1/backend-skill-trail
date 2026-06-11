package main

import (
	"context"
	"crypto/tls"
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

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
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

type server struct {
	db             *sql.DB
	cache          *redis.Client
	workerQueueURL string
	workerSender   workerMessageSender
}

type workerMessageSender interface {
	SendMessage(ctx context.Context, queueURL string, body string) (string, error)
}

type sqsWorkerMessageSender struct {
	client *sqs.Client
}

type workerJobMessage struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"createdAt"`
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	db, err := openDB()
	if err != nil {
		log.Printf("warning: database connection failed: %v", err)
	} else {
		defer db.Close()
	}

	cache, err := openCache()
	if err != nil {
		log.Printf("warning: cache connection failed: %v", err)
	} else {
		defer cache.Close()
	}

	workerQueueURL := os.Getenv("WORKER_QUEUE_URL")
	workerSender, err := openWorkerSender(context.Background(), workerQueueURL)
	if err != nil {
		log.Printf("warning: worker queue sender is not initialized: %v", err)
	}

	s := &server{
		db:             db,
		cache:          cache,
		workerQueueURL: workerQueueURL,
		workerSender:   workerSender,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/db/health", s.dbHealthHandler)
	mux.HandleFunc("/db/users-table", s.usersTableHandler)
	mux.HandleFunc("/cache/health", s.cacheHealthHandler)
	mux.HandleFunc("/cache/set", s.cacheSetHandler)
	mux.HandleFunc("/cache/list", s.cacheListHandler)
	mux.HandleFunc("/cache/session", s.cacheSessionHandler)
	mux.HandleFunc("/worker/jobs", s.workerJobsHandler)
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

func (s *sqsWorkerMessageSender) SendMessage(ctx context.Context, queueURL string, body string) (string, error) {
	out, err := s.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    &queueURL,
		MessageBody: &body,
	})
	if err != nil {
		return "", err
	}
	if out.MessageId == nil {
		return "", fmt.Errorf("sqs send succeeded without message id")
	}
	return *out.MessageId, nil
}

func openWorkerSender(ctx context.Context, queueURL string) (workerMessageSender, error) {
	if queueURL == "" {
		return nil, nil
	}

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}

	return &sqsWorkerMessageSender{client: sqs.NewFromConfig(cfg)}, nil
}

func (s *server) workerJobsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}

	if s.workerQueueURL == "" || s.workerSender == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "ng",
			"error":  "worker queue is not configured",
		})
		return
	}

	now := time.Now().UTC()
	job := workerJobMessage{
		ID:        fmt.Sprintf("worker-job-%d", now.UnixNano()),
		Type:      "worker.test",
		CreatedAt: now,
	}

	body, err := json.Marshal(job)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to encode worker job"})
		return
	}

	messageID, err := s.workerSender.SendMessage(r.Context(), s.workerQueueURL, string(body))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"status": "ng",
			"error":  "failed to send worker job",
		})
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":    "accepted",
		"jobId":     job.ID,
		"messageId": messageID,
	})
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) dbHealthHandler(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "ng",
			"error":  "database connection is not initialized",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbPingTimeout)
	defer cancel()

	if err := s.db.PingContext(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "ng",
			"error":  "database ping failed",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *server) usersTableHandler(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "ng",
			"error":  "database connection is not initialized",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbPingTimeout)
	defer cancel()

	var exists bool
	if err := s.db.QueryRowContext(ctx, usersTableExistsSQL).Scan(&exists); err != nil {
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

func (s *server) cacheHealthHandler(w http.ResponseWriter, r *http.Request) {
	if s.cache == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "ng",
			"error":  "cache connection is not initialized",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := s.cache.Ping(ctx).Err(); err != nil {
		host := os.Getenv("CACHE_HOST")
		port := os.Getenv("CACHE_PORT")
		tlsEnabled := os.Getenv("CACHE_TLS_ENABLED") == "true"
		log.Printf("Ping failed: %v", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "ng",
			"error":  fmt.Sprintf("failed to ping cache: %v (host=%s, port=%s, tls=%v). check logs for dial details.", err, host, port, tlsEnabled),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
	})
}

func (s *server) cacheSetHandler(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	val := r.URL.Query().Get("value")
	if key == "" || val == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "key and value are required"})
		return
	}

	if s.cache == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "cache connection is not initialized"})
		return
	}

	ctx := r.Context()
	if err := s.cache.Set(ctx, key, val, 10*time.Minute).Err(); err != nil {
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

func (s *server) cacheListHandler(w http.ResponseWriter, r *http.Request) {
	if s.cache == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "cache connection is not initialized"})
		return
	}

	ctx := r.Context()
	keys, err := s.cache.Keys(ctx, "*").Result()
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

func (s *server) cacheSessionHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		sessionID = fmt.Sprintf("sess_%d", time.Now().UnixNano())
	}

	if s.cache == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "cache connection is not initialized"})
		return
	}

	ctx := r.Context()
	sessionData := map[string]any{
		"user_id":    123,
		"last_login": time.Now().Format(time.RFC3339),
	}

	data, _ := json.Marshal(sessionData)
	if err := s.cache.Set(ctx, sessionID, data, 30*time.Minute).Err(); err != nil {
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
	tlsEnabled := os.Getenv("CACHE_TLS_ENABLED") == "true"

	if host == "" || port == "" {
		return nil, fmt.Errorf("cache configuration is missing")
	}

	addr := net.JoinHostPort(host, port)
	opts := &redis.Options{
		Addr:     addr,
		Password: password,
		DB:       0,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			log.Printf("Dialing cache: %s %s", network, addr)
			d := net.Dialer{
				Timeout: 5 * time.Second,
			}

			// 1. Resolve
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				log.Printf("DNS lookup failed for %s: %v", host, err)
			} else {
				log.Printf("DNS lookup success for %s: %v", host, ips)
			}

			// 2. Dial
			conn, err := d.DialContext(ctx, network, addr)
			if err != nil {
				log.Printf("TCP Dial failed for %s: %v", addr, err)
				return nil, err
			}
			log.Printf("TCP Dial success for %s", addr)

			// 3. TLS
			if tlsEnabled {
				log.Printf("Starting TLS handshake for %s", addr)
				tlsConn := tls.Client(conn, &tls.Config{
					InsecureSkipVerify: true,
					ServerName:         host,
				})
				if err := tlsConn.HandshakeContext(ctx); err != nil {
					log.Printf("TLS handshake failed for %s: %v", addr, err)
					conn.Close()
					return nil, err
				}
				log.Printf("TLS handshake success for %s", addr)
				return tlsConn, nil
			}

			return conn, nil
		},
	}

	return redis.NewClient(opts), nil
}

func writeJSON(w http.ResponseWriter, statusCode int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("failed to write response: %v", err)
	}
}
