package scheduler

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/robfig/cron/v3"
)

const (
	pausedFileName = "paused"
	runnerDirName  = ".runner"
	lastTickMeta   = "last_tick"
)

type Scheduler struct {
	config   Config
	executor Executor
	store    Store

	mu           sync.Mutex
	activeCancel context.CancelFunc
}

func New(config Config, executor Executor, store Store) *Scheduler {
	return &Scheduler{
		config:   normalizeConfig(config),
		executor: executor,
		store:    store,
	}
}

func (s *Scheduler) Run(ctx context.Context) error {
	if s.executor == nil {
		return errors.New("scheduler: executor is nil")
	}

	runCtx, stopSignals := signal.NotifyContext(ctx, syscall.SIGTERM)
	defer stopSignals()

	if !s.isEnabled() {
		<-runCtx.Done()
		return s.handleShutdown(runCtx)
	}

	if s.config.Interval > 0 {
		return s.runInterval(runCtx)
	}
	if strings.TrimSpace(s.config.Cron) != "" {
		return s.runCron(runCtx)
	}

	return errors.New("scheduler: schedule not configured")
}

func (s *Scheduler) runInterval(ctx context.Context) error {
	if s.config.RunOnStart {
		s.runTickIfAllowed(ctx)
	}

	ticker := time.NewTicker(s.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return s.handleShutdown(ctx)
		case <-ticker.C:
			s.runTickIfAllowed(ctx)
		}
	}
}

func (s *Scheduler) runCron(ctx context.Context) error {
	schedule, err := cron.ParseStandard(s.config.Cron)
	if err != nil {
		return err
	}

	location := time.Local
	nextDue := schedule.Next(time.Now().In(location))

	for {
		waitFor := time.Until(nextDue)
		if waitFor < 0 {
			waitFor = 0
		}

		timer := time.NewTimer(waitFor)
		select {
		case <-ctx.Done():
			timer.Stop()
			return s.handleShutdown(ctx)
		case <-timer.C:
		}

		now := time.Now().In(location)
		shouldRun := true
		if s.config.Missed == MissedSkip && now.After(nextDue) && now.Sub(nextDue) > 2*time.Second {
			shouldRun = false
		}
		if shouldRun {
			s.runTickIfAllowed(ctx)
		}

		nextDue = schedule.Next(now)
	}
}

func (s *Scheduler) runTickIfAllowed(ctx context.Context) {
	if !s.isEnabled() || s.isPaused() {
		return
	}

	s.recordTick(ctx)

	runCtx, cancel := context.WithCancel(ctx)
	s.setActiveCancel(cancel)
	defer s.clearActiveCancel()
	defer cancel()

	_ = s.executor.RunOnce(runCtx)
}

func (s *Scheduler) setActiveCancel(cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeCancel = cancel
}

func (s *Scheduler) clearActiveCancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeCancel = nil
}

func (s *Scheduler) handleShutdown(ctx context.Context) error {
	if s.config.GracefulShutdown == GracefulKill {
		s.mu.Lock()
		cancel := s.activeCancel
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}

	return ctx.Err()
}

func (s *Scheduler) isPaused() bool {
	workspaceRoot := s.config.WorkspaceRoot
	if workspaceRoot == "" {
		workspaceRoot = "."
	}

	pausedPath := filepath.Join(workspaceRoot, runnerDirName, pausedFileName)
	_, err := os.Stat(pausedPath)
	return err == nil
}

func (s *Scheduler) recordTick(ctx context.Context) {
	if s.store == nil {
		return
	}

	_ = s.store.SetMeta(ctx, lastTickMeta, time.Now().UTC().Format(time.RFC3339))
}

func (s *Scheduler) isEnabled() bool {
	return s.config.Enabled
}

func normalizeConfig(in Config) Config {
	out := in
	if out.WorkspaceRoot == "" {
		out.WorkspaceRoot = "."
	}
	if out.Missed == "" {
		out.Missed = MissedRunOnce
	}
	if out.GracefulShutdown == "" {
		out.GracefulShutdown = GracefulWait
	}
	return out
}
