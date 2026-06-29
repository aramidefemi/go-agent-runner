package control

import (
	"os"
	"path/filepath"

	"github.com/aramidefemi/go-agent-runner/internal/workspace"
)

const triggerFile = "trigger"

func TriggerPath(workspaceRoot string) string {
	return filepath.Join(workspace.NewPaths(workspaceRoot).RunnerDir(), triggerFile)
}

func RequestRunNow(workspaceRoot string) error {
	if err := workspace.EnsureRunnerDir(workspaceRoot); err != nil {
		return err
	}
	return os.WriteFile(TriggerPath(workspaceRoot), []byte("run"), 0o644)
}

func ConsumeTrigger(workspaceRoot string) bool {
	path := TriggerPath(workspaceRoot)
	if _, err := os.Stat(path); err != nil {
		return false
	}
	_ = os.Remove(path)
	return true
}

func SetPaused(workspaceRoot string, paused bool) error {
	path := workspace.NewPaths(workspaceRoot).PausedFile()
	if paused {
		if err := workspace.EnsureRunnerDir(workspaceRoot); err != nil {
			return err
		}
		return os.WriteFile(path, []byte("paused"), 0o644)
	}
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func IsPaused(workspaceRoot string) bool {
	_, err := os.Stat(workspace.NewPaths(workspaceRoot).PausedFile())
	return err == nil
}
