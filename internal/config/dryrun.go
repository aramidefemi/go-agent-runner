package config

import (
	"fmt"
	"strings"
)

func DryRunCommand(cfg *Config, workspaceRoot string) string {
	if cfg == nil {
		return ""
	}
	args := make([]string, 0, len(cfg.Agent.Args)+1)
	for _, arg := range cfg.Agent.Args {
		args = append(args, expandPlaceholders(arg, workspaceRoot, cfg.Prompt.File, "dry-run-id"))
	}
	parts := append([]string{cfg.Agent.Command}, args...)
	switch cfg.Agent.PromptVia {
	case "arg":
		parts = append(parts, "<prompt>")
	case "env":
		return strings.Join(parts, " ") + " RUNNER_PROMPT=<prompt>"
	}
	return strings.Join(parts, " ") + " < prompt_file"
}

func expandPlaceholders(value, workspaceRoot, promptFile, runID string) string {
	promptPath := resolvePath(workspaceRoot, promptFile)
	out := value
	out = strings.ReplaceAll(out, "{{workspace}}", workspaceRoot)
	out = strings.ReplaceAll(out, "{{prompt_file}}", promptPath)
	out = strings.ReplaceAll(out, "{{run_id}}", runID)
	return out
}

func ParseInterval(cfg *Config) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("config is required")
	}
	if strings.TrimSpace(cfg.Schedule.Interval) != "" {
		return cfg.Schedule.Interval, nil
	}
	return "", fmt.Errorf("no interval configured")
}
