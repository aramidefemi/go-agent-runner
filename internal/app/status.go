package app

import (
	"fmt"
	"os"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/aramidefemi/go-agent-runner/internal/config"
	"github.com/aramidefemi/go-agent-runner/internal/daemon"
	"github.com/aramidefemi/go-agent-runner/internal/executor"
	"github.com/aramidefemi/go-agent-runner/internal/store"
	"github.com/aramidefemi/go-agent-runner/internal/workspace"
)

type Snapshot struct {
	Workspace     string
	JobName       string
	ScheduleDesc  string
	Enabled       bool
	Paused        bool
	DaemonRunning bool
	DaemonPID     int
	LastTick      *time.Time
	NextRun       *time.Time
	NextRunNote   string
	LastRun       *store.Run
	Runs          []store.Run
	ActiveLock    *executor.LockInfo
}

func LoadSnapshot(workspaceRoot string) (Snapshot, error) {
	cfg, err := config.Load(workspaceRoot)
	if err != nil {
		return Snapshot{}, err
	}

	paths := workspace.NewPaths(workspaceRoot)
	st, err := OpenStore(workspaceRoot)
	if err != nil {
		return Snapshot{}, err
	}
	defer st.Close()

	daemonInfo, err := daemon.Status(workspaceRoot)
	if err != nil {
		return Snapshot{}, err
	}

	runs, err := st.ListRuns(100)
	if err != nil {
		return Snapshot{}, err
	}
	runs = filterRunsSince(runs, time.Now().Add(-24*time.Hour))

	var lastTick *time.Time
	if raw, _ := st.GetMeta("last_tick"); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			lastTick = &t
		}
	}

	snap := Snapshot{
		Workspace:     workspaceRoot,
		JobName:       cfg.Name,
		ScheduleDesc:  formatSchedule(cfg),
		Enabled:       cfg.Enabled,
		Paused:        isPaused(paths.PausedFile()),
		DaemonRunning: daemonInfo.Running,
		DaemonPID:     daemonInfo.PID,
		LastTick:      lastTick,
		Runs:          runs,
	}
	if len(runs) > 0 {
		snap.LastRun = &runs[0]
	}

	if lock, err := executor.ReadLock(workspaceRoot); err == nil && executor.IsLockAlive(lock) {
		snap.ActiveLock = lock
	}

	snap.NextRun, snap.NextRunNote = computeNextRun(cfg, snap)
	return snap, nil
}

func filterRunsSince(runs []store.Run, since time.Time) []store.Run {
	out := make([]store.Run, 0, len(runs))
	for _, r := range runs {
		if !r.StartedAt.Before(since) {
			out = append(out, r)
		}
	}
	return out
}

func isPaused(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func formatSchedule(cfg *config.Config) string {
	if cfg.Schedule.Interval != "" {
		return "every " + cfg.Schedule.Interval
	}
	if cfg.Schedule.Cron != "" {
		return "cron " + cfg.Schedule.Cron
	}
	return "unknown"
}

func computeNextRun(cfg *config.Config, snap Snapshot) (*time.Time, string) {
	if !snap.Enabled {
		return nil, "disabled"
	}
	if snap.Paused {
		return nil, "paused"
	}
	if !snap.DaemonRunning {
		return nil, "daemon stopped"
	}
	if snap.ActiveLock != nil {
		return nil, "running now"
	}

	now := time.Now()
	if cfg.Schedule.Interval != "" {
		interval, err := time.ParseDuration(cfg.Schedule.Interval)
		if err != nil {
			return nil, "invalid interval"
		}
		base := now
		if snap.LastTick != nil {
			base = snap.LastTick.Add(interval)
			if base.Before(now) {
				base = now
			}
		}
		return &base, ""
	}

	if cfg.Schedule.Cron != "" {
		schedule, err := cron.ParseStandard(cfg.Schedule.Cron)
		if err != nil {
			return nil, "invalid cron"
		}
		next := schedule.Next(now)
		return &next, ""
	}
	return nil, "no schedule"
}

// PrintStatus writes plain-text status for non-TTY environments.
func PrintStatus(w interface{ Write([]byte) (int, error) }, workspaceRoot string) error {
	snap, err := LoadSnapshot(workspaceRoot)
	if err != nil {
		return err
	}
	up := "down"
	if snap.DaemonRunning {
		up = "up"
	}
	_, _ = fmt.Fprintf(w, "daemon: %s\n", up)
	if snap.DaemonRunning {
		_, _ = fmt.Fprintf(w, "pid: %d\n", snap.DaemonPID)
	}
	if snap.LastTick != nil {
		_, _ = fmt.Fprintf(w, "last_tick: %s\n", snap.LastTick.UTC().Format(time.RFC3339))
	}
	if snap.NextRun != nil {
		_, _ = fmt.Fprintf(w, "next_run: %s\n", snap.NextRun.UTC().Format(time.RFC3339))
	} else if snap.NextRunNote != "" {
		_, _ = fmt.Fprintf(w, "next_run: %s\n", snap.NextRunNote)
	}
	if snap.LastRun != nil {
		exit := -1
		if snap.LastRun.ExitCode != nil {
			exit = *snap.LastRun.ExitCode
		}
		_, _ = fmt.Fprintf(w, "last_run: %s exit=%d duration=%s skipped=%v\n",
			snap.LastRun.ID, exit, snap.LastRun.Duration.Round(time.Millisecond), snap.LastRun.Skipped)
		if snap.LastRun.SkipReason != "" {
			_, _ = fmt.Fprintf(w, "skip_reason: %s\n", snap.LastRun.SkipReason)
		}
	}
	return nil
}
