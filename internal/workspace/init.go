package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultConfig = `name: my-workspace
enabled: true

schedule:
  interval: 1h

prompt:
  file: bootstrap.md

agent:
  command: agent
  args:
    - --print
    - --trust
    - --force
    - --workspace
    - "{{workspace}}"
  prompt_via: stdin

execution:
  cwd: .
  timeout: 45m

output:
  logs_dir: .runner/logs
  summary_file: .runner/last-summary.md

safety:
  max_concurrent: 1
  skip_if_locked: true
`

var gitignoreEntries = []string{
	".runner/logs/",
	".runner/*.pid",
	".runner/runner.lock",
	".runner/state.db",
}

type InitResult struct {
	CreatedConfig bool
	CreatedLogs   bool
	UpdatedIgnore bool
}

func Init(workspaceRoot string) (InitResult, error) {
	var result InitResult
	paths := NewPaths(workspaceRoot)

	if err := os.MkdirAll(paths.RunnerDir(), 0o755); err != nil {
		return result, err
	}

	configPath := filepath.Join(workspaceRoot, "runner.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := os.WriteFile(configPath, []byte(defaultConfig), 0o644); err != nil {
			return result, err
		}
		result.CreatedConfig = true
	}

	if err := os.MkdirAll(paths.LogsDir(), 0o755); err != nil {
		return result, err
	}
	result.CreatedLogs = true

	updated, err := appendGitignore(workspaceRoot)
	if err != nil {
		return result, err
	}
	result.UpdatedIgnore = updated
	return result, nil
}

func appendGitignore(workspaceRoot string) (bool, error) {
	path := filepath.Join(workspaceRoot, ".gitignore")
	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return false, err
	}

	var missing []string
	for _, entry := range gitignoreEntries {
		if !strings.Contains(existing, entry) {
			missing = append(missing, entry)
		}
	}
	if len(missing) == 0 {
		return false, nil
	}

	var b strings.Builder
	if existing != "" {
		b.WriteString(strings.TrimRight(existing, "\n"))
		b.WriteString("\n")
	}
	for _, entry := range missing {
		b.WriteString(entry)
		b.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func EnsureRunnerDir(workspaceRoot string) error {
	paths := NewPaths(workspaceRoot)
	if err := os.MkdirAll(paths.LogsDir(), 0o755); err != nil {
		return fmt.Errorf("create logs dir: %w", err)
	}
	return nil
}
