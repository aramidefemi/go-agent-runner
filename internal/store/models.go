package store

import "time"

type Run struct {
	ID          string
	Workspace   string
	JobName     string
	StartedAt   time.Time
	FinishedAt  *time.Time
	Duration    time.Duration
	ExitCode    *int
	TimedOut    bool
	Skipped     bool
	SkipReason  string
	LogPath     string
	SummaryPath string
}
