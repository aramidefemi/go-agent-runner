package tui

import (
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/aramidefemi/go-agent-runner/internal/app"
	"github.com/aramidefemi/go-agent-runner/internal/control"
	"github.com/aramidefemi/go-agent-runner/internal/daemon"
	"github.com/aramidefemi/go-agent-runner/internal/logs"
	"github.com/aramidefemi/go-agent-runner/internal/store"
)

const refreshInterval = 2 * time.Second

// Run starts the interactive dashboard for a workspace.
func Run(workspaceRoot string) error {
	m := newModel(workspaceRoot)
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

type screen int

const (
	screenHome screen = iota
	screenDetail
	screenConfirmStop
)

type model struct {
	workspace   string
	snap        app.Snapshot
	loaded      bool
	screen      screen
	cursor      int
	logOffset   int
	filter      string
	filterMode  bool
	filterInput string
	message     string
	detailLog   []string
	detailSum   string
	width       int
	height      int
	err         error
}

func newModel(workspaceRoot string) model {
	return model{workspace: workspaceRoot, screen: screenHome}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tickAfter(refreshInterval), loadSnapshot(m.workspace))
}

func tickAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return tickMsg{} })
}

func loadSnapshot(workspace string) tea.Cmd {
	return func() tea.Msg {
		snap, err := app.LoadSnapshot(workspace)
		return snapshotMsg{snap: snap, err: err}
	}
}

type tickMsg struct{}
type snapshotMsg struct {
	snap app.Snapshot
	err  error
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case snapshotMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.snap = msg.snap
			m.loaded = true
			m.err = nil
			if m.screen == screenDetail {
				m.loadDetailContent()
			}
		}
	case tickMsg:
		return m, tea.Batch(tickAfter(refreshInterval), loadSnapshot(m.workspace))
	case tea.KeyMsg:
		if m.filterMode {
			return m.handleFilterKey(msg)
		}
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) View() string {
	if m.err != nil {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("error: "+m.err.Error()) + "\n"
	}
	if !m.loaded {
		return "loading...\n"
	}

	var body string
	switch m.screen {
	case screenDetail:
		body = renderDetail(m)
	case screenConfirmStop:
		body = renderConfirmStop(m)
	default:
		body = renderHome(m)
	}

	help := renderHelp(m)
	msg := ""
	if m.message != "" {
		msg = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(m.message) + "\n"
	}
	return body + "\n" + msg + help
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenConfirmStop:
		return m.handleConfirmStopKey(msg)
	case screenDetail:
		return m.handleDetailKey(msg)
	default:
		return m.handleHomeKey(msg)
	}
}

func (m *model) handleHomeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	runs := m.filteredRuns()
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		if m.cursor < len(runs)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "enter":
		if len(runs) > 0 && m.cursor < len(runs) {
			m.screen = screenDetail
			m.logOffset = 0
			m.loadDetailContent()
		}
	case "/":
		m.filterMode = true
		m.filterInput = m.filter
	case "r":
		if !m.snap.DaemonRunning {
			m.message = "daemon not running — run: runner start"
			return m, nil
		}
		if err := control.RequestRunNow(m.workspace); err != nil {
			m.message = err.Error()
		} else {
			m.message = "run now requested"
		}
	case "p":
		paused := !m.snap.Paused
		if err := control.SetPaused(m.workspace, paused); err != nil {
			m.message = err.Error()
		} else if paused {
			m.message = "schedule paused"
		} else {
			m.message = "schedule resumed"
		}
		return m, loadSnapshot(m.workspace)
	case "s":
		m.screen = screenConfirmStop
	case "g":
		m.cursor = 0
	case "G":
		if len(runs) > 0 {
			m.cursor = len(runs) - 1
		}
	}
	return m, nil
}

func (m *model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "enter", "backspace":
		m.screen = screenHome
	case "j", "down":
		m.logOffset++
	case "k", "up":
		if m.logOffset > 0 {
			m.logOffset--
		}
	case "g":
		m.logOffset = 0
	case "G":
		m.logOffset = max(0, len(m.detailLog)-visibleLogLines(*m))
	}
	return m, nil
}

func (m *model) handleConfirmStopKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		if err := daemon.Stop(m.workspace); err != nil {
			m.message = err.Error()
		} else {
			m.message = "daemon stopped"
		}
		m.screen = screenHome
		return m, loadSnapshot(m.workspace)
	case "n", "esc", "q":
		m.screen = screenHome
	}
	return m, nil
}

func (m *model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filterMode = false
	case "enter":
		m.filter = m.filterInput
		m.filterMode = false
		m.cursor = 0
	case "backspace":
		if len(m.filterInput) > 0 {
			m.filterInput = m.filterInput[:len(m.filterInput)-1]
		}
	default:
		if len(msg.Runes) > 0 {
			m.filterInput += string(msg.Runes)
		}
	}
	return m, nil
}

func (m *model) filteredRuns() []store.Run {
	if m.filter == "" {
		return m.snap.Runs
	}
	out := make([]store.Run, 0)
	needle := m.filter
	for _, r := range m.snap.Runs {
		if containsFold(r.ID, needle) || containsFold(r.SkipReason, needle) {
			out = append(out, r)
		}
	}
	return out
}

func (m *model) loadDetailContent() {
	runs := m.filteredRuns()
	if m.cursor < 0 || m.cursor >= len(runs) {
		return
	}
	run := runs[m.cursor]
	if run.LogPath != "" {
		lines, err := logs.TailLines(run.LogPath, 5000)
		if err != nil {
			m.detailLog = []string{fmt.Sprintf("(log read error: %v)", err)}
		} else {
			m.detailLog = lines
		}
	} else {
		m.detailLog = []string{"(no log path)"}
	}
	if run.SummaryPath != "" {
		if b, err := os.ReadFile(run.SummaryPath); err == nil {
			m.detailSum = string(b)
		} else {
			m.detailSum = fmt.Sprintf("(summary read error: %v)", err)
		}
	} else {
		m.detailSum = ""
	}
}

func containsFold(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && searchFold(s, sub))
}

func searchFold(s, sub string) bool {
	// simple case-insensitive contains
	for i := 0; i+len(sub) <= len(s); i++ {
		if equalFold(s[i:i+len(sub)], sub) {
			return true
		}
	}
	return false
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
