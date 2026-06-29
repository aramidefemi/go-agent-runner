package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	runnerDirName = ".runner"
	pidFileName   = "runner.pid"
	metaFileName  = "meta.json"
)

var ErrAlreadyRunning = errors.New("daemon already running")

type StatusInfo struct {
	Running bool
	PID     int
	Meta    map[string]string
}

// BuildDaemonCommand can be replaced by callers that need custom bootstrap.
var BuildDaemonCommand = defaultBuildDaemonCommand

func Start(workspaceRoot string) error {
	if IsRunning(workspaceRoot) {
		return ErrAlreadyRunning
	}

	if err := os.MkdirAll(filepath.Join(workspaceRoot, runnerDirName), 0o755); err != nil {
		return err
	}

	cmd, err := BuildDaemonCommand(workspaceRoot)
	if err != nil {
		return err
	}
	if cmd == nil {
		return errors.New("daemon: nil command")
	}

	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer devNull.Close()

	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return err
	}

	return writePID(workspaceRoot, cmd.Process.Pid)
}

func Stop(workspaceRoot string) error {
	pid, err := readPID(workspaceRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}

	if err := os.Remove(pidFilePath(workspaceRoot)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return nil
}

func Status(workspaceRoot string) (StatusInfo, error) {
	meta, err := readMeta(workspaceRoot)
	if err != nil {
		return StatusInfo{}, err
	}

	pid, err := readPID(workspaceRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return StatusInfo{
				Running: false,
				PID:     0,
				Meta:    meta,
			}, nil
		}
		return StatusInfo{}, err
	}

	return StatusInfo{
		Running: processExists(pid),
		PID:     pid,
		Meta:    meta,
	}, nil
}

func IsRunning(workspaceRoot string) bool {
	pid, err := readPID(workspaceRoot)
	if err != nil {
		return false
	}
	return processExists(pid)
}

func defaultBuildDaemonCommand(workspaceRoot string) (*exec.Cmd, error) {
	exePath, err := os.Executable()
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(exePath, "daemon-loop", "--workspace", workspaceRoot)
	return cmd, nil
}

func WaitForStop(ctx context.Context, workspaceRoot string, pollEvery time.Duration) error {
	if pollEvery <= 0 {
		pollEvery = 250 * time.Millisecond
	}

	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()

	for {
		if !IsRunning(workspaceRoot) {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func readMeta(workspaceRoot string) (map[string]string, error) {
	metaPath := filepath.Join(workspaceRoot, runnerDirName, metaFileName)
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return map[string]string{}, nil
	}

	meta := map[string]string{}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, err
	}
	return meta, nil
}

func pidFilePath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, runnerDirName, pidFileName)
}

func writePID(workspaceRoot string, pid int) error {
	content := []byte(strconv.Itoa(pid))
	return os.WriteFile(pidFilePath(workspaceRoot), content, 0o644)
}

func readPID(workspaceRoot string) (int, error) {
	raw, err := os.ReadFile(pidFilePath(workspaceRoot))
	if err != nil {
		return 0, err
	}

	text := strings.TrimSpace(string(raw))
	if text == "" {
		return 0, errors.New("daemon: pid file is empty")
	}

	pid, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("daemon: invalid pid %q: %w", text, err)
	}
	if pid <= 0 {
		return 0, fmt.Errorf("daemon: invalid pid %d", pid)
	}
	return pid, nil
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}

	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
