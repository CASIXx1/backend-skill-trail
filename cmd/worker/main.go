package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

type workerConfig struct {
	QueueURL                  string
	WaitTimeSeconds           int32
	MaxMessages               int32
	VisibilityTimeoutSeconds  int32
	EmptyReceiveSleepDuration time.Duration
}

type queueMessage struct {
	MessageID     string
	ReceiptHandle string
	Body          string
}

type queueClient interface {
	ReceiveMessages(ctx context.Context, cfg workerConfig) ([]queueMessage, error)
	DeleteMessage(ctx context.Context, queueURL string, receiptHandle string) error
}

type sqsQueueClient struct {
	client *sqs.Client
}

type messageProcessor func(context.Context, queueMessage) error

func main() {
	cfg, err := configFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	awsCfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		log.Fatalf("failed to load AWS config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	worker := &worker{
		client:    &sqsQueueClient{client: sqs.NewFromConfig(awsCfg)},
		cfg:       cfg,
		processFn: logMessage,
	}

	log.Printf("worker started")
	worker.run(ctx)
	log.Printf("worker stopped")
}

type worker struct {
	client    queueClient
	cfg       workerConfig
	processFn messageProcessor
}

func configFromEnv() (workerConfig, error) {
	queueURL := os.Getenv("WORKER_QUEUE_URL")
	if queueURL == "" {
		return workerConfig{}, fmt.Errorf("WORKER_QUEUE_URL is required")
	}

	waitTime, err := int32Env("WORKER_WAIT_TIME_SECONDS", 20)
	if err != nil {
		return workerConfig{}, err
	}
	maxMessages, err := int32Env("WORKER_MAX_MESSAGES", 10)
	if err != nil {
		return workerConfig{}, err
	}
	visibilityTimeout, err := int32Env("WORKER_VISIBILITY_TIMEOUT", 30)
	if err != nil {
		return workerConfig{}, err
	}

	if waitTime < 0 || waitTime > 20 {
		return workerConfig{}, fmt.Errorf("WORKER_WAIT_TIME_SECONDS must be between 0 and 20")
	}
	if maxMessages < 1 || maxMessages > 10 {
		return workerConfig{}, fmt.Errorf("WORKER_MAX_MESSAGES must be between 1 and 10")
	}
	if visibilityTimeout < 0 {
		return workerConfig{}, fmt.Errorf("WORKER_VISIBILITY_TIMEOUT must be greater than or equal to 0")
	}

	return workerConfig{
		QueueURL:                  queueURL,
		WaitTimeSeconds:           waitTime,
		MaxMessages:               maxMessages,
		VisibilityTimeoutSeconds:  visibilityTimeout,
		EmptyReceiveSleepDuration: time.Second,
	}, nil
}

func int32Env(name string, fallback int32) (int32, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}

	return int32(parsed), nil
}

func (w *worker) run(ctx context.Context) {
	for ctx.Err() == nil {
		w.runOnce(ctx)
	}
}

func (w *worker) runOnce(ctx context.Context) {
	messages, err := w.client.ReceiveMessages(ctx, w.cfg)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("worker receive failed: %v", err)
			sleepContext(ctx, w.cfg.EmptyReceiveSleepDuration)
		}
		return
	}

	if len(messages) == 0 {
		sleepContext(ctx, w.cfg.EmptyReceiveSleepDuration)
		return
	}

	for _, message := range messages {
		if ctx.Err() != nil {
			return
		}

		if err := w.processFn(ctx, message); err != nil {
			log.Printf("worker process failed: message_id=%s error=%v", message.MessageID, err)
			continue
		}

		if err := w.client.DeleteMessage(ctx, w.cfg.QueueURL, message.ReceiptHandle); err != nil {
			log.Printf("worker delete failed: message_id=%s error=%v", message.MessageID, err)
		}
	}
}

func (c *sqsQueueClient) ReceiveMessages(ctx context.Context, cfg workerConfig) ([]queueMessage, error) {
	out, err := c.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:              &cfg.QueueURL,
		MaxNumberOfMessages:   cfg.MaxMessages,
		WaitTimeSeconds:       cfg.WaitTimeSeconds,
		VisibilityTimeout:     cfg.VisibilityTimeoutSeconds,
		MessageAttributeNames: []string{"All"},
		AttributeNames:        []types.QueueAttributeName{types.QueueAttributeNameAll},
	})
	if err != nil {
		return nil, err
	}

	messages := make([]queueMessage, 0, len(out.Messages))
	for _, msg := range out.Messages {
		messages = append(messages, queueMessage{
			MessageID:     stringValue(msg.MessageId),
			ReceiptHandle: stringValue(msg.ReceiptHandle),
			Body:          stringValue(msg.Body),
		})
	}

	return messages, nil
}

func (c *sqsQueueClient) DeleteMessage(ctx context.Context, queueURL string, receiptHandle string) error {
	_, err := c.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      &queueURL,
		ReceiptHandle: &receiptHandle,
	})
	return err
}

func logMessage(ctx context.Context, message queueMessage) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"level":      "info",
		"event_type": "worker_message_received",
		"message_id": message.MessageID,
		"body":       message.Body,
		"timestamp":  time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func sleepContext(ctx context.Context, duration time.Duration) {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
