# AIPMC 快速上手 — 常用指令手册

> 目标：让新用户 15 分钟内跑起来并开始用。完整能力见 [README](../README.md)，深层原理见 [DESIGN](DESIGN.md)。

## 0. 前提

- Go 1.25+（`go version`）
- 构建前端需要 Node.js + npm；只想用后端可以用 `./build.sh -f` 跳过
- 机器上已装你要接的 Agent（Claude Code / Codex CLI）

## 1. 构建

```bash
cd <项目目录>
./build.sh        # 完整构建（前端 + 二进制 → dist/aipmc）
./build.sh -f     # 跳过前端（无 Node 环境时）
```

## 2. 初始化与启动

```bash
aipmc init        # 初始化当前目录为项目：生成 .pmai/ + 自动安装 post-commit hook

aipmc serve       # 一条命令：Web UI (:8720) + 代理 (:19530) + 后台 pipeline
```

`serve` 首次运行会注册当前目录为项目；浏览器打开 http://127.0.0.1:8720。

其他启动方式：

```bash
aipmc proxy                       # 只启动协议代理（供 aipmc agent 等单独使用）
aipmc serve --project <路径>      # 指定项目目录
aipmc serve --profile <name>      # 指定凭据 profile（见第 4 节）
```

## 3. Agent 接入（核心路径：Claude Code / Codex CLI）

```bash
# 一行配置 hooks + MCP
aipmc setup claude
aipmc setup codex

# 启动 Agent（需先运行 proxy；会自动加载项目配置）
aipmc agent claude
aipmc agent codex
```

> 其他平台（gemini / cursor / opencode / windsurf / cline / roo）也有 `aipmc setup` 入口，但未经过真实流量打磨，边界可能有毛刺。

配置完成后，Agent 的 tool list 里会出现 `aipm_*` MCP 工具（40+），直接在对话里调用即可。首次建议让 Agent 调 `aipm_get_briefing` 看看项目简报。

对话中还能直接切模型：

```text
&aipmc-model list        # 列出可用模型
&aipmc-model switch xxx  # 切换当前模型
&aipmc-model sessions    # 查看活跃 Agent 状态板
```

## 4. 凭据与模型

```bash
# API Key 管理（AES-256-GCM 加密，0600 权限，支持多 profile）
aipmc key init                     # 创建 profile（设主密码）
aipmc key set <provider> <key>     # 保存某个 provider 的 key
aipmc key list                     # 列出所有 key（脱敏）
aipmc key show <provider>          # 查看明文（需要主密码）
aipmc key status                   # 凭据状态检查

# 模型注册表（虚拟模型 → Provider 路由）
aipmc models list                  # 列出虚拟模型
aipmc models current               # 当前生效模型
aipmc models switch <model-id>     # 切换默认模型
```

配置文件：`~/.aipmc/models.json`（模型网关）、`~/.aipmc/credentials`（加密凭据）、`.pmai/config.json`（项目级配置）。

## 5. 日常 CLI

### 任务

```bash
aipmc task list --status in_progress   # 列出进行中任务
aipmc task show --id task-xxx          # 查看任务详情
aipmc task add --title "实现 X" --priority P1 [--plan_id plan-xxx]
aipmc task update --id task-xxx --status done --note "完成说明"
aipmc task note --id task-xxx --content "补充备注"
aipmc task notes --id task-xxx
```

### Commit（必须挂到 task 上，否则报错）

```bash
aipmc commit add --task-id task-xxx --title "feat: X" \
  --commit-hash <git sha> --status committed \
  --test-status passed --review-status approved
```

### 其他实体

```bash
aipmc plan list|show|add|update
aipmc bug list --status open|show|add|update
aipmc decision list|show|add|review
aipmc idea list|show|capture|review|update|comment|convert
aipmc roadmap list|show|add|update
aipmc principle list|show|add|update
aipmc link list|add|delete
```

### 查询与日常

```bash
aipmc search "<关键词>" --limit 8     # 跨实体搜索（任务/commit/讨论等）
aipmc daily show [--date 2026-08-17]  # 查看当日笔记
aipmc doctor                          # 环境自检（DB 是否可用等）
aipmc info                            # 项目与命令概览
aipmc chat                            # 终端里直接和 Agent 对话
```

## 6. Agent 侧最常用的 MCP 工具

| 工具 | 什么时候用 |
|------|-----------|
| `aipm_get_briefing` | 启动/接手时：进行中任务、风险、最近活动 |
| `aipm_search_context` | 搜索历史任务/决策/bug |
| `aipm_create_task` | 接到新工作，先建 task |
| `aipm_record_commit` | 完成一轮修改后记录 commit（带 scope drift 检测） |
| `aipm_read_discussions` | 看别的 Agent 做过什么 |
| `aipm_list_sessions` / `aipm_update_status` | 报个到 / 看谁在做什么 |
| `aipm_daily_review` | 收尾时查看当日 commit 关联 |

## 7. Web UI 速览（:8720）

- **Activity**：实体/文件/会话关系图，先看这个了解全局
- **Plans & Tasks**：任务看板（日常主视图）
- **Discussions / Threads**：Agent 讨论与跨 plan 线索
- **Agents**：活跃 Agent 状态板
- **Chat**：网页里直接和 Agent 对话
- **Proxy**：代理启停、模型切换、流量捕获
- **Settings**：配置管理

## 8. 评估与日志

```bash
aipmc metrics                     # 对照 docs/EVALUATION.md 的指标检查
aipmc metrics --since 2026-08-01  # 自定义窗口
aipmc metrics --window 24h        # 日志类指标只看最近 24h

# 日志位置
~/.aipmc/logs/aipmc.log           # 共享日志（PIPELINE/INJECT/MCP 等，20MB 自动归档）
# 代理流量捕获（内存环形缓冲，不进文件）
curl http://127.0.0.1:19530/__proxy/capture?per_page=5
```

## 9. 常见问题

| 现象 | 排查 |
|------|------|
| `serve` 提示代理未运行 | 先 `aipmc proxy`，或在 Web UI Proxy 页启动 |
| 端口被占 | `serve` 会检测并提示；同一项目拒绝多实例并发 |
| Agent 没有 `aipm_*` 工具 | 检查是否跑过 `aipmc setup <platform>`，重启 Agent 会话 |
| L2 摘要 401 / 无 AI 摘要 | 检查 `aipmc key status` 与 `.pmai/config.json` 的模型配置 |
| 凭据功能 | 默认可用（纯 Go AES-256-GCM，无需 GmSSL/CGO）；`./build.sh` 纯 Go 构建 |
