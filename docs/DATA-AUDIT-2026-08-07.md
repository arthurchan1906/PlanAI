# AIPMC 数据资产全景审计 & 反馈镜子 v2 数据源

> 审计日期：2026-08-07
> 参与者：Claude（主审）、Codex（反馈镜子 v1 实现）
> 覆盖项目：aipmc + EncryptDrive

---

## 1. 数据源全景

### 1.1 日志类（`~/.aipmc/logs/aipmc.log`，212,107 行）

| Tag | 行数 | 内容 | 当前 metrics | 未利用的信号 |
|---|---|---|---|---|
| `[PIPELINE]` | 44,055 | 后台 pipeline 调度 | ❌ 无 | 运行频率、scan 覆盖、reconcile 成功率 |
| `[INJECT]` | 22,227 | 上下文注入 | ✅ C3 部分 | cooldown 跳过率、same_content 命中率、file_assoc 成功率 |
| `[LLM]` | 13,425 | LLM API 调用 | ✅ 消耗参考 + C1 | cache_hit/cache_create 按路径覆盖率（responses 路径缺失）、model 分布、passthrough vs translation 比例 |
| `[MCP]` | 2,927 | aipm MCP 工具调用 | ❌ 无 | 工具成功率、按 tool 分布、按 agent 分布、读写比 |
| `[YIELD]` | 1,194 | Agent 结构化输出信号 | ❌ 无 | 信号类型趋势（review/done/plan/sum）、agent 自觉性 |
| `[RECONCILE]` | 433 | Session 链接 | ❌ 无 | auto_linked 率、per project 分布 |
| `[GITSYNC]` | 432 | Git commit 同步 | ❌ 无 | 同步频率、created/updated 比 |
| `[EVENT]` | 403 | 事件去重 | ❌ 无 | dedup skip 率（高说明重复事件多） |
| `[HOOK]` | 少量 | Hook 执行 | ✅ B8 hook_error | panic 恢复率 |
| `[MCP-ERR]` | 13 | MCP 工具错误 | ❌ 无 | 按 tool 细分 |
| `[DONE-GATE]` | 少量 | Task done 校验 | ❌ 无 | 通过率、拒绝原因 |

### 1.2 数据库类（`.pmai/data/pmai.db`）

| 表 | 记录数（aipmc/ED） | 当前 metrics | 未利用的信号 |
|---|---|---|---|
| `discussion_log` | ~17k / ~34k | ❌ 间接 | MCP 采纳率、session 完整性、agent 活跃度 |
| `commits` | 132 / 876 | ❌ 无 | orphan 率、hook vs MCP 来源比、review 通过率 |
| `tasks` | 67 / 167 | ❌ 无 | 完成率、avg 生命周期、blocked 数 |
| `bugs` | 6 / 27 | ❌ 无 | 修复率、open 债务、avg 修复时间 |
| `events` | 205 / 423 | ✅ B6/D2 | 按 type 分布、产生速率 |
| `session_summaries` | 91 / 65 | ✅ B1/B2 | quality_score 按 agent（claude 66 vs codex 44）、workflow_completed 率 |
| `token_usage` | 按 session 维度 | ❌ 无 | token/session、model 分布（补日志缺 session_id 的短板） |
| `graph_edges` | 3,224 / — | ❌ 无 | 实体关联密度、孤立实体数 |
| `decisions` | 16 / 32 | ❌ 无 | accepted vs deprecated 比 |
| `plans` | 5 / 22 | ❌ 无 | 完成率、active vs done 分布 |
| `threads` | 6 / 3 | ❌ 无 | 活跃度 |

### 1.3 运行时数据（非持久化）

| 来源 | 内容 | 未利用的信号 |
|---|---|---|
| Proxy capture（`/__proxy/capture`）| 请求/响应镜像 | passthrough vs translation 判据、各协议流量占比 |
| `~/.aipmc/current_model` | 模型切换记录 | 切换频率、切换模式 |
| `~/.aipmc/config.json` | 配置状态 | 与 registry 一致性校验 |

---

## 2. 数据采集管道完整性审计

### 2.1 🔴 数据模型不一致：role 字段语义断裂

**现象**：不同 agent 的 hook 把 MCP 工具调用存在不同的 role 下。

| Agent | MCP 调用存储格式 | 真实 MCP 调用数（aipmc/ED） |
|---|---|---|
| Claude | `role='tool'` + 📡 前缀 | 1,247 / 2,134 |
| Codex | `role='assistant'` + 📡 前缀（**无 tool role**）| 327 / 552 |
| Cursor | `role='assistant'` + 📡 前缀 | 20 / 86 |
| OpenCode | `role='assistant'` + 📡 前缀 | 118 / 34 |
| Gemini | 几乎没有 | 2 / 0 |

**影响**：任何依赖 `role='tool'` 的查询完全忽略非 Claude agent 的 MCP 使用。应改用 `📡 aipm_` 或 `🛠 MCP:aipm` 前缀作为统一的 MCP 调用识别标准。

**结论（8/7 修正）**：role 差异是原生协议差异（Claude 的 tool_use 是独立 role，Codex 的 tool_call 内嵌在 assistant 消息里），**写入时强行统一会丢语义，读取时归一化即可**（`role='tool' OR (role='assistant' AND 内容含 📡 前缀)`）。不迁移历史数据。

### 2.2 🔴 关键词污染：文本匹配的假阳性

**现象**：`LIKE '%aipm%'` 匹配到大量非 MCP 调用的内容。

| 项目 | Agent | 真实 MCP | 噪声 | 膨胀率 | 噪声来源 |
|---|---|---|---|---|---|
| aipmc | codex | 327 | 2,850 | **8.7x** | shell 路径 `/Users/.../aipmc/` |
| aipmc | claude | 1,247 | ? | — | — |
| EncryptDrive | codex | 552 | 602 | **1.1x** | 用户 prompt 里说「使用 aipm」+ 直接跑 aipmc CLI |

**根因**：`📡 aipm_` 前缀是最可靠的匹配方式，之前的设计没有统一识别标准。

**结论（8/7 修正）**：**双源互补，以 `[MCP]` 日志为主**——`[MCP]` 日志是结构化数据（`tool=/status=/src=`），精确计数（服务端视角）；discussion_log 的 📡 前缀只做上下文（客户端视角、agent 维度）。`[MCP]` 日志已补 `src=` 字段（`e75b726`，取自 MCP initialize 的 clientInfo），不再需要从 discussion_log 文本反推。

### 2.3 🔴 StoreGitCommit 静默丢数据（已修复 `97ce8140`）

**根因**：`LIKE commit_hash \|\| '%'` 在空 hash 行上退化为 `LIKE '%'` → 匹配一切 → 新 commit 被合并到旧空行 → 静默丢弃。

**影响**：8/7 13:38 后 aipmc 库 6 个 commit（含 B4 基线文档）被吞。Hook 日志显示 `status=OK` 但数据库无新行。

**修复**：预检排除空 hash 行 + UPDATE 回填错误检查 + 回归测试。

**启示**：数据采集路径的日志显示「成功」不代表数据真的落库。需要端到端验证（hook 日志 count vs DB count）。

### 2.4 🟡 Hook 安装延迟 + MCP 主动关联差异

**现象**：aipmc 孤儿率 41.7%，EncryptDrive 孤儿率 0.3%，差距 139 倍。

| 项目 | Hook 安装日 | 安装前孤儿率 | 安装后孤儿率 | 真正原因 |
|---|---|---|---|---|
| aipmc | 7/29 | 56.1% | 5.6% | Hook 晚了 29 天 + StoreGitCommit bug |
| EncryptDrive | 7/30 | **0.3%** | 0.35% | Agent **主动用 `aipm_record_commit` MCP** 关联 |

**根因不是 hook，是 Agent 行为差异**：EncryptDrive 的 agent 在 `git commit` 后主动调用 `aipm_record_commit` MCP，不依赖 git hook。aipmc 的 agent 没有这个习惯。

### 2.5 🟡 LLM 日志字段不统一

**5 处 LLM 写入点，字段不一致**：

| 写入点 | cache_hit | cache_create | injected |
|---|---|---|---|
| `anthropic_passthrough.go:140` | ✅ | ✅ | ✅ |
| `proxy.go:963`（unified 同步）| ❌ | ❌ | ✅ |
| `proxy.go:1128`（unified 流式）| ✅ | ❌ | ✅ |
| `proxy.go:1207/1275`（OpenAI Chat）| ❌ | ❌ | ✅ |
| `responses_passthrough.go:114/185` | ❌ | ❌ | ❌ |

**影响**：codex responses 路径的 cache_rate 和 inject_rate 永远为 0 / N/A。

### 2.6 🟢 讨论记录不完整

EncryptDrive 有 12 个 claude session、6 个 cursor session 只有 user 消息没有 assistant 响应——hook 未捕获到完整的对话周期。

### 2.7 🟢 MCP 工具错误缺少聚合视角

`events.type='mcp_error'` 记录了 14 次错误，`[MCP-ERR]` 日志记录了 13 次，但都没有按 tool 聚合。`update_task_status`（6 次）和 `record_commit`（7 次）是主要错误源。

### 2.8 🔴 EncryptDrive commits 表 hash 列大面积残缺（547 行空 hash）

ED 库 876 个 commit 记录中 **547 个（62.4%）无 commit_hash**——来源不可追踪。按 review/test 状态分类：

| 特征（review/test）| 行数 | 来源判断 |
|---|---|---|
| `pending/not_run` | **538**（534 committed + 3 approved 状态 + 1 draft） | MCP `aipm_record_commit` **不带 commit_hash 参数**（CreateCommit 默认 pending/not_run；hook 路径写 auto/auto） |
| `approved/passed` | 3 | MCP 手动补录 |
| 其他 | 6 | MCP 记录 |

**根因：MCP 工具设计问题——`aipm_record_commit`/`aipm_record_commits` 允许不带 hash 创建 commit 记录**（538/547 = 98.4%），而非 StoreGitCommit bug 残留。Agent 从 git 拿 hash 是成本极低的，工具应强制要求或自动从 git 补全。

**修复（8/7 已落地）**：store 层 `CreateCommit`/`BatchCreateCommits` 强制 commit_hash 必填（空 hash 直接报错，错误信息提示 `git rev-parse HEAD`）+ MCP 工具描述改为必填 + 回归测试。**水龙头已关**；存量 547 行待一次性清理脚本评估（无法补的按来源归档/删除，可补的用 git log 回填）。

### 2.9 🟡 Worktree 提交无法记录（采集缺口）

`.claude/worktrees/` 下的 git worktree 提交全部失败：`PMAI database not found: <worktree>/.pmai/data/pmai.db`（7 条 ERR，10:53-15:34）。worktree 没有自己的 `.pmai`，hook 也不会回退到主仓库。

**修复方向**：hook 在 cwd 无 `.pmai` 时用 `git rev-parse --git-common-dir` 回退主仓库的 `.pmai`（P3，worktree 是临时分支影响有限）。

---

## 3. MCP 使用深度分析

### 3.1 采纳率（按会话）

| Agent | aipmc | EncryptDrive | 解读 |
|---|---|---|---|
| claude-code | **51.2%** | 41.8% | aipmc 的 claude session 更集中使用 MCP |
| codex-cli | 30.4% | **62.2%** | 差距来自 aipmc 有 25 个 test session（"say hi"） |
| cursor | 28.6% | 18.2% | 都低——受 getInt 类型 bug 影响 |
| opencode | 30.0% | 15.4% | 都低 |

### 3.2 使用强度（每次 MCP session 的平均调用）

| Agent | aipmc | EncryptDrive |
|---|---|---|
| claude-code | 57.1 | **92.8** |
| codex-cli | 23.4 | 24.0 |
| cursor | 10.0 | 43.0 |

### 3.3 绕过行为（直接 SQL 替代 MCP）

| Agent | aipmc 绕过次数 | EncryptDrive 绕过次数 | 绕过原因 |
|---|---|---|---|
| codex-cli | 335 | 大量 | 复杂分析 MCP 不支持 + 习惯性扩散 |
| claude-code | 120 | — | 部分 session 直接用 SQL 读 discussion |
| cursor | 109 | — | getInt 类型 bug → MCP 失败 → 被迫绕过 |

**cursor 绕过是技术故障驱动，codex 绕过是能力缺口驱动。前者修 bug 即可，后者需要扩展 MCP 工具。**

### 3.4 用户显式指令 vs Agent 行为

用户曾直接质问 cursor：
> "为什么你查 discussion 似乎是直接查询数据库的 为什么没有使用 mcp 的功能"

cursor 回应：花了 30+ 次 tool call **调试 MCP 的 getInt 类型 bug**，确认 `last_n: 5`（整数）失败而 `last_n: "5"`（字符串）成功。在修复之前，每次 MCP 失败就回退到 SQL。

**结论：Agent 不是不愿意用 MCP，是可靠性不够。**

### 3.5 双源互补架构（8/7 定稿）

**问题**：`[MCP]` 日志有 2,927 行但无 agent 字段，曾被迫用 discussion_log 文本反推（role 不一致 + 关键词污染双失真）。

**方案**：

| 源 | 视角 | 用途 | 状态 |
|---|---|---|---|
| `[MCP]` 日志（`tool=/status=/src=/name=`）| 服务端精确计数 | 调用数、成功率、工具分布、读写比 | ✅ 已补 `src=`（`e75b726`） |
| discussion_log（📡 前缀）| 客户端上下文 | agent 维度、会话采纳率、绕过行为 | 读取时 role 归一化 |

`src=` 取自 MCP initialize 的 `clientInfo`（stdio transport = 每 agent 独立进程，clientInfo 连接期内恒定，无跨 agent 残留）；`name=` 保留客户端原始声明值，映射表不全时可追溯。

**原则：任何 MCP 指标以 `[MCP]` 日志为准，discussion_log 只做交叉验证与上下文。**

---

## 4. 数据质量标准建议

### 4.1 埋点规范

```
1. MCP 调用双源：结构化 [MCP] 日志（tool/status/src/name）为准 + discussion_log 📡 前缀做上下文
2. LLM 日志 5 处写入点字段对齐：cache_hit, cache_create, injected 三字段全覆盖
3. 端到端校验：hook 报 OK 后验证「操作声称的结果」存在（新 hash+title 双匹配），不是「某个 ID 能查到」
4. role 读取时归一化（tool OR assistant+📡），不迁移存储
5. done-gate 硬性条件：commit_hash 非空（e75b726）
```

### 4.2 定期查证项

| 查证项 | 方法 | 频率 |
|---|---|---|
| hook commit count vs DB commit count | `grep -c 'post-commit status=OK'` vs `SELECT COUNT(*) FROM commits WHERE created_at > ...` | 每次 metrics 运行时 |
| commit 完整性三件套 | `orphan_rate`（task 关联）/ `hash_traceability`（hash 非空率）/ `hash_uniqueness`（去重率）同一行展示，**任一标红 → 采集管道异常告警**；统计窗口 = 自 `e75b726`（done-gate 修复）之后，避免历史污染淹没信号 | 每次 metrics |
| MCP 调用完整性 | 按 agent 对比 `[MCP]` 日志 `src=` count vs session 总数 | 每次 metrics |
| LLM 字段覆盖率 | 统计含 cache_hit 的 [LLM] 行占比 | 每周 |
| 讨论 session 完整性 | user msg count vs assistant msg count per session | 每周 |

### 4.3 可解释性规则

任何 metrics 指标的变更必须能追溯到以下之一：
- 代码修改（commit hash）
- 配置变更（config.json diff）
- Agent 行为变化（session 采样）
- 数据采集 bug 修复（如 StoreGitCommit）

**无法解释的指标波动 = 数据采集 bug，不是真实变化。**

**验证对象反模式（8/7 共识，通用规则）**：
> 操作成功的验证 = 用「操作声称的结果标识」（新 hash / title / 新 id）反查结果是否存在；
> 不是用「预检/前置查询的匹配对象」做存在性检查。

已知实例（全部已修）：
- `StoreGitCommit`：验证 `GetCommit(existingID)` 匹配的是旧空 hash 行 → 改为预检排除空 hash（`97ce8140`）
- done-gate：验证「有 approved+passed 的 commit」但未查 hash 真实性 → 加 `commit_hash IS NOT NULL AND commit_hash != ''`（`e75b726`）

同类风险待排查：`CreateBug`（验证 commit id 存在但未验证 bug 创建成功）、`CreateTask` 的返回路径。
