# AIPMC — 给 AI Agent 团队的共享项目记忆

AIPMC 是面向 AI 编程 Agent 的**项目管理中间层**。它不只是转发请求，而是**观察、理解、记忆** Agent 的工作过程，让 Claude Code 和 Codex CLI 共享同一份"项目记忆"，并把提炼后的结构化进度交给 PM。

> 不是给 Agent 的记忆——是给**项目**的记忆。

## 30 秒故事

同一个仓库里跑着三个 Agent：Claude 重构认证模块，Codex 修 hook 管道，Gemini 更新文档。它们互相完全不知道对方做了什么。

**❌ 没有 AIPMC**

每个 Agent 每天都从零开始。Claude 重新改坏了 Codex 刚修好的代码；孤儿 commit 越堆越多；"现在到底什么进度？"意味着要手动翻三份对话历史。

**✅ 有 AIPMC**

下一次会话自动带着这些信息开始：

```
昨天：Claude 重构了认证模块；Codex 修了个 hook bug（记得验证）；
有 1 个未关联的 commit 和 1 个阻塞任务。
```

这一切都能在网页看板上看到——Agent 不需要改变任何行为。

## 为什么不是"又一个记忆工具"？

mem0、Zep、Letta 存储的是**事实**，供单个助手回忆。AIPMC 记录的是**工作**——任务、commit、bug、决策——跨越整个 Agent 团队，还带着问责链：

| | mem0 / Zep / Letta | AIPMC |
|---|---|---|
| 存什么 | 事实、偏好、对话摘要 | 工作实体：task / commit / bug / decision，带生命周期状态 |
| 怎么捕获 | 显式 API 调用 / RAG 管道 | **被动捕获**：Hook + Proxy 拦截，Agent 零改动 |
| 作用域 | 单个助手的记忆 | 多个 Agent 共享同一份项目记忆 |
| 问责 | 无 | commit↔session↔task 关联、scope drift 检测、done-gate、孤儿告警 |
| 注入方式 | Agent 主动去查 | 流量层强制注入，有预算、content-hash 去重 |
| 自我度量 | 无 | `aipmc metrics` 对照文档化目标持续检查 |

真正的护城河是这个组合：**被动捕获 + 流量层强制注入 + 问责链**——记忆工具和 PM 工具单家都给不了这三样。

## 它解决什么问题

1. **Agent 之间互相不知道对方做了什么** — 每个 Agent 只能看到自己的对话历史
2. **编码过程中的知识丢失** — Bug 排查过程、架构决策、修改意图散落在各 Agent 的对话中
3. **没有"项目记忆"** — 每个 Agent 每次启动都是重新开始，不知道上次做到哪、有什么遗留问题

## 工作机制

| 层 | 机制 | 做什么 |
|----|------|--------|
| **L0 捕获** | Hook 自动拦截 | Agent 的对话、工具调用、文件操作自动记录到 SQLite，零配置 |
| **L1-L3 分析** | Pipeline 周期扫描 | B1 规则扫描（盲改检测、合规检查）→ L2 AI 语义摘要 → L3 commit↔session 关联 |
| **实时注入** | INJECT context | 每次 Agent 请求时自动注入「最近做了什么、有什么警告」，去重、有预算 |
| **事件闭环** | MCP + 评估 | Agent 通过 40+ MCP 工具读写任务/决策/bug，`aipmc metrics` 持续度量效果 |

## 架构

```
Agent (Claude Code / Codex CLI，真实流量打磨)
    │                                        │
    │  API 请求 (:19530)                     │  Hook（工具调用事件）
    ▼                                        ▼
┌────────────────┐                   ┌──────────────┐
│     Proxy      │                   │ Discussion   │
│  协议翻译/捕获  │                   │ Log (SQLite) │
│  INJECT 注入    │                   └──────┬───────┘
└───────┬────────┘                          │
        │                                   ▼
        │                          ┌──────────────┐
        │                          │  Pipeline    │
        │                          │  B1→L2→L3    │
        │                          │  30min 周期   │
        │                          └──────┬───────┘
        │                                 │
        │                           INJECT ▼
        │                   ┌────────────────────┐
        │                   │ 下次 Agent 请求时   │
        │                   │ 自动注入上下文      │
        │                   └────────────────────┘
        ▼
┌────────────────────────────┐
│  Web UI (:8720) + REST API │  Activity 关系图、任务看板、讨论/线索、
│  + 后台 Pipeline           │  Agents 状态板、Chat、审计、配置管理
└────────────────────────────┘
```

- **Proxy (:19530)**: 协议翻译（Anthropic ↔ OpenAI ↔ Gemini）+ Codex Responses 原生透传、流量捕获（`/__proxy/capture`）、INJECT 上下文注入、discussion 读取去重（省 prefix cache）
- **Hooks**: Claude Code / Codex CLI 的工具调用自动捕获 + post-commit git hook，失败可见化
- **Pipeline**: B1 规则扫描 → L2 AI 语义摘要 → L3 commit↔session 关联，后台 30 分钟循环自动跑
- **INJECT**: content-hash 去重、预算截断、warnings 优先的实时上下文注入，注入明细可观测（`[INJECT]` 日志）
- **PM DB**: 每项目独立 `.pmai/` SQLite（纯 Go，无 CGO），零配置

## 核心能力

### 协议网关与凭据

- 三协议翻译：Anthropic、OpenAI 兼容、Gemini，外加 Codex `/v1/responses` 原生透传
- 虚拟模型注册表 `~/.aipmc/models.json`：一个模型 ID 路由到任意 Provider
- 凭据加密存储：AES-256-GCM 加密的 API Key（纯 Go 标准库，无 CGO），`0600` 权限，支持多 profile（`aipmc key` 系列命令）
- 代理内命令 `&aipmc-model`：对话中直接切换模型；`sessions` 子命令查看活跃 Agent 状态板

### Agent 接入与捕获

- `aipmc setup <platform>` 一行配置 hooks + MCP，支持 claude / codex / gemini / cursor / opencode
- `aipm_list_sessions` / `aipm_update_status`：跨项目 Agent 状态板，实时看到每个 Agent 在做什么
- 工具调用、文件操作、对话消息全部落库，rune-safe 截断、跨消息去重

### 知识沉淀与检索

- **讨论库**: 所有 Agent 对话历史集中存储，`aipm_read_discussions` 支持 cursor 增量、session 过滤、时间窗口
- **实体模型**: Roadmap → Plan → Task → Commit 层级 + Bug / Decision / Idea / Principle / Docs，无孤儿、无回填
- **搜索**: FTS5 全文 + 中文 2-gram 召回 + AI 语义重排（`aipm_smart_search`），结果带命中片段
- **线索 Threads**: 跨 plan 聚合相关工作流，`aipm_daily_review` 每日自动分析 commit 关联

### Web UI（React 18 + Ant Design 5，内嵌于二进制）

- Project: Activity 关系图（实体/文件/会话）、Governance（决策+原则）、Plans & Tasks、Commits、Threads、Bugs
- Knowledge: Discussions、Inbox（Idea 漏斗）、Docs、Daily、Code
- Collab: Chat（直接与 Agent 会话）、Agents 状态板、Audit Log
- Config: Proxy 面板（启停/模型切换/流量捕获）、Settings

### 可观测性与评估

- 统一日志 `~/.aipmc/logs/aipmc.log`：BOOT 版本锚点（git sha）、project 标签、20MB 自动归档（保留 7 份）、非法 UTF-8 清洗
- `aipmc metrics`：只读评估命令，对照 `docs/EVALUATION.md` 目标检查覆盖/注入率/事件处理率等
- MCP 工具使用日志、事件处理追踪（`aipm_mark_consumed` / `aipm_mark_event_processed`）

## 快速开始

```bash
# 编译（前端 npm build + Go 二进制；-f 跳过前端）
./build.sh

# 一条命令启动：Web UI + 内嵌 Proxy + 后台 Pipeline
./aipmc serve

# 浏览器打开 http://127.0.0.1:8720
```

首次启动自动注册当前目录为项目，初始化 `.pmai/`；同一项目会拒绝多实例并发写日志。

新用户请先读 **[docs/QUICKSTART.md](docs/QUICKSTART.md)** — 常用指令分步上手。

## Agent 接入

```bash
# 为 Agent 配置 hooks + MCP（一行命令）
aipmc setup claude    # Claude Code
aipmc setup codex     # Codex CLI
aipmc setup gemini    # Gemini CLI
aipmc setup cursor    # Cursor
aipmc setup opencode  # OpenCode

# 启动 Agent（自动加载项目配置，Proxy 需先运行）
aipmc agent claude
aipmc agent codex
```

> **平台支持（诚实分级）**：Claude Code 和 Codex CLI 是经过真实流量打磨的路径——本项目自己每天都在用，hook 边界问题都是按真实流量修的。Gemini CLI、Cursor、OpenCode、Windsurf、Cline、Roo Code 提供一行配置（config + hooks），但没有持续在真实场景运行，边界可能有毛刺。

## MCP 工具（40+，Agent 可在对话中调用）

### 简报与上下文

| 工具 | 用途 |
|------|------|
| `aipm_get_briefing` | 项目简报：进行中任务、风险、最近活动、活跃线索 |
| `aipm_analyze` | 全量健康分析（scope 漂移、孤儿任务、重复 plan、阻塞超时） |
| `aipm_search_context` / `aipm_smart_search` | 关键词 / AI 语义搜索全部 PM 实体 |
| `aipm_read_discussions` | 读取其他 Agent 对话（cursor 增量、时间窗口） |
| `aipm_search_discussions` | 按关键词搜索讨论内容（中文 2-gram） |

### 记录与写入

| 工具 | 用途 |
|------|------|
| `aipm_create_task` / `aipm_update_task` / `aipm_update_task_status` | 任务生命周期（done 带 gate 检查） |
| `aipm_record_commit` / `aipm_record_commits` | 记录 commit + scope drift 检测 |
| `aipm_record_decision` / `aipm_record_bug` | 沉淀架构决策与 bug（根因 + 修复方案） |
| `aipm_link_entities` / `aipm_append_task_note` | 实体关联与备注 |

### 协作与自检

| 工具 | 用途 |
|------|------|
| `aipm_list_sessions` / `aipm_update_status` | Agent 状态板：谁在做什么 |
| `aipm_create_thread` / `aipm_add_to_thread` / `aipm_daily_review` | 跨 plan 线索聚合与每日 review |
| `aipmc_vision` | 截图自检 UI 效果（改代码 → 截图 → 看图验证闭环） |
| `aipm_submit_feedback` | 工具使用反馈（bug / 改进建议） |

完整工具列表见 `aipmc help` 与 MCP server（`aipmc mcp`）。

## CLI 速览

```bash
aipmc init                     # 初始化项目 + 自动装 post-commit hook
aipmc setup <platform>          # 配置 Agent 平台（无参数列出选项）
aipmc serve                    # 启动 Web UI + Proxy + Pipeline
aipmc proxy [--profile <name>] # 单独启动协议代理
aipmc chat                     # 命令行直接与 Agent 会话
aipmc metrics [--since all]    # 只读评估指标（对照 docs/EVALUATION.md）
aipmc key init/set/list/show   # 凭据管理（AES-256-GCM 加密，多 profile）
aipmc models                   # 模型注册表管理
aipmc task|commit|plan|bug|decision|idea|roadmap|principle [CRUD]
aipmc search|doctor|info|daily|session|thread|link|canon|event
```

## 配置

Web UI 的 Settings / Proxy 页面提供完整配置管理。核心文件：

| 文件 | 内容 |
|------|------|
| `~/.aipmc/models.json` | LLM 网关：Provider 注册 + 虚拟模型定义（含 responses 协议字段） |
| `~/.aipmc/credentials` | AES-256-GCM 加密的 API Key（`0600` 权限，多 profile） |
| `~/.aipmc/config.json` | 全局：代理端口/绑定、upstream、Anthropic URL |
| `.pmai/config.json` | 项目级：AI 模型、Agent 覆盖 |
| `~/.aipmc/logs/aipmc.log` | 统一共享日志（20MB 归档） |

## 开发

### 技术栈

| 层 | 技术 |
|----|------|
| 后端 | Go 1.25（`modernc.org/sqlite` 纯 Go，无 CGO） |
| 前端 | React 18 + Vite 5 + Ant Design 5（`go:embed frontend/dist` 打进二进制） |
| 加密 | Go 标准库 AES-256-GCM + PBKDF2-SHA256（纯 Go，无 CGO） |
| CI | GitHub Actions：前端 build + `go vet` + `go test` + build |

### 常用命令

```bash
./build.sh            # 完整构建（前端 + 二进制到 dist/）
./build.sh -f         # 跳过前端构建
go test ./...         # 后端测试
cd frontend && npm run dev   # 前端热更新开发
```

### 目录结构

```text
proxy/     协议翻译、INJECT、流量捕获、模型路由
hook/      Claude/Codex/Gemini/Cursor/OpenCode hooks + post-commit
session/   会话摘要、自动 pipeline、状态板、git 同步
store/     SQLite CRUD、讨论、审计、日常
search/    FTS5 + 中文 2-gram + AI 重排
mcp/       MCP server（40+ 工具）
api/       REST API（Web UI 数据接口）
web/       HTTP server
agent/     Agent 会话服务（Chat）
db/        schema、配置、凭据
frontend/  React 单页应用
docs/      设计文档与评估目标（EVALUATION.md 等）
```

## License

MIT
