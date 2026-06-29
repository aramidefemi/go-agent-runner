package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/aramidefemi/go-agent-runner/internal/app"
	"github.com/aramidefemi/go-agent-runner/internal/logs"
	"github.com/aramidefemi/go-agent-runner/internal/store"
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	okStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	selStyle   = lipgloss.NewStyle().Background(lipgloss.Color("8")).Bold(true)
)

func renderHome(m model) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("runner") + " " + dimStyle.Render(m.workspace) + "\n\n")

	b.WriteString(fmt.Sprintf("job:      %s\n", m.snap.JobName))
	b.WriteString(fmt.Sprintf("schedule: %s\n", m.snap.ScheduleDesc))
	b.WriteString(fmt.Sprintf("enabled:  %s\n", boolLabel(m.snap.Enabled, "yes", "no")))
	b.WriteString(fmt.Sprintf("paused:   %s\n", boolLabel(m.snap.Paused, "yes", "no")))

	daemon := errStyle.Render("down")
	if m.snap.DaemonRunning {
		daemon = okStyle.Render(fmt.Sprintf("up (pid %d)", m.snap.DaemonPID))
	}
	b.WriteString(fmt.Sprintf("daemon:   %s\n", daemon))

	if m.snap.LastTick != nil {
		b.WriteString(fmt.Sprintf("last_tick: %s\n", m.snap.LastTick.UTC().Format(time.RFC3339)))
	}
	b.WriteString(fmt.Sprintf("next_run:  %s\n", formatNextRun(m.snap)))

	if m.snap.ActiveLock != nil {
		b.WriteString(warnStyle.Render(fmt.Sprintf("active:    run %s started %s\n",
			shortID(m.snap.ActiveLock.RunID), m.snap.ActiveLock.StartedAt)))
	}

	if m.snap.LastRun != nil {
		b.WriteString(fmt.Sprintf("last_run:  %s\n", formatRunSummary(*m.snap.LastRun)))
	}

	b.WriteString("\n")
	b.WriteString(titleStyle.Render("runs (24h)") + "\n")
	if m.filterMode {
		b.WriteString(dimStyle.Render("filter: "+m.filterInput+"█") + "\n")
	} else if m.filter != "" {
		b.WriteString(dimStyle.Render("filter: "+m.filter) + "\n")
	}

	runs := m.filteredRuns()
	if len(runs) == 0 {
		b.WriteString(dimStyle.Render("  (no runs)") + "\n")
		return b.String()
	}

	header := fmt.Sprintf("  %-10s %-8s %-6s %-7s %s", "started", "duration", "exit", "flags", "id")
	b.WriteString(dimStyle.Render(header) + "\n")
	for i, run := range runs {
		line := formatRunRow(run)
		if i == m.cursor {
			b.WriteString(selStyle.Render("> "+line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	return b.String()
}

func renderDetail(m model) string {
	runs := m.filteredRuns()
	if m.cursor < 0 || m.cursor >= len(runs) {
		return "no run selected"
	}
	run := runs[m.cursor]

	var b strings.Builder
	b.WriteString(titleStyle.Render("run detail") + " " + dimStyle.Render(shortID(run.ID)) + "\n\n")
	b.WriteString(fmt.Sprintf("started:  %s\n", run.StartedAt.UTC().Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("duration: %s\n", run.Duration.Round(time.Millisecond)))
	b.WriteString(fmt.Sprintf("exit:     %s\n", exitLabel(run)))
	if run.Skipped {
		b.WriteString(fmt.Sprintf("skipped:  %s\n", run.SkipReason))
	}
	if run.LogPath != "" {
		b.WriteString(fmt.Sprintf("log:      %s (%s)\n", run.LogPath, formatBytes(logs.FileSize(run.LogPath))))
	}

	if m.detailSum != "" {
		b.WriteString("\n" + titleStyle.Render("summary") + "\n")
		b.WriteString(m.detailSum + "\n")
	}

	b.WriteString("\n" + titleStyle.Render("log") + "\n")
	visible := visibleLogLines(m)
	end := m.logOffset + visible
	if end > len(m.detailLog) {
		end = len(m.detailLog)
	}
	start := m.logOffset
	if start < 0 {
		start = 0
	}
	if start > len(m.detailLog) {
		start = len(m.detailLog)
	}
	for _, line := range m.detailLog[start:end] {
		b.WriteString(line + "\n")
	}
	if len(m.detailLog) > visible {
		b.WriteString(dimStyle.Render(fmt.Sprintf("(%d/%d lines, j/k scroll)", end, len(m.detailLog))) + "\n")
	}
	return b.String()
}

func renderConfirmStop(m model) string {
	return warnStyle.Render("Stop daemon? [y] yes  [n] cancel") + "\n"
}

func renderHelp(m model) string {
	switch m.screen {
	case screenDetail:
		return dimStyle.Render("j/k scroll log · esc/enter back · q quit")
	case screenConfirmStop:
		return dimStyle.Render("y confirm stop · n/esc cancel")
	default:
		return dimStyle.Render("j/k select · enter detail · r run now · p pause · s stop daemon · / filter · q quit")
	}
}

func formatNextRun(snap app.Snapshot) string {
	if snap.NextRun != nil {
		return snap.NextRun.UTC().Format(time.RFC3339)
	}
	if snap.NextRunNote != "" {
		return snap.NextRunNote
	}
	return "—"
}

func formatRunSummary(run store.Run) string {
	return shortID(run.ID) + " " + exitLabel(run) + " " + run.Duration.Round(time.Millisecond).String()
}

func formatRunRow(run store.Run) string {
	flags := ""
	if run.TimedOut {
		flags += "T"
	}
	if run.Skipped {
		flags += "S"
	}
	if flags == "" {
		flags = "·"
	}
	size := ""
	if run.LogPath != "" {
		size = formatBytes(logs.FileSize(run.LogPath))
	}
	return fmt.Sprintf("%-10s %-8s %-6s %-7s %s %s",
		run.StartedAt.UTC().Format("15:04:05"),
		run.Duration.Round(time.Millisecond).String(),
		exitLabel(run),
		flags,
		shortID(run.ID),
		size,
	)
}

func exitLabel(run store.Run) string {
	if run.Skipped {
		return "skip"
	}
	if run.TimedOut {
		return "timeout"
	}
	if run.ExitCode == nil {
		return "—"
	}
	return fmt.Sprintf("%d", *run.ExitCode)
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func boolLabel(v bool, yes, no string) string {
	if v {
		return yes
	}
	return no
}

func formatBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%dK", n/1024)
	default:
		return fmt.Sprintf("%dM", n/(1024*1024))
	}
}

func visibleLogLines(m model) int {
	h := m.height - 14
	if h < 5 {
		h = 5
	}
	return h
}
