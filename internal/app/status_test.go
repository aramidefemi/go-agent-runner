package app

import (
	"testing"
	"time"

	"github.com/aramidefemi/go-agent-runner/internal/config"
	"github.com/aramidefemi/go-agent-runner/internal/store"
)

func TestComputeNextRunPaused(t *testing.T) {
	snap := Snapshot{Enabled: true, Paused: true, DaemonRunning: true}
	next, note := computeNextRun(&config.Config{Schedule: config.ScheduleConfig{Interval: "1h"}}, snap)
	if next != nil || note != "paused" {
		t.Fatalf("expected paused, got %v %q", next, note)
	}
}

func TestComputeNextRunInterval(t *testing.T) {
	last := time.Now().Add(-30 * time.Minute)
	snap := Snapshot{Enabled: true, DaemonRunning: true, LastTick: &last}
	next, note := computeNextRun(&config.Config{Schedule: config.ScheduleConfig{Interval: "1h"}}, snap)
	if note != "" || next == nil {
		t.Fatalf("expected next run, got %v %q", next, note)
	}
	if next.Before(time.Now()) {
		t.Fatalf("next run should not be in the past: %v", next)
	}
}

func TestFilterRunsSince(t *testing.T) {
	now := time.Now()
	runs := filterRunsSince([]store.Run{
		{StartedAt: now.Add(-48 * time.Hour)},
		{StartedAt: now.Add(-1 * time.Hour)},
	}, now.Add(-24*time.Hour))
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
}
