package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/aramidefemi/go-agent-runner/internal/app"
	"github.com/aramidefemi/go-agent-runner/internal/config"
	"github.com/aramidefemi/go-agent-runner/internal/daemon"
	"github.com/aramidefemi/go-agent-runner/internal/logs"
	"github.com/aramidefemi/go-agent-runner/internal/store"
	"github.com/aramidefemi/go-agent-runner/internal/tui"
	"github.com/aramidefemi/go-agent-runner/internal/workspace"
)

func Execute() error {
	return NewRootCommand().Execute()
}

func NewRootCommand() *cobra.Command {
	var workspaceFlag string
	rootCmd := &cobra.Command{
		Use:           "runner",
		Short:         "Local scheduler and process supervisor for AI agent runs",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.PersistentFlags().StringVar(&workspaceFlag, "workspace", "", "workspace path (default: current directory)")

	workspaceArg := func() (string, error) {
		if workspaceFlag != "" {
			return filepath.Abs(workspaceFlag)
		}
		return os.Getwd()
	}

	rootCmd.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Create runner.yaml and .runner/ skeleton in cwd",
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := workspaceArg()
			if err != nil {
				return err
			}
			result, err := workspace.Init(ws)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "workspace: %s\n", ws)
			fmt.Fprintf(w, "runner.yaml: %s\n", createdOrExists(result.CreatedConfig))
			fmt.Fprintf(w, ".runner/logs/: %s\n", createdOrExists(result.CreatedLogs))
			fmt.Fprintf(w, ".gitignore: %s\n", updatedOrUnchanged(result.UpdatedIgnore))
			fmt.Fprintln(w)
			fmt.Fprintln(w, "next:")
			fmt.Fprintln(w, "  1. Create bootstrap.md (or edit prompt.file in runner.yaml)")
			fmt.Fprintln(w, "  2. runner validate")
			fmt.Fprintln(w, "  3. runner run")
			fmt.Fprintln(w, "  4. runner start")
			return nil
		},
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "Parse config, check agent binary exists, dry-run command",
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := workspaceArg()
			if err != nil {
				return err
			}
			cfg, err := config.Load(ws)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "valid: true")
			fmt.Fprintf(cmd.OutOrStdout(), "dry-run: %s\n", config.DryRunCommand(cfg, ws))
			return nil
		},
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "run",
		Short: "Run once now in foreground (ignore schedule)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := workspaceArg()
			if err != nil {
				return err
			}
			cfg, err := config.Load(ws)
			if err != nil {
				return err
			}
			st, err := app.OpenStore(ws)
			if err != nil {
				return err
			}
			defer st.Close()

			run, runErr := app.NewExecutor(cfg, st).RunOnce(cmd.Context(), ws)
			if run == nil {
				return runErr
			}
			printRunResult(cmd.OutOrStdout(), run)
			return runErr
		},
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "start",
		Short: "Start background scheduler daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := workspaceArg()
			if err != nil {
				return err
			}
			if err := daemon.Start(ws); err != nil {
				return err
			}
			info, _ := daemon.Status(ws)
			fmt.Fprintf(cmd.OutOrStdout(), "daemon started (pid=%d)\n", info.PID)
			return nil
		},
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "stop",
		Short: "Stop daemon (SIGTERM)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := workspaceArg()
			if err != nil {
				return err
			}
			if err := daemon.Stop(ws); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "daemon stopped")
			return nil
		},
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Daemon up/down, next tick, last run summary (opens TUI when interactive)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := workspaceArg()
			if err != nil {
				return err
			}
			if isatty.IsTerminal(os.Stdout.Fd()) {
				return tui.Run(ws)
			}
			return app.PrintStatus(cmd.OutOrStdout(), ws)
		},
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "logs [run-id]",
		Short: "Tail log file (latest run if omitted)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := workspaceArg()
			if err != nil {
				return err
			}
			st, err := app.OpenStore(ws)
			if err != nil {
				return err
			}
			defer st.Close()

			var run *store.Run
			if len(args) == 1 {
				run, err = st.GetRun(args[0])
			} else {
				runs, listErr := st.ListRuns(1)
				if listErr != nil {
					return listErr
				}
				if len(runs) == 0 {
					return errors.New("no runs found")
				}
				run = &runs[0]
			}
			if err != nil {
				return err
			}
			if run.LogPath == "" {
				return errors.New("run has no log path")
			}
			lines, err := logs.TailLines(run.LogPath, 200)
			if err != nil {
				return err
			}
			_, _ = io.WriteString(cmd.OutOrStdout(), strings.Join(lines, "\n")+"\n")
			return nil
		},
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "tui",
		Short: "Interactive dashboard (attach to local state.db)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := workspaceArg()
			if err != nil {
				return err
			}
			return tui.Run(ws)
		},
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:    "daemon-loop",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := workspaceArg()
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			return app.RunDaemonLoop(ctx, ws)
		},
	})

	return rootCmd
}

func printRunResult(w io.Writer, run *store.Run) {
	fmt.Fprintf(w, "run_id: %s\n", run.ID)
	if run.Skipped {
		fmt.Fprintf(w, "skipped: true (%s)\n", run.SkipReason)
		return
	}
	exit := -1
	if run.ExitCode != nil {
		exit = *run.ExitCode
	}
	fmt.Fprintf(w, "exit_code: %d\n", exit)
	fmt.Fprintf(w, "timed_out: %v\n", run.TimedOut)
	fmt.Fprintf(w, "duration: %s\n", run.Duration.Round(time.Millisecond))
	if run.LogPath != "" {
		fmt.Fprintf(w, "log: %s\n", run.LogPath)
	}
	if run.SummaryPath != "" {
		fmt.Fprintf(w, "summary: %s\n", run.SummaryPath)
	}
}

func createdOrExists(created bool) string {
	if created {
		return "created"
	}
	return "exists"
}

func updatedOrUnchanged(updated bool) string {
	if updated {
		return "updated"
	}
	return "unchanged"
}

