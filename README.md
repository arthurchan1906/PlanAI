# PlanAI — AI Project Manager

PlanAI transforms how you work with AI coding agents. It provides a unified proxy layer that routes **Claude Code**, **Codex CLI**, and **Gemini CLI** through any LLM backend — while automatically capturing project context (tasks, commits, decisions, discussions) via hooks.

## Architecture

```
Agent (Claude/Codex/Gemini) → Proxy (:19530) → Upstream LLM (DeepSeek/OpenAI/Ollama)
                                    │
                              Web UI (:8720)
                              项目管理 / 流量查看 / Agent 启动
```

- **Proxy**: Protocol translation (Anthropic ↔ OpenAI ↔ Gemini), traffic inspection, Anthropic passthrough
- **Web UI**: Project dashboard, task tracking, commit history, agent launcher, settings
- **Hooks**: Automatic PM data capture from agent tool calls
- **PM DB**: SQLite per-project, zero configuration

## Quick Start

```bash
# Build
go build -o aipmc .

# Start (proxy + web, one command)
./aipmc serve

# Or start individually
./aipmc proxy    # proxy only on :19530
./aipmc web      # web UI on :8720

# Open browser
open http://127.0.0.1:8720
```

## Commands

| Command | Description |
|---------|-------------|
| `aipmc serve` | Start proxy + web (auto-select project, port 8720) |
| `aipmc serve --project <path>` | Start with specific project |
| `aipmc serve --no-browser` | Skip auto-opening browser |
| `aipmc proxy` | Start proxy only (:19530) |
| `aipmc web` | Start web only (:8720) |
| `aipmc agent <claude\|codex\|gemini>` | Launch agent pre-configured for proxy |
| `aipmc init` | Initialize `.pmai/` in current directory |
| `aipmc setup <claude\|codex\|gemini\|cursor\|opencode>` | Configure hooks for a code agent |

## Configuration

All settings managed through the Web UI (Settings page):

| Setting | Scope | File |
|---------|-------|------|
| Upstream URL | Global | `~/.aipmc/config.json` |
| Anthropic URL | Global | `~/.aipmc/config.json` |
| Proxy Model | Global | `~/.aipmc/config.json` |
| API Key (`UPSTREAM_KEY`) | Env var | — |
| AI Endpoint | Per-project | `.pmai/config.json` |
| Web Port | Per-project | `.pmai/config.json` |

### Anthropic Passthrough

When `anthropic_url` is configured, Claude Code requests bypass the OpenAI translation layer entirely. The proxy forwards raw Anthropic protocol directly to the upstream, preserving `thinking/signature/tool_use/stop_reason` semantics.

## Project Management

```
$ aipmc serve

当前目录未注册。已注册 2 个项目:

  [1]  5分钟前        D:\projects\english-learning
  [2]  1小时前        D:\projects\PlanAI

输入序号 [1-2]，或 Enter 注册当前目录: _
```

- Run `aipmc serve` from any directory — pick a registered project or register the current one
- Projects auto-register in `~/.aipmc/projects.json`
- Dead paths auto-cleaned on startup
- New projects auto-initialized (no separate `aipmc init` needed)

## Building from Source

```bash
# Requires Go 1.22+
git clone https://github.com/your-org/PlanAI.git
cd PlanAI

# Build frontend (optional — embed available)
cd frontend && npm install && npm run build && cd ..

# Build
go build -o aipmc .

# Frontend is embedded via go:embed
```

## License

MIT
