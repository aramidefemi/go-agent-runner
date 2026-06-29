package executor

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const lockFilePath = ".runner/runner.lock"

type LockInfo struct {
	PID       int    `json:"pid"`
	RunID     string `json:"run_id"`
	StartedAt string `json:"started_at"`
}

type LockFile = LockInfo

func Acquire(workspace string, runID string, startedAt time.Time) error {
	lockPath := filepath.Join(workspace, lockFilePath)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return err
	}

	if existing, err := readLockFile(lockPath); err == nil {
		if isPIDAlive(existing.PID) {
			return ErrLockHeld
		}
	}

	payload := LockFile{
		PID:       os.Getpid(),
		RunID:     runID,
		StartedAt: startedAt.UTC().Format(time.RFC3339),
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	return os.WriteFile(lockPath, bytes, 0o644)
}

func Release(workspace string) error {
	lockPath := filepath.Join(workspace, lockFilePath)
	err := os.Remove(lockPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func ReadLock(workspace string) (*LockInfo, error) {
	lockPath := filepath.Join(workspace, lockFilePath)
	return readLockFile(lockPath)
}

func IsLockAlive(lock *LockInfo) bool {
	if lock == nil {
		return false
	}
	return isPIDAlive(lock.PID)
}

func readLockFile(path string) (*LockInfo, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lock LockInfo
	if err := json.Unmarshal(bytes, &lock); err != nil {
		return nil, err
	}
	if lock.PID <= 0 {
		return nil, errors.New("invalid pid in lock")
	}
	return &lock, nil
}

func isPIDAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	return !errors.Is(err, syscall.ESRCH)
}
