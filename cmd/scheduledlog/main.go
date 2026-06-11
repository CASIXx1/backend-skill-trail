package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

const (
	eventType           = "scheduled_new_relic_heartbeat"
	defaultScheduleName = "eventbridge-scheduler"
)

type heartbeatEvent struct {
	Level        string `json:"level"`
	EventType    string `json:"event_type"`
	Message      string `json:"message"`
	Timestamp    string `json:"timestamp"`
	Source       string `json:"source"`
	ScheduleName string `json:"schedule_name"`
}

func main() {
	if err := writeHeartbeat(os.Stdout, time.Now().UTC(), scheduleNameFromEnv()); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write scheduled heartbeat: %v\n", err)
		os.Exit(1)
	}
}

func scheduleNameFromEnv() string {
	if value := os.Getenv("SCHEDULE_NAME"); value != "" {
		return value
	}
	return defaultScheduleName
}

func writeHeartbeat(out *os.File, now time.Time, scheduleName string) error {
	event := heartbeatEvent{
		Level:        "info",
		EventType:    eventType,
		Message:      "scheduled New Relic heartbeat",
		Timestamp:    now.Format(time.RFC3339Nano),
		Source:       "scheduled-log",
		ScheduleName: scheduleName,
	}
	return json.NewEncoder(out).Encode(event)
}
