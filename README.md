# PlanAI — AI 项目管理器

PlanAI 是一个面向 AI 编程 Agent 的**项目管理中间层**。它不只是转发请求，它**观察、理解、记忆** Agent 的工作过程。

## 它解决什么问题

在多 Agent 协作（Claude Code、Codex CLI、Gemini CLI）中，有三个核心痛点：

1. **Agent 之间互相不知道对方做了什么** — 每个 Agent 只能看到自己的对话历史，无法了解其他 Agent 的进展
2. **编码过程中的知识丢失** — Bug 排查过程、架构决策、修改意图散落在各 Agent 的对话中，无法沉淀
3. **没有"项目记忆"** — Agent 每次启动都是重新开始，不知道上次做到哪了、有什么遗留问题

PlanAI 通过三层机制解决这些问题：

| 层 | 机制 | 做什么 |
|----|------|--------|
| **L0 捕获** | Hook 自动拦截 | 所有 Agent 的对话、工具调用、文件操作自动记录到 SQLite |
| **L1-L3 分析** | Pipeline 周期扫描 | 从对话中提取目标、检测盲改循环、关联 commit 与 session |
| **实时注入** | INJECT context | 下次 Agent 启动时，自动注入「最近做了什么、有什么警告」 |

## 一句话理解

```
没有 PlanAI：Agent 每天从零开始，不知道昨天做了什么
有 PlanAI：   Agent 启动时自动收到「昨天修了 hook bug，注意 vision MCP 管道还有遗留问题」
```

## 架构

```
Agent (Claude/Codex/Gemini/Cursor)
    │                            │
    │  API 请求                  │  Hook (工具调用事件)
    ▼                            ▼
┌──────────┐              ┌──────────────┐
│  Proxy   │              │ Discussion   │
│  协议翻译 │              │ Log (SQLite) │
│  流量捕获 │              └──────┬───────┘
└────┬─────┘                     │
     │                           ▼
     │                   ┌──────────────┐
     │                   │  Pipeline    │
     │                   │  B1→L2→L3    │
     │                   │  周期扫描     │
     │                   └──────┬───────┘
     │                          │
     │                    INJECT ▼
     │              ┌────────────────────┐
     │              │ 下次 Agent 请求时   │
     │              │ 自动注入上下文      │
     │              └────────────────────┘
     ▼
┌──────────┐
│  Web UI  │  任务看板、讨论浏览、Agent 启动、配置管理
│  :8720   │
└──────────┘
```

- **Proxy (:19530)**: 协议翻译（Anthropic ↔ OpenAI ↔ Gemini）、流量捕获、Anthropic 透传
- **Hooks**: Agent 工具调用时自动捕获讨论记录、文件操作元数据
- **Pipeline**: B1 规则扫描（盲改检测、合规检查）→ L2 AI 语义摘要 → L3 commit↔session 关联
- **INJECT**: Content-hash 去重、800 chars 上限、warnings 优先的实时上下文注入
- **Web UI (:8720)**: 任务看板、讨论历史、Agent 启动器、模型配置
- **PM DB**: 每项目独立 `.pmai/` SQLite，零配置

## 快速开始

```bash
# 编译
./build.sh

# 启动（proxy + web + pipeline 自动运行）
./aipmc serve

# 浏览器打开 http://127.0.0.1:8720
```

首次启动自动注册当前目录为项目，初始化 `.pmai/`。

## Agent 接入

```bash
# 为 Agent 配置 hooks + MCP（一行命令）
aipmc setup claude    # Claude Code
aipmc setup codex     # Codex CLI
aipmc setup gemini    # Gemini CLI
aipmc setup cursor    # Cursor
aipmc setup opencode  # OpenCode
```

Agent 启动：

```bash
aipmc agent claude    # 启动 Claude Code（自动加载项目配置）
aipmc agent codex     # 启动 Codex CLI
```

## MCP 工具（Agent 可在对话中调用）

| 工具 | 用途 |
|------|------|
| `aipm_get_briefing` | 获取项目简报（进行中任务、风险、最近活动） |
| `aipm_read_discussions` | 读取其他 Agent 的对话历史（支持 cursor 增量） |
| `aipm_search_discussions` | 按关键词搜索讨论内容 |
| `aipm_smart_search` | AI 语义搜索 PM 实体 |
| `aipm_create_task` | 创建任务 |
| `aipm_record_commit` | 记录 commit |
| `aipm_record_decision` | 记录架构决策 |
| `aipm_record_bug` | 记录 bug（含根因分析和修复方案） |
| `aipmc_vision` | 截图自检 UI 效果 |

## 配置

Web UI 的 Settings 页面提供完整配置管理。核心文件：

| 文件 | 内容 |
|------|------|
| `~/.aipmc/models.json` | LLM 网关：Provider 注册 + 虚拟模型定义 |
| `~/.aipmc/credentials` | SM4-GCM 加密的 API Key（`0600` 权限） |
| `.pmai/config.json` | 项目级：AI 模型、代理端口、Agent 覆盖 |

## License

MIT
