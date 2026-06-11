package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestWriteHeartbeatOutputsExpectedJSON(t *testing.T) {
	readFile, writeFile, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe failed: %v", err)
	}
	defer readFile.Close()

	now := time.Date(2026, 6, 11, 12, 34, 56, 789, time.UTC)
	if err := writeHeartbeat(writeFile, now, defaultScheduleName); err != nil {
		t.Fatalf("write heartbeat failed: %v", err)
	}
	if err := writeFile.Close(); err != nil {
		t.Fatalf("close write pipe failed: %v", err)
	}

	var got heartbeatEvent
	if err := json.NewDecoder(readFile).Decode(&got); err != nil {
		t.Fatalf("decode heartbeat failed: %v", err)
	}

	if got.EventType != eventType {
		t.Fatalf("unexpected event_type: %q", got.EventType)
	}
	if got.ScheduleName != defaultScheduleName {
		t.Fatalf("unexpected schedule_name: %q", got.ScheduleName)
	}
	if got.Source != "scheduled-log" {
		t.Fatalf("unexpected source: %q", got.Source)
	}
	if _, err := time.Parse(time.RFC3339Nano, got.Timestamp); err != nil {
		t.Fatalf("timestamp is not RFC3339Nano: %v", err)
	}
}

func TestScheduleNameFromEnvUsesCustomName(t *testing.T) {
	t.Setenv("SCHEDULE_NAME", "infra-skill-trail-dev-scheduled-log")

	if got := scheduleNameFromEnv(); got != "infra-skill-trail-dev-scheduled-log" {
		t.Fatalf("unexpected schedule name: %q", got)
	}
}

func TestScheduleNameFromEnvDefaults(t *testing.T) {
	t.Setenv("SCHEDULE_NAME", "")

	if got := scheduleNameFromEnv(); got != defaultScheduleName {
		t.Fatalf("unexpected default schedule name: %q", got)
	}
}
