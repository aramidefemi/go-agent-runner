package app

import (
	"context"
	"fmt"
	"time"

	"github.com/aramidefemi/go-agent-runner/internal/config"
	"github.com/aramidefemi/go-agent-runner/internal/executor"
	"github.com/aramidefemi/go-agent-runner/internal/scheduler"
	"github.com/aramidefemi/go-agent-runner/internal/store"
	"github.com/aramidefemi/go-agent-runner/internal/workspace"
)

func OpenStore(workspaceRoot string) (*store.Store, error) {
	paths := workspace.NewPaths(workspaceRoot)
	if err := workspace.EnsureRunnerDir(workspaceRoot); err != nil {
		return nil, err
	}
	st, err := store.New(paths.StateDB())
	if err != nil {
		return nil, err
	}
	if err := st.Migrate(); err != nil {
		_ = st.Close()
		return nil, err
	}
	return st, nil
}

func NewExecutor(cfg *config.Config, st *store.Store) *executor.Executor {
	return &executor.Executor{Config: *cfg, Store: st}
}

func RunDaemonLoop(ctx context.Context, workspaceRoot string) error {
	cfg, err := config.Load(workspaceRoot)
	if err != nil {
		return err
	}
	st, err := OpenStore(workspaceRoot)
	if err != nil {
		return err
	}
	defer st.Close()

	schedCfg := scheduler.Config{
		WorkspaceRoot: workspaceRoot,
		Enabled:       cfg.Enabled,
		RunOnStart:    cfg.Schedule.RunOnStart,
	}
	if cfg.Schedule.Missed == "skip" {
		schedCfg.Missed = scheduler.MissedSkip
	} else {
		schedCfg.Missed = scheduler.MissedRunOnce
	}
	if cfg.Schedule.Interval != "" {
		d, err := time.ParseDuration(cfg.Schedule.Interval)
		if err != nil {
			return fmt.Errorf("parse interval: %w", err)
		}
		schedCfg.Interval = d
	} else {
		schedCfg.Cron = cfg.Schedule.Cron
	}

	exec := NewExecutor(cfg, st)
	sched := scheduler.New(schedCfg, &executorAdapter{exec: exec, workspace: workspaceRoot}, &storeAdapter{st: st})
	return sched.Run(ctx)
}

type executorAdapter struct {
	exec      *executor.Executor
	workspace string
}

func (a *executorAdapter) RunOnce(ctx context.Context) error {
	_, err := a.exec.RunOnce(ctx, a.workspace)
	return err
}

type storeAdapter struct {
	st *store.Store
}

func (a *storeAdapter) SetMeta(ctx context.Context, key, value string) error {
	return a.st.SetMeta(key, value)
}
