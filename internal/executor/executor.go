package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/aramidefemi/go-agent-runner/internal/config"
	"github.com/aramidefemi/go-agent-runner/internal/store"
)

const defaultKillGrace = 30 * time.Second

type RunStore interface {
	InsertRun(run store.Run) error
}

type Executor struct {
	Config config.Config
	Store  RunStore
	Now    func() time.Time
}

func (e *Executor) RunOnce(ctx context.Context, workspaceRoot string) (*store.Run, error) {
	if workspaceRoot == "" {
		return nil, errors.New("workspace root is required")
	}
	if e.Config.Agent.Command == "" {
		return nil, errors.New("agent command is required")
	}
	if e.Config.Prompt.File == "" {
		return nil, errors.New("prompt file is required")
	}

	now := e.Now
	if now == nil {
		now = time.Now
	}

	runID := uuid.NewString()
	startedAt := now().UTC()
	run := &store.Run{
		ID:        runID,
		Workspace: workspaceRoot,
		JobName:   e.Config.Name,
		StartedAt: startedAt,
	}

	if err := Acquire(workspaceRoot, runID, startedAt); err != nil {
		if errors.Is(err, ErrLockHeld) && e.Config.Safety.SkipIfLocked {
			finishedAt := now().UTC()
			run.Skipped = true
			run.SkipReason = "locked"
			run.FinishedAt = &finishedAt
			run.Duration = finishedAt.Sub(startedAt)
			return run, e.saveRun(*run)
		}
		return nil, err
	}
	defer func() { _ = Release(workspaceRoot) }()

	promptPath := resolvePath(workspaceRoot, e.Config.Prompt.File)
	promptBytes, err := os.ReadFile(promptPath)
	if err != nil {
		return nil, err
	}

	args := ExpandArgs(e.Config.Agent.Args, map[string]string{
		"workspace":   workspaceRoot,
		"prompt_file": promptPath,
		"run_id":      runID,
	})

	envMap := baseEnvMap()
	envMap["RUNNER_WORKSPACE"] = workspaceRoot
	envMap["RUNNER_PROMPT_FILE"] = promptPath
	envMap["RUNNER_RUN_ID"] = runID
	envMap["RUNNER_STARTED_AT"] = startedAt.Format(time.RFC3339)
	envMap["RUNNER_JOB_NAME"] = e.Config.Name

	if envFile := e.Config.Execution.EnvFile; envFile != "" {
		loaded, err := loadEnvFile(resolvePath(workspaceRoot, envFile))
		if err != nil {
			return nil, err
		}
		for k, v := range loaded {
			envMap[k] = v
		}
	}
	for k, v := range e.Config.Execution.ExtraEnv {
		envMap[k] = v
	}

	var stdin io.Reader
	switch e.Config.Agent.PromptVia {
	case "", "stdin":
		stdin = bytes.NewReader(promptBytes)
	case "arg":
		args = append(args, string(promptBytes))
	case "env":
		envMap["RUNNER_PROMPT"] = string(promptBytes)
	case "none":
	default:
		return nil, fmt.Errorf("unsupported prompt_via: %s", e.Config.Agent.PromptVia)
	}

	logPath, logFile, err := e.openLogFile(workspaceRoot, runID)
	if err != nil {
		return nil, err
	}
	defer logFile.Close()
	run.LogPath = logPath

	execCtx, cancel := e.withTimeout(ctx)
	defer cancel()

	cmd := exec.Command(e.Config.Agent.Command, args...)
	cmd.Stdin = stdin
	cmd.Stdout = io.MultiWriter(logFile)
	cmd.Stderr = io.MultiWriter(logFile)
	cmd.Env = envMapToList(envMap)
	cmd.Dir = resolvePath(workspaceRoot, e.Config.Execution.Cwd)
	if e.Config.Execution.Cwd == "" {
		cmd.Dir = workspaceRoot
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	waitErr := waitWithGracefulTermination(execCtx, cmd, defaultKillGrace)
	finishedAt := now().UTC()
	run.FinishedAt = &finishedAt
	run.Duration = finishedAt.Sub(startedAt)

	if waitErr == nil {
		code := 0
		run.ExitCode = &code
	} else {
		var exitErr *exec.ExitError
		switch {
		case errors.Is(execCtx.Err(), context.DeadlineExceeded):
			run.TimedOut = true
			if errors.As(waitErr, &exitErr) {
				code := exitErr.ExitCode()
				run.ExitCode = &code
			}
		case errors.As(waitErr, &exitErr):
			code := exitErr.ExitCode()
			run.ExitCode = &code
		default:
			return run, waitErr
		}
	}

	if run.ExitCode != nil && *run.ExitCode == 0 && !run.TimedOut && e.Config.Output.SummaryFile != "" {
		summaryPath := resolvePath(workspaceRoot, e.Config.Output.SummaryFile)
		if _, err := os.Stat(summaryPath); err == nil {
			run.SummaryPath = summaryPath
		}
	}

	if err := e.saveRun(*run); err != nil {
		return run, err
	}
	return run, nil
}

func (e *Executor) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if e.Config.Execution.Timeout == "" {
		return ctx, func() {}
	}
	d, err := time.ParseDuration(e.Config.Execution.Timeout)
	if err != nil || d <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}

func (e *Executor) openLogFile(workspaceRoot, runID string) (string, *os.File, error) {
	logsDir := e.Config.Output.LogsDir
	if logsDir == "" {
		logsDir = ".runner/logs"
	}
	logsPath := resolvePath(workspaceRoot, logsDir)
	if err := os.MkdirAll(logsPath, 0o755); err != nil {
		return "", nil, err
	}
	logPath := filepath.Join(logsPath, runID+".log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", nil, err
	}
	return logPath, file, nil
}

func (e *Executor) saveRun(run store.Run) error {
	if e.Store == nil {
		return nil
	}
	return e.Store.InsertRun(run)
}

func resolvePath(root, value string) string {
	if filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(root, value)
}

func baseEnvMap() map[string]string {
	env := make(map[string]string)
	for _, entry := range os.Environ() {
		parts := bytes.SplitN([]byte(entry), []byte("="), 2)
		if len(parts) != 2 {
			continue
		}
		env[string(parts[0])] = string(parts[1])
	}
	return env
}

func envMapToList(envMap map[string]string) []string {
	keys := make([]string, 0, len(envMap))
	for key := range envMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+envMap[key])
	}
	return out
}

func waitWithGracefulTermination(ctx context.Context, cmd *exec.Cmd, killGrace time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
		}
		select {
		case err := <-done:
			return err
		case <-time.After(killGrace):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			return <-done
		}
	}
}
