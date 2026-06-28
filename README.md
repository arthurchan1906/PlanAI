# PlanAI — AI 项目管理器

PlanAI 是一个 AI 编程代理（Agent）的统一中间层。它提供协议代理、项目管理数据捕获和 Web 管理界面，让你可以无缝使用 **Claude Code**、**Codex CLI** 和 **Gemini CLI**，并统一管理所有 Agent 产生的任务、提交、决策和讨论记录。

## 架构

```
Agent (Claude/Codex/Gemini) → Proxy (:19530) → 上游 LLM (DeepSeek/OpenAI/Ollama)
                                    │
                              Web UI (:8720)
                              项目管理 / 流量查看 / Agent 启动
```

- **Proxy**: 协议翻译（Anthropic ↔ OpenAI ↔ Gemini）、流量捕获、Anthropic 透传
- **Web UI**: 任务看板、提交历史、Agent 启动器、配置管理
- **Hooks**: Agent 工具调用时自动捕获 PM 数据
- **PM DB**: 每项目独立 SQLite，零配置

## 快速开始

```bash
# 编译
go build -o aipmc .

# 一条命令启动（proxy + web）
./aipmc serve

# 浏览器自动打开 http://127.0.0.1:8720
```

首次启动时，在任意目录运行 `aipmc serve`：
- 如果当前目录已注册，直接使用
- 如果已有其他注册项目，会显示选择器
- 按 Enter 注册当前目录为新项目
- 新项目自动初始化 `.pmai/`，无需手动 `aipmc init`

## 命令参考

| 命令 | 说明 |
|------|------|
| `aipmc serve` | 启动 proxy + web（自动选择项目，默认 :8720） |
| `aipmc serve --project <路径>` | 指定项目启动 |
| `aipmc serve --no-browser` | 不自动打开浏览器 |
| `aipmc proxy` | 仅启动代理 (:19530) |
| `aipmc web` | 仅启动 Web (:8720) |
| `aipmc agent <claude\|codex\|gemini>` | 启动预配置的 Agent |
| `aipmc init` | 在当前目录初始化 `.pmai/` |
| `aipmc setup <claude\|codex\|gemini\|cursor\|opencode>` | 为指定 Agent 配置 hooks |

## 配置

所有配置通过 Web UI（Settings 页面）管理：

| 配置项 | 作用域 | 存储位置 |
|--------|--------|----------|
| 上游 API 端点 | 全局 | `~/.aipmc/config.json` |
| Anthropic 端点 | 全局 | `~/.aipmc/config.json` |
| 代理模型覆写 | 全局 | `~/.aipmc/config.json` |
| API Key (`UPSTREAM_KEY`) | 环境变量 | — |
| AI 端点 (PM 用) | 每项目 | `.pmai/config.json` |
| Web 端口 | 每项目 | `.pmai/config.json` |


## 项目结构

```
PlanAI/
├── main.go           # CLI 入口
├── proxy/            # 代理核心（协议适配、翻译、发射器、流量捕获）
├── api/              # Web API（配置、Agent 启动器）
├── web/              # Web 服务器（静态文件、反向代理）
├── db/               # 数据库和配置层
├── app/              # 应用运行时
├── hook/             # Agent Hook 集成
├── frontend/         # React 前端 (Ant Design)
└── paths/            # 跨平台路径工具
```

## License

MIT
