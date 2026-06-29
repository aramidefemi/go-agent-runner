# Go Agent Runner

Local scheduler and process supervisor CLI for unattended AI agent runs.

The runner does not know what the agent does. It fires on a schedule, runs a configured shell command with a prompt file, and records what happened.

## What it does

- **Agent-agnostic** — runs any shell command you configure (Cursor `agent`, Claude Code, custom scripts)
- **Workspace-local** — `cd` into any folder, `runner init`, set an interval, walk away
- **Interval or cron scheduling** — background daemon with `start` / `stop` / `status`
- **One-shot runs** — `runner run` executes immediately in the foreground
- **Overlap prevention** — PID lock skips ticks when a previous run is still alive
- **Observable by default** — timestamped logs, exit codes, duration, SQLite run history
- **Prompt is the contract** — runner passes `bootstrap.md` (or any path) through unchanged; the agent owns all intelligence

## What it doesn't do

- Parse task files, handoffs, or prompt content
- Choose which task to run next or spawn sub-agents
- Send email, commit code, or orchestrate cloud workers
- Replace Cursor IDE, Claude Code UI, or any agent framework
- Provide a web UI or multi-machine sync (v1)

All intelligence lives in the agent + prompt file. The runner is the alarm clock, tape recorder, and control panel.

## How it compares

Local scheduled AI agent runners are an active indie space — several strong solo and small-team projects tackle the same problem from different angles. None of these are backed by big platforms; they're built by developers solving the same "run my agent while I sleep" itch. They're all worth a look depending on what you need.

| | **Go Agent Runner** | [Junior](https://github.com/JHostalek/junior) | [cronai](https://github.com/islo-labs/cronai) | [kage](https://github.com/igtm/kage) | [crnd](https://github.com/ysm-dev/crnd) | [schedx](https://github.com/Alireza29675/schedx) |
|---|---|---|---|---|---|---|
| **Runtime** | Single Go binary | Go binary | Node.js | Shell + OS scheduler | Bun | Rust binary |
| **Agent support** | Any shell command | Claude Code | Claude Code | Many CLIs | Any command | Shell + `--prompt` |
| **Config model** | Per-workspace `runner.yaml` | Repo + natural-language schedules | Project `cronai.yml` | Markdown tasks in repo | TOML job definitions | `schedx.yaml` + `~/.schedx/` |
| **Background mode** | Own daemon (`start`/`stop`) | Persistent daemon | Persistent daemon | OS cron / launchd | Persistent daemon | File-based (no daemon) |
| **Prompt handling** | Passes file through unchanged | Agent-driven task queue | Inline YAML prompts | Markdown task body | Command args | First-class `--prompt` |
| **Git worktrees** | No (agent decides) | Built-in isolation + merge-back | No | No | No | No |
| **Integrations** | None (by design) | MCP, Slack, Linear, etc. | GitHub, Linear, Slack | None | Agent skill system | Agent skill system |
| **TUI dashboard** | Yes | Yes | Yes | CLI only | CLI + JSON | CLI + JSON |
| **Scope** | Scheduler + supervisor only | Full autonomous dev platform | Cron manager for Claude | Ultra-light OS-native layer | Agent-friendly cron daemon | General local scheduler |

### What makes this project different

Go Agent Runner is not trying to be the most capable tool in the table. It's trying to be the **smallest correct tool** for one job: fire an agent on a schedule and record what happened.

1. **The runner is dumb on purpose** — It never parses your prompt, picks tasks, manages handoffs, or encodes agent logic. `bootstrap.md` is the entire contract. You can change what the agent does without touching the runner.

2. **Truly agent-agnostic** — Not locked to Claude Code or Cursor. If it runs in a shell, it runs here. Swap `agent` for `claude`, `cursor-agent`, or `./my-script.sh` in `runner.yaml`.

3. **Workspace-local, not global** — Each folder gets its own `runner.yaml`, schedule, and `.runner/` state. Three projects, three schedules, no shared daemon config to manage.

4. **Zero integration surface** — No GitHub OAuth, no Slack webhooks, no MCP wiring in the runner. Integrations live in your agent and prompt, where they belong.

5. **One binary, no runtime** — No Node, Bun, or Docker. Build or install once, use everywhere on your Mac.

**Pick Junior or cronai** if you want a full autonomous coding platform or Claude-native cron management with a TUI today. **Pick kage** if you want zero idle memory via OS schedulers. **Pick Go Agent Runner** if you want the thinnest possible layer between a clock and whatever agent CLI you already use.

## Installation

**From source (recommended during development):**

```bash
git clone https://github.com/aramidefemi/go-agent-runner.git
cd go-agent-runner
go build -o runner ./cmd/runner
sudo mv runner /usr/local/bin/   # optional: install globally
```

**Install to `$GOPATH/bin` or `$GOBIN`:**

```bash
go install github.com/aramidefemi/go-agent-runner/cmd/runner@latest
```

Requires Go 1.24+.

## Quick start

```bash
cd my-project
runner init
```

`runner init` creates `runner.yaml`, `.runner/logs/`, and appends runner artifacts to `.gitignore`. It does **not** create `bootstrap.md` — you write that yourself.

```bash
# 1. Create your prompt file
vim bootstrap.md

# 2. Verify config and agent binary
runner validate

# 3. Test a single foreground run
runner run

# 4. Start the background scheduler (1h interval by default)
runner start

# 5. Check daemon and last run
runner status

# 6. View logs from the latest run
runner logs

# 7. Stop when done
runner stop
```

Use `--workspace /path/to/project` on any command to target a workspace other than the current directory.

## CLI reference

| Command | Description |
|---------|-------------|
| `runner init` | Create `runner.yaml` and `.runner/` skeleton in cwd |
| `runner validate` | Parse config, check agent binary exists, print dry-run command |
| `runner run` | Run once now in foreground (ignores schedule) |
| `runner start` | Start background scheduler daemon |
| `runner stop` | Stop daemon (SIGTERM) |
| `runner status` | Daemon up/down, next run, last run — opens TUI when terminal is interactive |
| `runner logs [run-id]` | Tail log file (latest run if ID omitted) |
| `runner tui` | Interactive dashboard (same as `runner status` in a TTY) |

**Global flag:** `--workspace <path>` — workspace root (default: current directory)

## Configuration (`runner.yaml`)

Key fields from the default `runner init` template:

```yaml
name: my-workspace          # display name
enabled: true               # false = daemon loads but never fires

schedule:
  interval: 1h              # 30s, 5m, 2h, 1d — or use cron instead
  # cron: "0 */5 * * *"     # robfig/cron, local timezone
  # run_on_start: false     # fire immediately when daemon starts

prompt:
  file: bootstrap.md        # relative to workspace root (or absolute path)

agent:
  command: agent            # shell command to invoke
  args:
    - --print
    - --trust
    - --force
    - --workspace
    - "{{workspace}}"
  prompt_via: stdin         # stdin | arg | env

execution:
  cwd: .
  timeout: 45m
  # env_file: .env
  # extra_env:
  #   FOO: bar

output:
  logs_dir: .runner/logs
  summary_file: .runner/last-summary.md   # optional; agent writes, runner displays

safety:
  max_concurrent: 1
  skip_if_locked: true      # skip tick if previous run still running
```

### Template placeholders in `agent.args`

| Placeholder | Value |
|-------------|-------|
| `{{workspace}}` | Absolute workspace path |
| `{{prompt_file}}` | Absolute prompt file path |
| `{{run_id}}` | Current run UUID |

### Environment variables injected into the agent

| Variable | Description |
|----------|-------------|
| `RUNNER_WORKSPACE` | Absolute path to workspace root |
| `RUNNER_PROMPT_FILE` | Absolute path to prompt file |
| `RUNNER_PROMPT` | Full prompt text (only if `prompt_via: env`) |
| `RUNNER_RUN_ID` | UUID for this run |
| `RUNNER_STARTED_AT` | RFC3339 timestamp |
| `RUNNER_JOB_NAME` | `name` from config |

See [SPEC.md](SPEC.md) for full configuration examples (Cursor, Claude Code, custom scripts).

## Workspace layout

After `runner init`:

```
my-workspace/
├── runner.yaml              # scheduler + agent command config (required)
├── bootstrap.md             # prompt passed to agent (you create this)
├── .runner/
│   ├── runner.pid           # daemon PID (background mode)
│   ├── runner.lock          # active job lock
│   ├── state.db             # SQLite run history
│   └── logs/
│       └── <run-id>.log
└── …                        # whatever the agent touches
```

Runner never requires `.agent/`, `task.md`, or git. Those are optional conventions for coding workspaces.

## License

[MIT Non-Commercial License](LICENSE) — free to use, modify, and distribute for non-commercial purposes. Commercial use is not permitted.
