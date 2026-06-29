package workspace

import "path/filepath"

type Paths struct {
	workspaceRoot string
}

func NewPaths(workspaceRoot string) Paths {
	return Paths{workspaceRoot: filepath.Clean(workspaceRoot)}
}

func (p Paths) RunnerDir() string {
	return filepath.Join(p.workspaceRoot, ".runner")
}

func (p Paths) LogsDir() string {
	return filepath.Join(p.RunnerDir(), "logs")
}

func (p Paths) StateDB() string {
	return filepath.Join(p.RunnerDir(), "state.db")
}

func (p Paths) LockFile() string {
	return filepath.Join(p.RunnerDir(), "runner.lock")
}

func (p Paths) PIDFile() string {
	return filepath.Join(p.RunnerDir(), "runner.pid")
}

func (p Paths) PausedFile() string {
	return filepath.Join(p.RunnerDir(), "paused")
}
