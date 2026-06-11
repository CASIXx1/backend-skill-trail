package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sns"
)

const (
	jobLogHeartbeat     = "log_heartbeat"
	jobSendNotification = "send_notification"

	eventTypeHeartbeat    = "scheduled_new_relic_heartbeat"
	eventTypeNotification = "scheduled_notification_email"
	defaultScheduleName   = "eventbridge-scheduler"
)

type scheduledEvent struct {
	Level        string `json:"level"`
	EventType    string `json:"event_type"`
	Message      string `json:"message"`
	Timestamp    string `json:"timestamp"`
	Source       string `json:"source"`
	ScheduleName string `json:"schedule_name"`
	JobName      string `json:"job_name"`
}

type notificationPublisher interface {
	Publish(ctx context.Context, params *sns.PublishInput, optFns ...func(*sns.Options)) (*sns.PublishOutput, error)
}

func main() {
	ctx := context.Background()
	if err := run(ctx, os.Stdout, time.Now().UTC(), configFromEnv()); err != nil {
		fmt.Fprintf(os.Stderr, "scheduled job failed: %v\n", err)
		os.Exit(1)
	}
}

type envConfig struct {
	JobName      string
	ScheduleName string
	SNSTopicARN  string
}

func configFromEnv() envConfig {
	jobName := os.Getenv("JOB_NAME")
	if jobName == "" {
		jobName = jobLogHeartbeat
	}

	scheduleName := os.Getenv("SCHEDULE_NAME")
	if scheduleName == "" {
		scheduleName = defaultScheduleName
	}

	return envConfig{
		JobName:      jobName,
		ScheduleName: scheduleName,
		SNSTopicARN:  os.Getenv("SNS_TOPIC_ARN"),
	}
}

func run(ctx context.Context, out io.Writer, now time.Time, cfg envConfig) error {
	switch cfg.JobName {
	case jobLogHeartbeat:
		return writeEvent(out, now, cfg.ScheduleName, cfg.JobName, eventTypeHeartbeat, "scheduled New Relic heartbeat")
	case jobSendNotification:
		awsCfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			return fmt.Errorf("load AWS config: %w", err)
		}
		return runNotification(ctx, out, now, cfg, sns.NewFromConfig(awsCfg))
	default:
		return fmt.Errorf("unknown JOB_NAME %q", cfg.JobName)
	}
}

func runNotification(ctx context.Context, out io.Writer, now time.Time, cfg envConfig, publisher notificationPublisher) error {
	if cfg.SNSTopicARN == "" {
		return fmt.Errorf("SNS_TOPIC_ARN is required for %s", jobSendNotification)
	}

	message := fmt.Sprintf("Scheduled notification from %s at %s", cfg.ScheduleName, now.Format(time.RFC3339Nano))
	_, err := publisher.Publish(ctx, &sns.PublishInput{
		TopicArn: &cfg.SNSTopicARN,
		Subject:  stringPtr("Skill Trail scheduled notification"),
		Message:  &message,
	})
	if err != nil {
		return fmt.Errorf("publish SNS notification: %w", err)
	}

	return writeEvent(out, now, cfg.ScheduleName, cfg.JobName, eventTypeNotification, "scheduled notification email sent")
}

func writeEvent(out io.Writer, now time.Time, scheduleName string, jobName string, eventType string, message string) error {
	event := scheduledEvent{
		Level:        "info",
		EventType:    eventType,
		Message:      message,
		Timestamp:    now.Format(time.RFC3339Nano),
		Source:       "scheduled-log",
		ScheduleName: scheduleName,
		JobName:      jobName,
	}
	return json.NewEncoder(out).Encode(event)
}

func stringPtr(value string) *string {
	return &value
}
