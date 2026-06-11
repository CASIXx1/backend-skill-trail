package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sns"
)

type fakePublisher struct {
	input *sns.PublishInput
	err   error
}

func (f *fakePublisher) Publish(ctx context.Context, params *sns.PublishInput, optFns ...func(*sns.Options)) (*sns.PublishOutput, error) {
	f.input = params
	if f.err != nil {
		return nil, f.err
	}
	return &sns.PublishOutput{}, nil
}

func TestRunLogHeartbeatOutputsExpectedJSON(t *testing.T) {
	var out bytes.Buffer
	now := time.Date(2026, 6, 11, 12, 34, 56, 789, time.UTC)
	cfg := envConfig{JobName: jobLogHeartbeat, ScheduleName: defaultScheduleName}

	if err := run(context.Background(), &out, now, cfg); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	var got scheduledEvent
	if err := json.NewDecoder(&out).Decode(&got); err != nil {
		t.Fatalf("decode event failed: %v", err)
	}

	if got.EventType != eventTypeHeartbeat {
		t.Fatalf("unexpected event_type: %q", got.EventType)
	}
	if got.ScheduleName != defaultScheduleName {
		t.Fatalf("unexpected schedule_name: %q", got.ScheduleName)
	}
	if got.JobName != jobLogHeartbeat {
		t.Fatalf("unexpected job_name: %q", got.JobName)
	}
	if _, err := time.Parse(time.RFC3339Nano, got.Timestamp); err != nil {
		t.Fatalf("timestamp is not RFC3339Nano: %v", err)
	}
}

func TestConfigFromEnvUsesCustomValues(t *testing.T) {
	t.Setenv("JOB_NAME", jobSendNotification)
	t.Setenv("SCHEDULE_NAME", "infra-skill-trail-dev-scheduled-notification")
	t.Setenv("SNS_TOPIC_ARN", "arn:aws:sns:ap-northeast-1:123456789012:topic")

	cfg := configFromEnv()
	if cfg.JobName != jobSendNotification {
		t.Fatalf("unexpected job name: %q", cfg.JobName)
	}
	if cfg.ScheduleName != "infra-skill-trail-dev-scheduled-notification" {
		t.Fatalf("unexpected schedule name: %q", cfg.ScheduleName)
	}
	if cfg.SNSTopicARN != "arn:aws:sns:ap-northeast-1:123456789012:topic" {
		t.Fatalf("unexpected SNS topic ARN: %q", cfg.SNSTopicARN)
	}
}

func TestConfigFromEnvDefaults(t *testing.T) {
	t.Setenv("JOB_NAME", "")
	t.Setenv("SCHEDULE_NAME", "")

	cfg := configFromEnv()
	if cfg.JobName != jobLogHeartbeat {
		t.Fatalf("unexpected default job name: %q", cfg.JobName)
	}
	if cfg.ScheduleName != defaultScheduleName {
		t.Fatalf("unexpected default schedule name: %q", cfg.ScheduleName)
	}
}

func TestRunNotificationPublishesSNSAndLogs(t *testing.T) {
	var out bytes.Buffer
	now := time.Date(2026, 6, 11, 12, 34, 56, 789, time.UTC)
	cfg := envConfig{
		JobName:      jobSendNotification,
		ScheduleName: "infra-skill-trail-dev-scheduled-notification",
		SNSTopicARN:  "arn:aws:sns:ap-northeast-1:123456789012:topic",
	}
	publisher := &fakePublisher{}

	if err := runNotification(context.Background(), &out, now, cfg, publisher); err != nil {
		t.Fatalf("run notification failed: %v", err)
	}
	if publisher.input == nil {
		t.Fatal("expected SNS publish input")
	}
	if got := *publisher.input.TopicArn; got != cfg.SNSTopicARN {
		t.Fatalf("unexpected topic ARN: %q", got)
	}

	var got scheduledEvent
	if err := json.NewDecoder(&out).Decode(&got); err != nil {
		t.Fatalf("decode event failed: %v", err)
	}
	if got.EventType != eventTypeNotification {
		t.Fatalf("unexpected event_type: %q", got.EventType)
	}
	if got.JobName != jobSendNotification {
		t.Fatalf("unexpected job_name: %q", got.JobName)
	}
}

func TestRunNotificationRequiresTopicARN(t *testing.T) {
	err := runNotification(context.Background(), &bytes.Buffer{}, time.Now(), envConfig{JobName: jobSendNotification}, &fakePublisher{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunNotificationReturnsPublishError(t *testing.T) {
	publisher := &fakePublisher{err: errors.New("publish failed")}
	err := runNotification(context.Background(), &bytes.Buffer{}, time.Now(), envConfig{
		JobName:     jobSendNotification,
		SNSTopicARN: "arn:aws:sns:ap-northeast-1:123456789012:topic",
	}, publisher)
	if err == nil {
		t.Fatal("expected error")
	}
}
