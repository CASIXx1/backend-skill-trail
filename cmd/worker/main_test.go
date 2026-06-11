package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeQueueClient struct {
	messages      []queueMessage
	receiveErr    error
	receiveCalls  int
	deleteCalls   int
	deletedQueue  string
	deletedHandle string
}

func (f *fakeQueueClient) ReceiveMessages(ctx context.Context, cfg workerConfig) ([]queueMessage, error) {
	f.receiveCalls++
	if f.receiveErr != nil {
		return nil, f.receiveErr
	}
	return f.messages, nil
}

func (f *fakeQueueClient) DeleteMessage(ctx context.Context, queueURL string, receiptHandle string) error {
	f.deleteCalls++
	f.deletedQueue = queueURL
	f.deletedHandle = receiptHandle
	return nil
}

func TestConfigFromEnvDefaults(t *testing.T) {
	t.Setenv("WORKER_QUEUE_URL", "https://sqs.example/worker")
	t.Setenv("WORKER_WAIT_TIME_SECONDS", "")
	t.Setenv("WORKER_MAX_MESSAGES", "")
	t.Setenv("WORKER_VISIBILITY_TIMEOUT", "")

	cfg, err := configFromEnv()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.QueueURL != "https://sqs.example/worker" {
		t.Fatalf("unexpected queue URL: %q", cfg.QueueURL)
	}
	if cfg.WaitTimeSeconds != 20 {
		t.Fatalf("expected wait time 20, got %d", cfg.WaitTimeSeconds)
	}
	if cfg.MaxMessages != 10 {
		t.Fatalf("expected max messages 10, got %d", cfg.MaxMessages)
	}
	if cfg.VisibilityTimeoutSeconds != 30 {
		t.Fatalf("expected visibility timeout 30, got %d", cfg.VisibilityTimeoutSeconds)
	}
}

func TestConfigFromEnvRequiresQueueURL(t *testing.T) {
	t.Setenv("WORKER_QUEUE_URL", "")

	if _, err := configFromEnv(); err == nil {
		t.Fatal("expected error")
	}
}

func TestRunOnceDeletesAfterSuccessfulProcessing(t *testing.T) {
	client := &fakeQueueClient{
		messages: []queueMessage{
			{MessageID: "msg-1", ReceiptHandle: "receipt-1", Body: `{"ok":true}`},
		},
	}
	processed := 0
	w := &worker{
		client: client,
		cfg: workerConfig{
			QueueURL:                  "https://sqs.example/worker",
			EmptyReceiveSleepDuration: time.Nanosecond,
		},
		processFn: func(ctx context.Context, message queueMessage) error {
			processed++
			return nil
		},
	}

	w.runOnce(context.Background())

	if processed != 1 {
		t.Fatalf("expected one processed message, got %d", processed)
	}
	if client.deleteCalls != 1 {
		t.Fatalf("expected one delete call, got %d", client.deleteCalls)
	}
	if client.deletedQueue != "https://sqs.example/worker" {
		t.Fatalf("unexpected deleted queue: %q", client.deletedQueue)
	}
	if client.deletedHandle != "receipt-1" {
		t.Fatalf("unexpected deleted receipt handle: %q", client.deletedHandle)
	}
}

func TestRunOnceDoesNotDeleteAfterProcessingFailure(t *testing.T) {
	client := &fakeQueueClient{
		messages: []queueMessage{
			{MessageID: "msg-1", ReceiptHandle: "receipt-1", Body: `{"ok":true}`},
		},
	}
	w := &worker{
		client: client,
		cfg: workerConfig{
			QueueURL:                  "https://sqs.example/worker",
			EmptyReceiveSleepDuration: time.Nanosecond,
		},
		processFn: func(ctx context.Context, message queueMessage) error {
			return errors.New("process failed")
		},
	}

	w.runOnce(context.Background())

	if client.deleteCalls != 0 {
		t.Fatalf("expected no delete calls, got %d", client.deleteCalls)
	}
}

func TestRunOnceReceiveErrorDoesNotPanic(t *testing.T) {
	client := &fakeQueueClient{receiveErr: errors.New("receive failed")}
	w := &worker{
		client: client,
		cfg: workerConfig{
			QueueURL:                  "https://sqs.example/worker",
			EmptyReceiveSleepDuration: time.Nanosecond,
		},
		processFn: func(ctx context.Context, message queueMessage) error {
			t.Fatal("process should not be called")
			return nil
		},
	}

	w.runOnce(context.Background())

	if client.receiveCalls != 1 {
		t.Fatalf("expected one receive call, got %d", client.receiveCalls)
	}
	if client.deleteCalls != 0 {
		t.Fatalf("expected no delete calls, got %d", client.deleteCalls)
	}
}
