package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeWorkerMessageSender struct {
	messageID string
	err       error
	queueURL  string
	body      string
	calls     int
}

func (f *fakeWorkerMessageSender) SendMessage(ctx context.Context, queueURL string, body string) (string, error) {
	f.calls++
	f.queueURL = queueURL
	f.body = body
	if f.err != nil {
		return "", f.err
	}
	return f.messageID, nil
}

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

	s := &server{}
	s.dbHealthHandler(rec, req)

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

func TestUsersTableHandlerMissingConfig(t *testing.T) {
	t.Setenv("DB_HOST", "")
	t.Setenv("DB_PORT", "")
	t.Setenv("DB_USER", "")
	t.Setenv("DB_PASSWORD", "")
	t.Setenv("DB_NAME", "")

	req := httptest.NewRequest(http.MethodGet, "/db/users-table", nil)
	rec := httptest.NewRecorder()

	s := &server{}
	s.usersTableHandler(rec, req)

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
}

func TestLogTestHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/logs/test", nil)
	rec := httptest.NewRecorder()

	logTestHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", body["status"])
	}

	if body["testID"] == "" {
		t.Fatal("expected testID")
	}

	if body["count"] != float64(4) {
		t.Fatalf("expected count 4, got %v", body["count"])
	}
}

func TestWorkerJobsHandlerMissingConfig(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/worker/jobs", nil)
	rec := httptest.NewRecorder()

	s := &server{}
	s.workerJobsHandler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if body["status"] != "ng" {
		t.Fatalf("expected status ng, got %q", body["status"])
	}
}

func TestWorkerJobsHandlerSuccess(t *testing.T) {
	sender := &fakeWorkerMessageSender{messageID: "sqs-message-1"}
	s := &server{
		workerQueueURL: "https://sqs.example/queue",
		workerSender:   sender,
	}

	req := httptest.NewRequest(http.MethodPost, "/worker/jobs", nil)
	rec := httptest.NewRecorder()

	s.workerJobsHandler(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, rec.Code)
	}
	if sender.calls != 1 {
		t.Fatalf("expected one send call, got %d", sender.calls)
	}
	if sender.queueURL != "https://sqs.example/queue" {
		t.Fatalf("unexpected queue URL: %q", sender.queueURL)
	}

	var sent workerJobMessage
	if err := json.Unmarshal([]byte(sender.body), &sent); err != nil {
		t.Fatalf("failed to decode sent worker message: %v", err)
	}
	if sent.ID == "" {
		t.Fatal("expected generated worker job ID")
	}
	if sent.Type != "worker.test" {
		t.Fatalf("expected worker.test type, got %q", sent.Type)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["messageId"] != "sqs-message-1" {
		t.Fatalf("expected messageId sqs-message-1, got %q", body["messageId"])
	}
}

func TestWorkerJobsHandlerSendFailure(t *testing.T) {
	sender := &fakeWorkerMessageSender{err: errors.New("send failed")}
	s := &server{
		workerQueueURL: "https://sqs.example/queue",
		workerSender:   sender,
	}

	req := httptest.NewRequest(http.MethodPost, "/worker/jobs", nil)
	rec := httptest.NewRecorder()

	s.workerJobsHandler(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}

func TestOKLogHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/logs/status/ok", nil)
	rec := httptest.NewRecorder()

	okLogHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", body["status"])
	}

	if body["testID"] == "" {
		t.Fatal("expected testID")
	}
}

func TestErrorLogHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/logs/status/error", nil)
	rec := httptest.NewRecorder()

	errorLogHandler(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if body["status"] != "ng" {
		t.Fatalf("expected status ng, got %q", body["status"])
	}

	if body["testID"] == "" {
		t.Fatal("expected testID")
	}
}

func TestECSLogHandlerWithoutMetadataEndpoint(t *testing.T) {
	t.Setenv("ECS_CONTAINER_METADATA_URI_V4", "")

	req := httptest.NewRequest(http.MethodGet, "/logs/ecs", nil)
	rec := httptest.NewRecorder()

	ecsLogHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", body["status"])
	}

	if body["metadataAvailable"] != false {
		t.Fatalf("expected metadataAvailable false, got %v", body["metadataAvailable"])
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

func TestUsersTableExistsSQL(t *testing.T) {
	required := []string{
		"information_schema.tables",
		"table_schema = 'public'",
		"table_name = 'users'",
	}

	for _, fragment := range required {
		if !strings.Contains(usersTableExistsSQL, fragment) {
			t.Fatalf("expected users table SQL to contain %q", fragment)
		}
	}
}
