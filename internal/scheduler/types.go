package scheduler

import (
	"context"
	"time"
)

type MissedBehavior string

const (
	MissedRunOnce MissedBehavior = "run_once"
	MissedSkip    MissedBehavior = "skip"
)

type GracefulShutdownMode string

const (
	GracefulWait GracefulShutdownMode = "wait"
	GracefulKill GracefulShutdownMode = "kill"
)

type Config struct {
	WorkspaceRoot    string
	Enabled          bool
	Interval         time.Duration
	Cron             string
	RunOnStart       bool
	Missed           MissedBehavior
	GracefulShutdown GracefulShutdownMode
}

type Executor interface {
	RunOnce(ctx context.Context) error
}

type Store interface {
	SetMeta(ctx context.Context, key, value string) error
}
