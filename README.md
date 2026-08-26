# AIPMC — Project Memory for Your AI Agent Team

AIPMC is a middleware layer for AI coding agents. It doesn't just forward requests — it **observes, understands, and remembers** what your agents actually do, so Claude Code and Codex CLI work from the same project memory, and you see real progress instead of three chat histories.

> Not memory for your agent — **memory for your project.**

## The 30-second story

You run three agents on one repo: Claude refactors the auth module, Codex fixes the hook pipeline, Gemini updates the docs. None of them has any idea what the others did.

**❌ Without AIPMC**

Every agent starts from zero every day. Claude re-breaks the code Codex just fixed. Orphaned commits pile up. "What's the actual status?" means reading three chat histories by hand.

**✅ With AIPMC**

The next session automatically opens with:

```
Yesterday: Claude refactored auth, Codex fixed a hook bug (verify it),
there's 1 unlinked commit and 1 blocked task.
```

You watch all of it on a dashboard — no agent behavior change required.

## Why not "just another memory tool"?

mem0, Zep, and Letta store **facts** for one assistant to recall. AIPMC records **work** — tasks, commits, bugs, decisions — across a team of agents, with accountability:

| | mem0 / Zep / Letta | AIPMC |
|---|---|---|
| What it stores | facts, preferences, conversation summaries | work entities: task / commit / bug / decision with lifecycle states |
| How it captures | explicit API calls / RAG pipelines | **passive** hooks + proxy interception, zero agent changes |
| Scope | one assistant's memory | shared memory across your agents |
| Accountability | none | commit↔session↔task links, scope-drift detection, done-gate, orphan alerts |
| Injection | the agent has to ask | forced at the traffic layer, budgeted, content-hash deduplicated |
| Self-measurement | none | `aipmc metrics` checks itself against documented targets |

The moat is the combination: **passive capture + traffic-layer injection + an accountability chain** — no single memory tool or PM tool gives you all three.

## What it solves

1. **Agents don't know what each other did** — each agent only sees its own chat history
2. **Knowledge gets lost in the process** — bug investigations, architecture decisions, and intent are scattered across conversations
3. **There's no project memory** — every agent starts from zero, unsure where things stand or what's still broken

## How it works

| Layer | Mechanism | What it does |
|------|-----------|--------------|
| **L0 Capture** | Hooks | tool calls, file operations, and messages from your agents land in SQLite automatically, zero config |
| **L1–L3 Analysis** | Pipeline | B1 rule scans (blind-edit detection, compliance) → L2 AI semantic summaries → L3 commit↔session linking |
| **Live Injection** | INJECT | each agent request gets "what changed, what to watch" injected at the traffic layer — deduplicated, budgeted |
| **Closed Loop** | MCP + metrics | agents read/write tasks, decisions, and bugs through 40+ MCP tools; `aipmc metrics` measures whether it works |

## Architecture

```
Agent (Claude Code / Codex CLI, battle-tested)
    │                                        │
    │  API requests (:19530)                 │  Hooks (tool-call events)
    ▼                                        ▼
┌────────────────┐                   ┌──────────────┐
│     Proxy      │                   │ Discussion   │
│ translate/     │                   │ Log (SQLite) │
│ capture        │                   └──────┬───────┘
│ INJECT context │                          │
└───────┬────────┘                          │
        │                                   ▼
        │                          ┌──────────────┐
        │                          │  Pipeline    │
        │                          │  B1→L2→L3    │
        │                          │  30-min loop  │
        │                          └──────┬───────┘
        │                                 │
        │                           INJECT ▼
        │                   ┌────────────────────┐
        │                   │ next request gets   │
        │                   │ context injected    │
        │                   └────────────────────┘
        ▼
┌────────────────────────────┐
│ Web UI (:8720) + REST API  │  activity graph, task board, discussions/
│ + background Pipeline      │  threads, agents board, chat, audit, config
└────────────────────────────┘
```

- **Proxy (:19530)**: protocol translation (Anthropic ↔ OpenAI ↔ Gemini) + native Codex `/v1/responses` passthrough, traffic capture (`/__proxy/capture`), INJECT context injection, discussion-read dedup (protects prefix cache)
- **Hooks**: automatic capture for Claude Code and Codex CLI + post-commit git hook, failures visible in logs
- **Pipeline**: B1 rule scans → L2 AI semantic summaries → L3 commit↔session linking, auto-runs every 30 minutes
- **INJECT**: content-hash dedup, budgeted truncation, warnings-first, fully observable (`[INJECT]` log lines)
- **PM DB**: per-project `.pmai/` SQLite (pure Go, no CGO), zero config

## Features

### Protocol gateway & credentials

- Three-protocol translation (Anthropic, OpenAI-compatible, Gemini) plus native Codex Responses passthrough
- Virtual model registry `~/.aipmc/models.json`: one model ID routes to any provider
- Encrypted credentials: SM4-GCM encrypted API keys, `0600` permissions, multi-profile (`aipmc key`)
- In-proxy command `&aipmc-model`: switch models mid-conversation; `sessions` subcommand shows the live agents board

### Capture & collaboration

- One-line setup per platform: `aipmc setup <platform>` (hooks + MCP)
- `aipm_list_sessions` / `aipm_update_status`: cross-project agents board — see what every agent is doing right now
- All tool calls, file operations, and messages are persisted (rune-safe truncation, cross-message dedup)

### Knowledge & search

- **Discussion log**: all agent conversations in one place; `aipm_read_discussions` supports cursor paging, session filters, and time windows
- **Entity model**: Roadmap → Plan → Task → Commit hierarchy + Bug / Decision / Idea / Principle / Docs — no orphans, no back-fill
- **Search**: FTS5 full-text + Chinese 2-gram recall + AI semantic rerank (`aipm_smart_search`), results with hit snippets
- **Threads**: aggregate related work across plans; `aipm_daily_review` analyzes daily commit associations

### Web dashboard (React 18 + Ant Design 5, embedded in the binary)

- Project: Activity graph (entities/files/sessions), Governance (decisions + principles), Plans & Tasks, Commits, Threads, Bugs
- Knowledge: Discussions, Inbox (idea funnel), Docs, Daily, Code
- Collab: Chat (talk to agents directly), Agents board, Audit log
- Config: Proxy panel (start/stop, model switching, traffic capture), Settings

### Observability & evaluation

- Unified log `~/.aipmc/logs/aipmc.log`: BOOT version anchor (git sha), project tags, 20MB auto-archive (keeps 7), invalid-UTF8 sanitization
- `aipmc metrics`: read-only evaluation against `docs/EVALUATION.md` targets (coverage, injection rate, event processing rate)
- MCP tool usage logs and event processing tracking (`aipm_mark_consumed` / `aipm_mark_event_processed`)

## Quick start

```bash
# build (frontend + Go binary; use -f to skip the frontend)
./build.sh

# one command starts everything: web UI + embedded proxy + background pipeline
./aipmc serve

# open http://127.0.0.1:8720
```

First run registers the current directory as a project and initializes `.pmai/`. Duplicate instances for the same project are refused to avoid concurrent log writes.

New here? Read **[docs/QUICKSTART.md](docs/QUICKSTART.md)** — a command-by-command onboarding guide.

## Agent setup

```bash
# configure hooks + MCP for an agent platform (one line)
aipmc setup claude    # Claude Code
aipmc setup codex     # Codex CLI
aipmc setup gemini    # Gemini CLI
aipmc setup cursor    # Cursor
aipmc setup opencode  # OpenCode

# launch an agent with project config loaded (proxy must be running)
aipmc agent claude
aipmc agent codex
```

> **Platform support (honest tiering)**: Claude Code and Codex CLI are the battle-tested paths — used daily on this project itself, with hook edge cases fixed against real traffic. Gemini CLI, Cursor, OpenCode, Windsurf, Cline, and Roo Code have one-line setup (config + hooks), but are not actively exercised; expect rough edges.

## MCP tools (40+, callable by agents mid-conversation)

### Briefing & context

| Tool | Purpose |
|------|---------|
| `aipm_get_briefing` | project briefing: active tasks, risks, recent activity, threads |
| `aipm_analyze` | full health analysis (scope drift, orphan tasks, duplicate plans, blockers) |
| `aipm_search_context` / `aipm_smart_search` | keyword / AI semantic search across all PM entities |
| `aipm_read_discussions` | read other agents' conversations (cursor paging, time windows) |
| `aipm_search_discussions` | keyword search over discussions (Chinese 2-gram) |

### Recording & writing

| Tool | Purpose |
|------|---------|
| `aipm_create_task` / `aipm_update_task` / `aipm_update_task_status` | task lifecycle (done goes through a gate check) |
| `aipm_record_commit` / `aipm_record_commits` | record commits + scope-drift detection |
| `aipm_record_decision` / `aipm_record_bug` | capture architecture decisions and bugs (root cause + fix) |
| `aipm_link_entities` / `aipm_append_task_note` | entity links and task notes |

### Collaboration & self-check

| Tool | Purpose |
|------|---------|
| `aipm_list_sessions` / `aipm_update_status` | agents board: who is doing what |
| `aipm_create_thread` / `aipm_add_to_thread` / `aipm_daily_review` | cross-plan threads and daily review |
| `aipmc_vision` | screenshot-based UI self-check (edit → screenshot → verify loop) |
| `aipm_submit_feedback` | tool feedback (bug / suggestion) |

Full tool list: `aipmc help` and the MCP server (`aipmc mcp`).

## CLI

```bash
aipmc init                     # initialize project + install post-commit hook
aipmc setup <platform>          # configure an agent platform (no args lists options)
aipmc serve                    # web UI + proxy + pipeline in one process
aipmc proxy [--profile <name>] # run the protocol proxy alone
aipmc chat                     # talk to an agent from the terminal
aipmc metrics [--since all]    # read-only evaluation metrics
aipmc key init/set/list/show   # credential management (SM4-GCM, multi-profile)
aipmc models                   # model registry management
aipmc task|commit|plan|bug|decision|idea|roadmap|principle [CRUD]
aipmc search|doctor|info|daily|session|thread|link|canon|event
```

## Configuration

The Settings / Proxy pages in the web UI manage everything. Core files:

| File | Content |
|------|---------|
| `~/.aipmc/models.json` | LLM gateway: provider registry + virtual model definitions |
| `~/.aipmc/credentials` | SM4-GCM encrypted API keys (`0600`, multi-profile) |
| `~/.aipmc/config.json` | global: proxy port/bind, upstream, Anthropic URL |
| `.pmai/config.json` | project-level: AI model, agent overrides |
| `~/.aipmc/logs/aipmc.log` | unified shared log (20MB archive) |

## Development

### Tech stack

| Layer | Tech |
|-------|------|
| Backend | Go 1.25 (`modernc.org/sqlite`, pure Go, no CGO) |
| Frontend | React 18 + Vite 5 + Ant Design 5 (embedded via `go:embed frontend/dist`) |
| Encryption | GmSSL (SM4-GCM, optional CGO; credentials degrade without it) |
| CI | GitHub Actions: frontend build + `go vet` + `go test` + build |

### Commands

```bash
./build.sh                  # full build (frontend + binary → dist/)
./build.sh -f               # skip frontend build
go test ./...               # backend tests
cd frontend && npm run dev  # frontend hot-reload development
```

### Directory layout

```text
proxy/     protocol translation, INJECT, traffic capture, model routing
hook/      Claude/Codex/Gemini/Cursor/OpenCode hooks + post-commit
session/   session summaries, auto pipeline, agents board, git sync
store/     SQLite CRUD, discussions, audit, daily
search/    FTS5 + Chinese 2-gram + AI rerank
mcp/       MCP server (40+ tools)
api/       REST API (web UI data)
web/       HTTP server
agent/     agent chat service
db/        schema, config, credentials
frontend/  React SPA
docs/      design docs and evaluation targets (EVALUATION.md, ...)
```

## License

MIT
