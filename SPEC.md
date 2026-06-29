# Agent Runner — Specification

> Local scheduler + process supervisor + TUI for unattended AI agent runs.  
> **The runner does not know what the agent does.** It only fires on a schedule, runs a configured command with a prompt file, and records what happened.

---

## 1. Design principles

| Principle | Meaning |
|-----------|---------|
| **Agent-agnostic** | Runner never encodes coding, email, or sub-agent logic. That lives entirely in the prompt file (`bootstrap.md` or any path you choose). |
| **Prompt is the contract** | One file (or stdin) tells the agent what to read, how to behave, when to stop, and how to hand off. Runner passes it through unchanged. |
| **Scheduler is the product** | Core value: reliable local cron, overlap prevention, logs, morning review. Not orchestration, not SQS, not cloud. |
| **Workspace-local config** | `cd` into any folder, `runner init`, set interval, walk away. Multiple workspaces can each have their own schedule. |
| **Observable by default** | Every run gets a timestamped log, exit code, duration, and optional summary file the agent writes itself. |

---

## 2. Scope

### In scope

- CLI + TUI (Go, single binary)
- Per-workspace YAML config
- Interval scheduling (`5m`, `1h`, `6h`, cron expression)
- Background daemon (`start` / `stop` / `status`)
- Spawn configured agent command with prompt file
- PID lock (no overlapping runs for same job)
- SQLite run history + log files on disk
- TUI: list runs, tail logs, pause/resume schedule, trigger now
- Pluggable **agent backends** via config (shell command template only)

### Out of scope (v1)

- Cloud workers, SQS, Redis, distributed queue
- Runner-managed sub-agents (agent handles that via prompt)
- Built-in git worktree logic (agent or prompt can request it; runner may optionally support `cwd` override)
- Web UI
- Multi-machine sync
- Secret management beyond env vars + `.env` path in config

---

## 3. Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  runner (Go binary)                                         │
│                                                             │
│  ┌─────────┐   ┌──────────────┐   ┌─────────────────────┐  │
│  │  CLI    │   │  Scheduler   │   │  TUI (bubbletea)    │  │
│  │ cobra   │──▶│ ticker/cron  │──▶│ runs · logs · ctrl  │  │
│  └─────────┘   └──────┬───────┘   └─────────────────────┘  │
│                       │                                     │
│                       ▼                                     │
│              ┌─────────────────┐                            │
│              │  Job executor   │                            │
│              │  lock · timeout │                            │
│              │  exec · capture │                            │
│              └────────┬────────┘                            │
└───────────────────────┼─────────────────────────────────────┘
                        │
                        ▼
              ┌─────────────────────┐
              │  Agent backend      │  ← configured shell command
              │  (cursor / claude / │
              │   custom script)    │
              └─────────┬───────────┘
                        │
                        ▼
              ┌─────────────────────┐
              │  bootstrap.md       │  ← agent reads this; runner does not parse it
              │  (+ optional files  │
              │   agent loads)      │
              └─────────────────────┘
```

**Data flow per tick:**

1. Scheduler wakes → load workspace config
2. Acquire lock → skip if previous run still alive
3. Read `prompt_file` bytes (do not interpret)
4. `exec` agent command with substituted env vars
5. Stream stdout/stderr to `logs/<run-id>.log`
6. On exit: record row in SQLite, release lock
7. Optional: read `summary_file` if agent wrote it (runner displays in TUI; does not validate content)

---

## 4. Workspace layout

Any directory can be a workspace. After `runner init`:

```
my-workspace/
├── runner.yaml          # scheduler + agent command config (required)
├── bootstrap.md         # prompt passed to agent (name configurable)
├── .runner/
│   ├── runner.pid       # daemon PID (if background mode)
│   ├── runner.lock      # active job lock
│   ├── state.db         # SQLite run history
│   └── logs/
│       └── 2026-06-29T06-00-00Z.log
└── …                    # whatever the agent touches (code, handoff, etc.)
```

Runner **never requires** `.agent/`, `task.md`, or git. Those are conventions for coding workspaces only.

---

## 5. Configuration (`runner.yaml`)

```yaml
# runner.yaml — workspace root

name: exitnine-mobile          # display name in TUI
enabled: true                  # false = daemon loads but never fires

schedule:
  # Exactly one of:
  interval: 1h                 # 30s, 5m, 2h, 1d
  # cron: "0 */5 * * *"        # optional: robfig/cron, local timezone

prompt:
  file: bootstrap.md           # relative to workspace root; required

agent:
  # Shell command template. Runner sets env vars (see §6), does not parse flags.
  command: agent
  args:
    - --print
    - --trust
    - --force
    - --workspace
    - "{{workspace}}"
  # Prompt delivery — exactly one:
  prompt_via: stdin             # cat prompt_file → stdin (default)
  # prompt_via: arg             # append prompt file contents as final arg
  # prompt_via: env             # RUNNER_PROMPT env var (warn if huge)

execution:
  cwd: .                        # working directory for subprocess
  timeout: 45m                  # kill after; mark run timed_out
  env_file: .env                # optional; loaded before run (not committed by runner)
  extra_env:
  #   FOO: bar

output:
  logs_dir: .runner/logs
  summary_file: .runner/last-summary.md   # optional; agent writes, runner displays

safety:
  max_concurrent: 1             # per workspace; v1 hardcoded to 1
  skip_if_locked: true          # if true, skip tick; if false, queue (v2)
```

### Example: coding workspace (Cursor)

```yaml
name: exitnine-mobile
schedule:
  interval: 1h
prompt:
  file: .agent/bootstrap.md
agent:
  command: agent
  args: ["--print", "--trust", "--force", "--workspace", "{{workspace}}"]
  prompt_via: stdin
execution:
  cwd: .
  timeout: 45m
output:
  summary_file: .agent/handoff.md
```

### Example: business email check (Claude Code)

```yaml
name: inbox-digest
schedule:
  interval: 5h
prompt:
  file: bootstrap.md
agent:
  command: claude
  args: ["-p", "--dangerously-skip-permissions"]
  prompt_via: arg
execution:
  cwd: .
  timeout: 20m
  extra_env:
    INBOX_ACCOUNT: alerts@mycompany.com
output:
  summary_file: .runner/last-summary.md
```

### Example: custom wrapper script

```yaml
name: nightly-report
schedule:
  cron: "0 6 * * *"
prompt:
  file: prompts/nightly.md
agent:
  command: ./scripts/run-my-agent.sh
  args: ["{{workspace}}", "{{prompt_file}}"]
  prompt_via: env
```

---

## 6. Environment variables (runner → agent)

Runner injects these into every subprocess. Agent/prompt may reference them; runner does not read them back.

| Variable | Description |
|----------|-------------|
| `RUNNER_WORKSPACE` | Absolute path to workspace root |
| `RUNNER_PROMPT_FILE` | Absolute path to prompt file |
| `RUNNER_PROMPT` | Full prompt text (only if `prompt_via: env`) |
| `RUNNER_RUN_ID` | UUID for this run |
| `RUNNER_STARTED_AT` | RFC3339 timestamp |
| `RUNNER_JOB_NAME` | `name` from config |

Template placeholders in `args` (expanded by runner before exec):

| Placeholder | Value |
|-------------|-------|
| `{{workspace}}` | Absolute workspace path |
| `{{prompt_file}}` | Absolute prompt file path |
| `{{run_id}}` | Current run UUID |

---

## 7. CLI interface

Binary name: `runner` (or `agentr` — pick one at implementation time).

```
runner init              Create runner.yaml + .runner/ skeleton in cwd
runner validate          Parse config, check agent binary exists, dry-run command
runner run               Run once now (foreground), ignore schedule
runner start             Start background scheduler daemon
runner stop              Stop daemon (SIGTERM, wait for active job optional)
runner status            Daemon up/down, next tick, last run summary
runner tui               Interactive dashboard (attach to local state.db)
runner logs [run-id]     Tail log file (non-TUI convenience)
```

### `runner init`

- Writes default `runner.yaml` if missing
- Creates `.runner/logs/`
- Does **not** create `bootstrap.md` (user/agent owns content)
- Prints suggested next steps

### `runner start` (daemon)

1. Load `runner.yaml` from cwd (or `--workspace` flag)
2. Write PID to `.runner/runner.pid`
3. Double-fork or equivalent so shell returns immediately
4. Start scheduler loop in background process
5. On SIGTERM: stop accepting new ticks; optional `graceful_shutdown: wait|kill` for active job

### `runner tui`

Screens:

1. **Home** — job name, schedule, enabled/paused, next run, last run status
2. **Runs** — table: time, duration, exit, timed out?, log size
3. **Run detail** — scrollable log, link to summary file if present
4. **Actions** — `r` run now, `p` pause/resume, `s` stop daemon, `q` quit TUI (daemon keeps running)

Keys: vim-style `j/k`, `/` filter, `Enter` drill down.

---

## 8. Scheduler behavior

### Interval mode

- First tick: optional `run_on_start: true` (default false) — fire immediately when daemon starts
- Subsequent: `time.Ticker` at configured interval
- **No catch-up storm**: if machine slept 8h with `interval: 1h`, run **once** on wake, not 8 times

### Cron mode

- Use `robfig/cron/v3` with local timezone
- Missed triggers while asleep: configurable `missed: skip|run_once` (default `run_once`)

### Skip conditions (before spawn)

| Condition | Action |
|-----------|--------|
| `enabled: false` | Skip silently |
| Lock held (PID alive) | Skip, log `skipped: locked` |
| Previous run within `min_gap` (optional) | Skip |
| Manual pause file `.runner/paused` | Skip |

Runner does **not** skip based on task content, git state, or handoff — unless the **prompt** instructs the agent to exit early and the agent does so.

---

## 9. Job execution

```go
type Run struct {
    ID          string
    Workspace   string
    JobName     string
    StartedAt   time.Time
    FinishedAt  *time.Time
    Duration    time.Duration
    ExitCode    *int
    TimedOut    bool
    Skipped     bool
    SkipReason  string
    LogPath     string
    SummaryPath string // copied path at end of run, may be empty
}
```

### Lock file (`.runner/runner.lock`)

```json
{
  "pid": 12345,
  "run_id": "uuid",
  "started_at": "2026-06-29T06:00:00Z"
}
```

- Acquire before spawn; release in defer
- Stale lock: if PID dead, overwrite
- Timeout: send SIGTERM, wait `kill_grace: 30s`, then SIGKILL; mark `timed_out: true`

### Logging

- Tee stdout/stderr to `.runner/logs/<run-id>.log`
- Also store path in SQLite
- Rotate: optional `max_log_files: 100` — delete oldest

### Summary

- After successful exit, if `output.summary_file` exists, store path on run row
- Runner does not parse markdown; TUI shows file contents or mtime

---

## 10. Persistence (SQLite)

Database: `.runner/state.db`

```sql
CREATE TABLE runs (
    id            TEXT PRIMARY KEY,
    job_name      TEXT NOT NULL,
    started_at    TEXT NOT NULL,
    finished_at   TEXT,
    duration_ms   INTEGER,
    exit_code     INTEGER,
    timed_out     INTEGER NOT NULL DEFAULT 0,
    skipped       INTEGER NOT NULL DEFAULT 0,
    skip_reason   TEXT,
    log_path      TEXT,
    summary_path  TEXT
);

CREATE TABLE meta (
    key   TEXT PRIMARY KEY,
    value TEXT
);
-- meta: last_tick, paused, daemon_started_at, config_hash
```

Global state path: per-workspace only (no central DB in v1). TUI opened in a workspace reads that workspace's DB.

---

## 11. Agent backends (reference)

Runner treats these identically — only config differs.

### Cursor Agent CLI

```yaml
agent:
  command: agent
  args: ["--print", "--trust", "--force", "--workspace", "{{workspace}}"]
  prompt_via: stdin
```

### Claude Code

```yaml
agent:
  command: claude
  args: ["-p"]
  prompt_via: stdin
```

### Any script

```yaml
agent:
  command: /usr/local/bin/my-agent
  args: ["--prompt-file", "{{prompt_file}}"]
  prompt_via: none   # script reads file itself
```

---

## 12. Prompt file responsibilities (not runner)

Document for prompt authors. Runner does not enforce any of this.

The prompt file (`bootstrap.md`, etc.) should define:

- What to read on startup (handoff, inbox API, etc.)
- Sub-agent delegation pattern (if any)
- Single-run scope ("do one task then stop")
- Where to write handoff / summary (`output.summary_file` path)
- Exit conditions (blocked, success, partial)
- Whether to commit, send email, etc.

**Coding example** (convention, not runner requirement):

```markdown
You are an automation agent. Unattended run.

1. Read `.agent/handoff.md` and `.cursor/task.md`
2. Complete exactly ONE task
3. Update handoff.md and regenerate bootstrap.md for the next run
4. Run tests for touched modules
5. Do not commit unless TASK allows it
6. Write a 5-line summary to `.runner/last-summary.md`
```

**Email example:**

```markdown
Check business inbox for account INBOX_ACCOUNT (env).
Summarize actionable items only.
Send digest to alerts@mycompany.com via sendmail or API.
Write subject lines and counts to `.runner/last-summary.md`.
If nothing actionable, exit 0 with summary "no action needed".
```

---

## 13. Security

| Risk | Mitigation |
|------|------------|
| `--force` / skip-permissions agents | Document clearly; user opts in per workspace config |
| `.env` secrets | Runner loads `env_file` into subprocess only; never log env |
| Arbitrary `agent.command` | User owns their `runner.yaml`; `runner validate` warns on suspicious paths |
| Lock bypass | Manual delete of lock file documented for recovery |
| Log leakage | Logs may contain agent output; keep `.runner/` in `.gitignore` template |

`runner init` should append to `.gitignore`:

```
.runner/logs/
.runner/*.pid
.runner/runner.lock
.runner/state.db
```

---

## 14. Platform notes

### macOS

- Daemon survives terminal close; does **not** survive sleep unless machine wakes
- Document: use `caffeinate -dims runner start` for overnight on lid-closed Mac, or accept missed ticks
- Optional v2: integrate with `launchd` via `runner install-launchd` generating a plist that calls `runner run` (single shot per interval — simpler than long-lived daemon)

### Linux

- Same daemon model; systemd user unit optional v2

---

## 15. Project structure (Go)

```
runner/
├── SPEC.md                 # this file
├── go.mod
├── cmd/
│   └── runner/
│       └── main.go
├── internal/
│   ├── config/             # load + validate runner.yaml
│   ├── scheduler/          # interval + cron + sleep catch-up
│   ├── executor/           # lock, exec, timeout, logging
│   ├── daemon/             # start/stop/pid
│   ├── store/              # sqlite runs + meta
│   └── tui/                # bubbletea models
└── scripts/
    └── example-runner.yaml
```

### Dependencies (suggested)

| Package | Use |
|---------|-----|
| `github.com/spf13/cobra` | CLI |
| `github.com/charmbracelet/bubbletea` | TUI |
| `github.com/charmbracelet/lipgloss` | TUI styling |
| `gopkg.in/yaml.v3` | Config |
| `github.com/robfig/cron/v3` | Cron schedules |
| `modernc.org/sqlite` | Pure Go SQLite |

---

## 16. MVP milestones

### M1 — Run once (1–2 days)

- [ ] `config` load/validate
- [ ] `runner run` — spawn agent, log output, print exit code
- [ ] Env substitution + stdin prompt delivery

### M2 — Scheduler (1–2 days)

- [ ] `runner start` / `stop` / `status`
- [ ] Interval scheduling + lock + timeout
- [ ] SQLite run history

### M3 — TUI (1–2 days)

- [ ] Run list + log viewer
- [ ] Pause/resume, run now

### M4 — Polish

- [ ] Cron mode
- [ ] `runner init` + `.gitignore`
- [ ] `runner validate`
- [ ] Sleep catch-up policy
- [ ] `summary_file` display in TUI

---

## 17. Acceptance criteria (MVP done)

1. `cd any-folder && runner init && runner start` — daemon runs in background
2. Agent fires on schedule with prompt file contents; runner does not parse prompt
3. Overlapping ticks skip cleanly when previous job still running
4. `runner tui` shows last 24h of runs and full logs
5. `runner stop` stops daemon; active job behavior documented
6. Two workspaces with different intervals and different agent commands work independently
7. Works with Cursor `agent` and Claude `claude` backends via yaml only — no code changes

---

## 18. Non-goals reminder

The runner is **not** an AI framework. It does not:

- Parse `task.md`, handoff, or CAP references
- Choose which task to run next
- Spawn sub-agents
- Send email or commit code
- Replace Cursor IDE or Claude Code UI

All intelligence lives in the agent + prompt file. The runner is the alarm clock, tape recorder, and control panel.
