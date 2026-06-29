package config

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const FileName = "runner.yaml"

type Config struct {
	Name      string          `yaml:"name"`
	Enabled   bool            `yaml:"enabled"`
	Schedule  ScheduleConfig  `yaml:"schedule"`
	Prompt    PromptConfig    `yaml:"prompt"`
	Agent     AgentConfig     `yaml:"agent"`
	Execution ExecutionConfig `yaml:"execution"`
	Output    OutputConfig    `yaml:"output"`
	Safety    SafetyConfig    `yaml:"safety"`
}

type ScheduleConfig struct {
	Interval    string `yaml:"interval"`
	Cron        string `yaml:"cron"`
	RunOnStart  bool   `yaml:"run_on_start"`
	Missed      string `yaml:"missed"`
}

type PromptConfig struct {
	File string `yaml:"file"`
}

type AgentConfig struct {
	Command   string   `yaml:"command"`
	Args      []string `yaml:"args"`
	PromptVia string   `yaml:"prompt_via"`
}

type ExecutionConfig struct {
	Cwd      string            `yaml:"cwd"`
	Timeout  string            `yaml:"timeout"`
	EnvFile  string            `yaml:"env_file"`
	ExtraEnv map[string]string `yaml:"extra_env"`
}

type OutputConfig struct {
	LogsDir     string `yaml:"logs_dir"`
	SummaryFile string `yaml:"summary_file"`
}

type SafetyConfig struct {
	MaxConcurrent int  `yaml:"max_concurrent"`
	SkipIfLocked  bool `yaml:"skip_if_locked"`
}

func Load(workspaceRoot string) (*Config, error) {
	configPath := filepath.Join(workspaceRoot, FileName)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", configPath, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", configPath, err)
	}
	applyDefaults(&cfg)

	if err := Validate(&cfg, workspaceRoot); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func Validate(cfg *Config, workspaceRoot string) error {
	if cfg == nil {
		return errors.New("config is required")
	}
	if strings.TrimSpace(workspaceRoot) == "" {
		return errors.New("workspace root is required")
	}
	if strings.TrimSpace(cfg.Name) == "" {
		return errors.New("name is required")
	}
	if err := validateSchedule(cfg.Schedule); err != nil {
		return err
	}
	if err := validatePrompt(cfg.Prompt, workspaceRoot); err != nil {
		return err
	}
	if err := validateAgent(cfg.Agent); err != nil {
		return err
	}
	if err := validateExecution(cfg.Execution, workspaceRoot); err != nil {
		return err
	}
	if err := validateSafety(cfg.Safety); err != nil {
		return err
	}
	return nil
}

func applyDefaults(cfg *Config) {
	if cfg.Agent.PromptVia == "" {
		cfg.Agent.PromptVia = "stdin"
	}
	if cfg.Execution.Cwd == "" {
		cfg.Execution.Cwd = "."
	}
	if cfg.Execution.Timeout == "" {
		cfg.Execution.Timeout = "45m"
	}
	if cfg.Output.LogsDir == "" {
		cfg.Output.LogsDir = ".runner/logs"
	}
	if cfg.Safety.MaxConcurrent == 0 {
		cfg.Safety.MaxConcurrent = 1
	}
}

func validateSchedule(schedule ScheduleConfig) error {
	hasInterval := strings.TrimSpace(schedule.Interval) != ""
	hasCron := strings.TrimSpace(schedule.Cron) != ""
	if hasInterval == hasCron {
		return errors.New("schedule requires exactly one of interval or cron")
	}
	if hasInterval {
		if _, err := time.ParseDuration(schedule.Interval); err != nil {
			return fmt.Errorf("invalid schedule.interval: %w", err)
		}
	}
	return nil
}

func validatePrompt(prompt PromptConfig, workspaceRoot string) error {
	if strings.TrimSpace(prompt.File) == "" {
		return errors.New("prompt.file is required")
	}
	promptPath := resolvePath(workspaceRoot, prompt.File)
	info, err := os.Stat(promptPath)
	if err != nil {
		return fmt.Errorf("prompt.file not found: %w", err)
	}
	if info.IsDir() {
		return errors.New("prompt.file must be a file")
	}
	return nil
}

func validateAgent(agent AgentConfig) error {
	if strings.TrimSpace(agent.Command) == "" {
		return errors.New("agent.command is required")
	}
	if _, err := exec.LookPath(agent.Command); err != nil {
		return fmt.Errorf("agent.command not found on PATH: %w", err)
	}
	switch agent.PromptVia {
	case "stdin", "arg", "env", "none":
		return nil
	default:
		return errors.New("agent.prompt_via must be one of: stdin, arg, env, none")
	}
}

func validateExecution(execCfg ExecutionConfig, workspaceRoot string) error {
	cwdPath := resolvePath(workspaceRoot, execCfg.Cwd)
	cwdInfo, err := os.Stat(cwdPath)
	if err != nil {
		return fmt.Errorf("execution.cwd not found: %w", err)
	}
	if !cwdInfo.IsDir() {
		return errors.New("execution.cwd must be a directory")
	}

	if _, err := time.ParseDuration(execCfg.Timeout); err != nil {
		return fmt.Errorf("invalid execution.timeout: %w", err)
	}

	if strings.TrimSpace(execCfg.EnvFile) != "" {
		envPath := resolvePath(workspaceRoot, execCfg.EnvFile)
		envInfo, err := os.Stat(envPath)
		if err != nil {
			return fmt.Errorf("execution.env_file not found: %w", err)
		}
		if envInfo.IsDir() {
			return errors.New("execution.env_file must be a file")
		}
	}
	return nil
}

func validateSafety(safety SafetyConfig) error {
	if safety.MaxConcurrent < 1 {
		return errors.New("safety.max_concurrent must be >= 1")
	}
	return nil
}

func resolvePath(workspaceRoot, candidate string) string {
	if filepath.IsAbs(candidate) {
		return candidate
	}
	return filepath.Join(workspaceRoot, candidate)
}
