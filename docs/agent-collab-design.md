# Agent 协作感知设计文档

> 状态: **修订版 v1.12（终审第九轮定稿：社区调研借鉴 — agentlocks git wrapper 锁 + scope.json 域校验机制 + 编译互踩定位修正 + 机制生效边界；决策 25 op 字段提前 Phase 1c；窗口统一）** — 2026-08-05
> 本文档固化 Codex/Claude 八轮讨论成果 + 8/5 EncryptDrive 实况验证 + 社区实践借鉴 + Claude/Codex 三轮 challenge 收敛，是后续实施的唯一依据。

---

## 1. 背景与目标

### 1.1 问题

用户同时操作多个 code agent（Claude Code / Codex CLI 等）在同一项目上工作，agent 之间认知隔离：

- 不知道还有谁在干活、各自在做什么
- 不知道谁碰过自己正在改的文件（冲突风险）
- 讨论内容靠用户手动复制粘贴传递
- 切换上下文时信息丢失（"继续"的歧义）

### 1.2 目标（本次范围：L1 感知）

让每个 agent 在请求时自动获知：

1. **我是谁**（当前 session 的身份）
2. **旁边有谁**（活跃的其他 agent）
3. **对方在碰什么文件**（同行活动）
4. **谁和我改了同一个文件**（文件碰触告警）

### 1.3 明确不做（本次）

- ❌ 消息路由（to=agent B）— 7/31 已否，精度不可控
- ❌ agent 主动调用 MCP 作为唯一入口 — 使用率 5%
- ❌ 自动语义抽取（NLP 判断"发现了什么"）— hook 层做不了
- ❌ 事件驱动唤醒（L3）— agent 是被动进程，产品形态未定

---

## 2. 核心架构约束（已验证）

### 2.1 INJECT 拿不到 session_id

`proxy/anthropic.go` 的 `AnthropicRequest` 结构体不包含 session_id / metadata / user_id。
Claude Code 的 session_id 只通过 hook stdin 传入，API 请求不携带。

**推论**：INJECT 无法精确区分"当前是哪个 session"，只能：

- 用 `extractFilePaths(body)` 提取请求中的文件路径
- 展示所有活跃 session（agent 自行辨识自己的 session_id 前缀）
- Phase 2 用启发式（文件重叠度）推断自己，失败模式即正确行为

**⚠️ 已知局限**：「agent 自行辨识 session_id 前缀」是未经论证的能力假设——system message
里没有注入「你是哪个 session」，agent 无法可靠知道自己的前缀。Phase 1 接受此局限（全量展示），
Phase 2 的 guessSelf 是此问题的正式解。

### 2.2 数据现状（实测）

| 数据源 | 内容 | session_id | 文件路径 | task_id |
|--------|------|:---:|:---:|:---:|
| `discussion_log` | 全部对话 + 工具调用 | ✅ | 部分（metadata/desc） | ❌ |
| `session_summaries` | L2 摘要（goal） | ✅ | ❌ | 部分（entity_refs） |
| Claude hook metadata | Write/Edit/Read 路径（role=tool） | ✅ | ✅（Edit 顶层 `$.file_path`；**Read 无 metadata**，实测 0/926） | ❌ |
| Cursor hook metadata | Read/Search/Edit（role=assistant，👁/🔍/📝 前缀） | ✅（= conversation_id，实测 1361/1363） | ✅（689 条含 file_path，其中 Read 403/403） | ❌ |
| Codex hook metadata | Write/Read/apply_patch/Bash | ✅ | ⚠️ **无结构化 `file_path`**（实测 0 条），路径藏在 `tool_input.command` 文本 | ❌ |
| MCP server 日志 | MCP 工具调用（📡 格式） | ⚠️ 代码传空串，store 层归一化为 `unknown`（部分经 MergeOrphans 回填真实 id） | ❌ | ✅（部分工具提取） |

**关键缺陷**：

1. hook 存绝对路径（`/Users/.../mcp/mcp.go`），`extractFilePaths` 出相对路径（`mcp/mcp.go`）— 格式不对称
2. `extractFilePaths` 只解析 `text` block，漏掉 `tool_use`（Write/Edit 的 `input.file_path` 抓不到）
3. **MCP 工具调用对 Claude hook 不可见**（`mcp/mcp.go:2304` 注释「MCP tools are invisible
   to Claude Code hooks」）— 由 `mcpLogDiscussion`（mcp.go:2307）记录，代码传 `session_id=""`，
   store 层（`store/discussion.go:97`）归一化为 `"unknown"`（幽灵 session 的真实值，
   `session/merge_mcp.go:16` 已有 `IsOrphanSessionID` 判定 `""`/`"unknown"`）
4. INJECT 的 peer 数据若每请求实时查询会 hash 漂移
5. **三 hook metadata 结构异构**（实测，8/4 终审第四轮）：Claude 顶层 `$.file_path`
   （Read 无 metadata）；Cursor `_type:post_tool` + 顶层 `$.file_path`（Read 有，403/403）；
   Codex **无结构化 `file_path`**（0 条），路径只存在于 `tool_input.command` 的 Bash
   命令文本（2031 条含项目路径）。方案 C 的「统一补 rel_path」需按 hook 分别实现，
   Codex 需明确覆盖边界（见 3.2 / 4.1）

### 2.3 性能（实测）

- `discussion_log` 16427 行全表 LIKE 扫描 11ms，INJECT 有 5 分钟缓存 — 不需要索引
- 结论：性能不是瓶颈，路径匹配正确性才是

### 2.4 机制生效边界（v1.12 新增——8/5 Claude/Codex 三轮 challenge 收敛）

**核心约束**：我们控制的是第三方 agent（Claude Code / Codex CLI / Cursor 等），不是自有 agent。每个 agent 的能力集不同——PreToolUse hook 事件只有 Claude Code 支持，`--worktree` 只有 Claude Code 原生支持，PostToolUse hook 事件双方均有。

**以下机制不依赖 agent 配合，通过 git 自身互斥 / proxy 注入 / PostToolUse hook 被动生效**：

| 机制 | Claude | Codex | 生效原理 |
|------|:---:|:---:|------|
| **注入感知**（文件碰触 + 折叠计数 + op 状态） | ✅ | ✅ | proxy 层 INJECT，普适 |
| **git 操作排他锁** | ✅ | ✅ | `index.lock` 是 git 自带的互斥——hook 只做排序（Claude 先取锁→先完成），Codex 同时 commit 时 git 自身报锁冲突，**不需要 Codex 配合** |
| **scope.json（warn 模式）** | ✅ | ✅ | PostToolUse hook 双方均有——commit 时校验变更文件是否越界，输出 warn 提示 |
| **GIT_GC_AUTO=0** | ✅ | ✅ | 环境变量，进程级生效 |

**以下机制依赖 agent 特定能力，仅对支持该能力的 agent 生效**：

| 机制 | Claude | Codex | 原因 |
|------|:---:|:---:|------|
| **PreToolUse deny**（Edit/Write 拦截） | ✅ | ❌ | Codex 无 PreToolUse hook 事件 |
| **scope.json（block 模式）** | ✅ | ❌ | block 需要 PreToolUse 拒绝 commit |
| **worktree 编译隔离** | ✅ | ❌ | `claude --worktree` 原生支持；Codex 无等价 CLI flag |

**设计原则**：
- 感知注入（proxy INJECT）是**普适底座**——对所有 agent 生效，不依赖任何 agent 特定能力
- 机制层（锁 / scope / worktree）是**差异化增强**——对支持对应能力的 agent 提供更强保障
- "被动生效" 不等于 "退化"——git 锁通过 `index.lock` 被动生效与主动拦截等价（最终都是互斥）
- 机制对不配合 agent **不是零效果**，而是通过不同路径生效（被动 vs 主动）
- 不追求「全 agent 统一机制」——接受架构不对称，明确标注每个机制的生效范围

---

## 3. 数据层设计：discussion_log metadata 增强（方案 C）

### 3.1 动机

`discussion_log` 是文档库（对话 + metadata 混存、路径格式脏），不适合作为查询表。
**⚠️ 8/4 终审定案：方案 C（不建表 + 写入时在 metadata 补归一化路径）**。

演进过程：

| 方案 | 结论 | 原因 |
|------|------|------|
| A. 建 session_activity 表 | ❌ 放弃 | 双写成本大；Phase 1 只需「谁碰了哪个文件」，不值得 |
| B. 查 discussion_log + 查询端归一化 | ❌ 放弃 | **三 hook metadata 异构**（Claude Read 空 / Cursor Read 有 / Codex 无结构化路径，路径只在 Bash 命令文本），查询端要 parse 3+ 种格式，成本高于写入时归一化（8/4 四轮修正论据） |
| **C. 不建表 + hook 写入时在 metadata 补 `rel_path`** | ✅ **采用** | 写入时归一化一次，查询端 json_extract 精确匹配；无建表成本 |

**⚠️ 8/4 终审第三/四轮：Read metadata 时间分布 + 三 hook 异构核验**。

**Claude Code 范围**（Claude 指控「6 月有、7 月后无，是回归」——不成立，是 Claude hook
一贯设计）：

| 月份 | Edit（📝） | Edit 有 metadata | Read（👁） | Read 有 metadata |
|------|:---:|:---:|:---:|:---:|
| 6 月 | 80 | 80（100%） | 132 | 0（0%） |
| 7 月 | 147 | 147（100%） | 616 | 0（0%） |
| 8 月 | 8 | 8（100%） | 178 | 0（0%） |

**三 hook 横向核验**（8/4 终审第四轮，我的实测；上一轮「全库 0 条 Read 含 file_path」
表述**错误**——只查了 `role=tool`（Claude Code），漏了 Cursor 的 Read）：

| 维度 | Claude Code | Cursor | Codex CLI |
|------|:---:|:---:|:---:|
| Read（👁）记录 | 926 | 403 | — |
| Read 含 file_path | **0（0%）** | **403（100%，全在 2026-06）** | 无（无结构化字段） |
| Edit（📝）含 file_path | 235（100%） | 92（100%） | 无 |
| metadata 结构 | `{"type":"edit","file_path":...}`，Read 为空 `[]` | `{"_type":"post_tool","conversation_id":...,"file_path":...}` | `{"_type":"post_tool","tool_input":{"command":...}}` |
| session_id 列 | 自有 UUID | = metadata.conversation_id（1361/1363） | = metadata.session_id |
| 最后活跃 | 2026-08-04 | **2026-06-29（已停用）** | 2026-08-04 |

**结论**：
- Claude 指控 1「407 条 Cursor Read 含 file_path」**定性成立、数量差 4**（实测 403 条，
  全部 2026-06；Cursor hook 6/29 后停用，非回归）
- 我上轮「全库 0 条」**错误**：Cursor 工具记录 role=assistant（👁 前缀），
  不在 `role=tool` 里
- **方案 C 否 B 的真正论据不是「Read metadata 为空」**（Cursor 的 Read 有），而是
  **三 hook 异构 + Codex 无结构化路径**：查询端要 parse 3+ 种格式（Claude Edit metadata /
  Cursor post_tool / Codex Bash 命令文本），成本高于写入时归一化——结论不变，论据修正

**方案 C 要点**：

- hook 在写入 `LogDiscussion` 时，顺手把归一化相对路径存进 metadata 的 `rel_path` 字段
- **查询端用 `json_extract(metadata, '$.rel_path') = ?` 精确匹配**，替代 LIKE
  （实测：12650 条 metadata 100% valid JSON，json_extract 完全可用）
- **Read 记录也补 metadata**（现在 Claude hook 的 Read metadata 为空，实测确认）——否则 Read 无法参与同行感知
- 项目外文件（`/tmp/fix.py`）→ 不写 `rel_path`（宁缺勿脏）

**为什么用 json_extract 而非 LIKE**（解决 Claude 指出的两个真问题）：

- **转义脆弱**：LIKE 的 `%`/`_` 是通配符，文件名含 `%`/`_` 会误匹配。`json_extract`
  是精确等值匹配，无通配符问题
- **格式耦合**：LIKE 依赖 metadata JSON 的确切格式（字段顺序/嵌套）。`json_extract`
  只依赖字段名 `$.rel_path`，与格式顺序无关，稳定性高于 LIKE

**性能**：json_extract 无索引时全表扫描，量级与 LIKE 相当（万级行毫秒级）。
Phase 2 若需精确路径匹配可评估加 `metadata` 索引或生成列。

### 3.2 metadata 约定（方案 C，不建表）

discussion_log 的 metadata JSON 增加 `rel_path` 字段（hook 写入时计算）。

**⚠️ 三 hook 异构（8/4 终审第四轮定案）**：「统一补 rel_path」是按 **hook 分别实现**
的动作，不是同一段机械代码：

| hook | file_path 来源 | 补 rel_path 方式 |
|------|:---:|------|
| Claude Code | Edit 顶层 `$.file_path`；Read **无 metadata** | Edit 直接加；Read 需先建 `{"type":"read",...}` |
| Cursor | 顶层 `$.file_path`（Read/Edit/Search 都有，实测） | 直接加 rel_path（成本最低） |
| Codex | **无结构化字段**，在 `tool_input.command` Bash 文本里 | 只能靠 `parseBashFileOp` 现算，启发式不稳定（复合命令/管道提不出）→ 需定义覆盖边界 |

```json
// Edit/Write（已有 file_path，新增 rel_path）
{"type":"edit","file_path":"/Users/.../mcp/mcp.go","rel_path":"mcp/mcp.go","hunks":[...]}

// Read（现在 metadata 为空，需补）
{"type":"read","rel_path":"mcp/mcp.go","lines_count":120}

// Bash 文件操作（parseBashFileOp 已有，补 rel_path）
{"type":"bash","rel_path":"mcp/mcp.go","command":"..."}
```

### 3.3 路径归一化约定

- **统一存相对项目根路径**，正斜杠（`filepath.ToSlash`）
- hook 在写入处做归一化，查询端永远干净
- **项目外文件**（如 `/tmp/fix.py`）→ 返回空字符串，**不写入**（宁缺勿脏）

```
/Users/dazsec/workspace/aipmc/mcp/mcp.go → mcp/mcp.go
/tmp/fix_mcp.py → (跳过，不写入)
```

### 3.4 Store 层函数（查询封装）

```go
// 查询 discussion_log（json_extract(metadata,'$.rel_path') 匹配）
// Phase 0/1 不带 exclude，Phase 2 加参数
func GetEntitySessions(entityIDs []string, since string) ([]ActivitySession, error)
func GetRecentSessions(since string, limit int) ([]ActivitySession, error)

type ActivitySession struct {
    SessionID    string
    Source       string
    LatestEntity string
    LatestType   string
    LastSeen     string
}
```

**注意**：`excludeSessionID` 参数延后到 Phase 2（`guessSelf` 实现后）才加入，避免依赖倒置。

**幽灵 session 过滤（方案 C 修正，8/4 终审第五轮核验）**：

**实测分布**（`session_id='unknown'`）：

| source | unknown 条数 | 内容构成 | IsOrphanSessionID 会误伤？ |
|--------|:---:|------|:---:|
| claude-code | 465 | **100% 📡 MCP**（0 条真实对话/文件操作） | ❌ 不误伤 |
| gemini-cli | 37 | 4 条 👁 真实 Read + 4 条 user 对话 + 15 条 🛠 MCP + 其余工具 | ✅ 误伤 8 条真实活动 |
| aipmc-vision | 32 | **100% 真实对话**（16 user + 16 assistant，0 条 MCP） | ✅ 误伤全部 |
| mcp / claude-code-mcp | 4 + 1 | 真实讨论 / 📡 | ✅ 误伤 4 条讨论 |

**结论（推翻 v1.8 的「双保险」）**：`IsOrphanSessionID` 过滤 **不是独立保险**——
它把 `unknown` 里其他 source 的真实活动一起滤掉（gemini-cli 的 👁 Read、
aipmc-vision 的全部对话）。v1.8 把「过滤 📡」和「过滤 unknown」当成两层保险，
实测二者**不等价且第二层有害**。Claude 结论对（幽灵过滤不能过滤所有 unknown），
但它的论据错（它称 claude-code 的 464 条 unknown 不全是 MCP，实测 465 条 100% 是 MCP）。

**定案过滤（三层，全部只针对 MCP 记录，不碰 unknown 真实活动）**：

1. `content NOT LIKE '📡%'` — 主过滤（MCP 记录前缀，Claude/Codex 均一致，实测）
2. **依赖 `RecentAgentActivity` 已有的 `HAVING users > 0`** — 幽灵 MCP 记录是
   assistant 角色、无 user 消息，天然被排除；真实对话（哪怕 unknown）有 user 消息，
   不会被误伤（这是现有代码 `store/discussion.go:414` 的现成行为）
3. **不引入 `IsOrphanSessionID` 过滤**（v1.8 误加，v1.9 移除）——它对 unknown 的
   真实活动误伤，且与第 2 层职责重叠

**`unknown` 值说明**：`unknown` 是 store 层对空串的归一化
（`store/discussion.go:97`）。`IsOrphanSessionID`（`session/merge_mcp.go:16`）保留给
**MergeOrphans 专用**（孤儿回填判定），不用于查询过滤。

**历史数据回填（定案）**：存量 discussion_log 无 `rel_path` 字段。Phase 0 落地后：
- **不回填**存量记录的 metadata（改历史 JSON 风险大、收益低）
- 同行感知查询**只覆盖 Phase 0 之后**的新记录（`created_at >= 方案 C 上线时间`）
- 冷启动期（1-2 小时）[同行 agent] 段为空是预期行为，验收标准按此设定

---

## 4. Hook 增强

### 4.1 写入点（Phase 0 范围：Claude + Codex；Cursor 纳入但低优先）

**Claude hook**（`hook/hook_claude.go`）：在现有 Write/Edit/Read 的 `LogDiscussion`
写入时，**给 metadata 补充 `rel_path` 字段**（方案 C，见 3.2）：

| 工具 | metadata 补充 |
|------|:---:|
| Write/Edit | 已有 `file_path`，加 `rel_path` |
| Read | **现在 metadata 为空，需补** `{"type":"read","rel_path":...,"lines_count":N}` |
| Bash（文件操作） | parseBashFileOp 已有，补 `rel_path`（Phase 3 前可不做） |

**注意**：MCP 工具调用**不走 hook**——MCP 对 Claude hook 不可见（见 4.2），
由 `mcpLogDiscussion` 在 mcp server 侧记录，且 session_id 传空串（store 层归一化为 `unknown`）。

**注意**：`ProcessClaudeHook` 顶部需补 `pmdb.RuntimeDir()` 调用获取 projectRoot
（setup 函数里已有，但运行时 hook 进程没有）。

**Codex hook**（`hook/hook_codex.go`）**同步纳入 Phase 0**：`extractFileOpMeta`
（hook_codex.go:250，Write/Read/apply_patch/Bash 全覆盖）已产出 `file_path`，同样补
`rel_path`。

**⚠️ Codex 覆盖边界（8/4 终审第四轮，Claude 建议定案）**：实测 Codex metadata
**无结构化 `file_path`**（0 条 key），`extractFileOpMeta` 靠 `parseBashFileOp`
（hook_codex.go:487/567）从 `tool_input.command` 文本现算，**启发式不稳定**（复杂命令
提不出路径，如 `cat X 2>/dev/null || echo`、管道、变量）。方案 C 在 Codex 上的
「补 rel_path」= 先稳定命令解析。**定案边界**：

- ✅ 结构化工具（Write/Read/apply_patch 的 `file_path`/`path`/`filePath` 字段）→ 补 rel_path
- ✅ Bash 中可稳定解析的简单文件操作（`>`、`>>`、`sed -i`、`cat 单文件`）→ 补 rel_path
- ❌ 解析失败/复合命令 → **不写 rel_path**（宁缺勿脏，与项目外文件同规则）
- 查询端对 Codex 记录接受「部分操作无 rel_path」的漏检，验收标准按此设定

**Cursor hook**（`hook/cursor/` 已存在）：metadata 顶层已有 `$.file_path`（Read/Edit/
Search 都覆盖，实测 689 条），补 rel_path **成本最低**。但 Cursor hook **6/29 后停用**
（用户未开），纳入 Phase 0 但**低优先**——重开 Cursor 时自动生效，不阻塞主路径。

**理由**：用户实际同时开 Claude + Codex。若 Phase 0 只做 Claude，`[同行 agent]` 段
只能看到 Claude 的同行，看不到 Codex 的，感知缺一半。Codex hook 纳入 Phase 0 成本低、
价值高；Cursor 因停用降为低优先。

### 4.2 MCP task_id 提取（Phase 3，走 mcp server 而非 hook）

**已推翻的假设（8/4 自审）**：~~Claude hook 能捕获 MCP 工具调用，补 metadata 即可提取
task_id~~。代码事实：MCP 调用对 hook 不可见，由 `mcpLogDiscussion`（mcp.go:2307）记录，
`session_id=""`、`source` 写死 `claude-code`。

**修正后的路径**：在 `mcpLogDiscussion`（mcp.go）内部提取 task_id，写入
discussion_log（content 前缀 `📡`，metadata 含 task_id）。该函数已解析 `args`
（含 `task_id`），成本低。但注意：代码传 `session_id=""`，store 层归一化为
`"unknown"`（实测 claude-code 的 📡 记录 465 条为 `unknown`、507 条经 MergeOrphans
回填了真实 session_id），**多数情况无法记录「哪个 session 看的」**。

**⚠️ 幽灵 session 问题（8/4 Claude 终审）**：MCP 记录的 `session_id=""`（store 层归一化为 `unknown`，实测 465 条）若进入
`GetRecentSessions` 的 session 分组聚合，会形成「unknown」幽灵 session，污染
同行感知展示。

**规避方案（方案 C 修正，8/4 终审第五轮）**：MCP 记录（content 前缀 `📡`）**不进同行
感知查询**（`GetRecentSessions` / `GetEntitySessions` 按 `content NOT LIKE '📡%'` 过滤，
且依赖现有 `RecentAgentActivity` 的 `HAVING users > 0` 排除无 user 消息的幽灵记录；
**不引入 `IsOrphanSessionID` 过滤**——实测它会误伤 unknown 的真实活动，见 3.4）。
只在 Phase 3 单独作为「task 关注度」数据使用（如 `aipm_get_session` 或 task 详情页
展示「最近有 MCP 调用关注了此 task」）。这样幽灵 session 问题绕开。

可提取 task_id 的工具（args 里有 task_id 字段）：
`aipm_get_task` / `aipm_update_task` / `aipm_update_task_status`
`aipm_record_commit` / `aipm_record_commits` / `aipm_append_task_note`

`aipm_create_task` 的 task_id 在返回值里，不在 input 里 — 暂缓。

---

### 4.3 PreToolUse 冲突拦截（Phase 1c，Claude 专属增强，8/4 终审第八轮定案）

**定位**：user prompt 注入解决「知道旁边有谁」（普适），PreToolUse 解决「冲突时强制停」
（Claude 专属）。**平台不对称是能力差异**：Codex 无 PreToolUse 概念、Cursor 自定义
permission 格式、OpenCode 只有事后——deny 拦截只对 Claude Code 生效，Codex 靠
PostToolUse 事后记录 + user prompt 注入兜底。

**机制**（Claude Code 的 PreToolUse hook，需新增注册，当前只注册了
Stop/StopFailure/UserPromptSubmit/PostToolUse，见 `hook_claude.go:284-287`）：

```
agent 要 Edit/Write X.swift
→ PreToolUse hook（工具执行前，stdin 含 tool_input.file_path）
→ 查 discussion_log：其他 session 5 分钟内碰过 X.swift？
→ 查该 session 最后活跃时间（RecentAgentActivity.last_seen）？
→ 双条件同时满足 → deny + reason
→ agent 被拦下，看到 reason，自纠后再决定
```

**⚠️ 唯一传达通道是 deny**：`additionalContext` 对 PreToolUse **未实现**（官方
issue #6965，只对 PostToolUse/UserPromptSubmit/SessionStart 生效）。`allow` 的
reason 不一定给 agent 看。所以提示只能走 `deny + permissionDecisionReason`。

**双条件 deny 定案（8/4 终审第八轮，Claude 修正 Codex 的「session 活跃即拦」过宽）**：

| 条件 | 判定 | 数据源 |
|------|------|--------|
| 1. 文件碰触 | 其他 session **5 分钟内**碰过 X.swift | discussion_log（PostToolUse 写入） |
| 2. session 活跃 | 该 session 最后活跃 **≤5 分钟前**（仍在工作） | `RecentAgentActivity.last_seen`（`store/discussion.go:432`，现成） |

**两个条件 AND 才 deny**——收窄两边的误伤：
- 碰过 X + 仍活跃 → 真冲突风险，拦 ✅
- 碰过 X + 已静默 → 过去时，不拦 ✅
- 活跃 + 没碰过 X → 无关，不拦 ✅

**节流与绕过**：
- **节流**：同一文件每 5 分钟最多 deny 一次（与 sessionCache TTL 一致），防 agent
  重试触发死循环
- **绕过**：reason 明确写「确认无冲突后可继续，或先与对方协调」——deny 是提醒不是死锁
- **保守优先**：宁可漏拦（agent 靠 user prompt 注入看到碰触），不误伤正常编辑
  （打断工作流，用户会关掉功能）

**输出格式**：
```json
{
  "hookSpecificOutput": {
    "hookEventName": "PreToolUse",
    "permissionDecision": "deny",
    "permissionDecisionReason": "⚠️ codex-cli 也在操作 AddContactView.swift（5 分钟内），请先协调或读取 .pmai/peers.md"
  }
}
```

**固有局限**：查的是「历史碰触」（PostToolUse 写入），无法阻止两个 agent 同一瞬间改
同一文件——所有方案共同的限制，非 PreToolUse 特有。
### 4.4 Git 操作排他锁（借鉴 agentlocks，解决 index.lock 竞态）

**定位**：今天的 EncryptDrive 验证暴露——真实冲突**全部走 Bash hook**（`git commit`/`git stash`/`git reset`），而非 Edit/Write 工具。PreToolUse deny 对这些 git 操作**完全不可见**（只拦 Edit/Write）。

**社区方案**：[agentlocks](https://github.com/simke9445/agentlocks) 专为第三方 agent（Claude Code / Codex harness）设计，合成 `@git/index` 锁——`git begin` 获取租约、`git end` 释放，纯文件锁 + TTL 租约 + prune。

**我们的采纳**（v1.12 定案）：

| 维度 | 设计 |
|------|------|
| 实现位置 | **hook 层 Bash 命令拦截**，不解析命令行文本——检测到 `git commit/stash/reset` 时先取锁，拿到再放行，拿不到提示「对方正在 git 操作，等待 5 秒」。不用独立 wrapper（`aipmc git commit`）踩 5% 主动使用率死穴 |
| 读写分离 | `git commit/stash/reset`（改 index/refs）→ 取锁；`git status/log/diff`（只读）→ 永远放行。**`git checkout` 不进锁表**——切换分支在共享单工作区是结构性冲突（切换整个文件树），不是锁能解决的，靠「同工作区禁 checkout」纪律 |
| 锁粒度 | 进程级文件锁，TTL **120s + 自动续期**（每 10s 续一次，commit 结束后释放）。**v1.12 修正**：原 30s 在 xcstrings 假 diff（8000+ 行）场景下会误伤长时间 commit；`prune` 清除过期死锁 |
| 首次 commit 处理 | `HEAD~1..HEAD` 在首次 commit 时 `HEAD~1` 不存在 → 回退到 `git diff --cached`（首次）或 `git diff --name-only $(git merge-base HEAD main)..HEAD`（累积 diff 对比 task base）。**v1.12 修正**：原方案 `HEAD~1..HEAD` 漏首次 commit |
| 与 PreToolUse deny 的关系 | 互补——deny 拦 Edit/Write 文件操作，git 锁拦 Bash 层 git 写操作。互不替代 |
| 提示语义 | **只进注入告警（软提示）**，**不进 deny（硬拦截）**——Bash 命令非精确工具调用，误判代价高。注入层："⚠️ codex 正在提交代码，建议等待"; deny 层保持工具级精确（只拦 Edit/Write） |

**实施**：Phase 1c 附带（与 PreToolUse deny 同期）。hook 的 Bash 处理分支加 `acquireGitLock` / `releaseGitLock`，TTL prune 在 PostToolUse 记录阶段执行。

---

### 4.5 文件域隔离：scope.json 机制

**定位**：把「任务分配时划文件域」（人的纪律）升级为**机械化校验**——不再依赖 agent 自觉。

**设计**（借鉴社区 `@ruah-dev/orch` 的 `scope.json` + 社区 `scope-policy.json`）：

```json
// .pmai/scope.json — 由 task 创建时自动写入，或用户手动维护
{
  "tasks": {
    "task-contact-alignment": {
      "plan": "通讯录页对齐 Android",
      "roots": ["EncryptDrive/Features/Contacts/", "EncryptDrive/Shared/Storage/PalStore.swift"],
      "exceptions": []  // 显式允许「既不在 roots 也不在 roots 子目录的越界文件」
    },
    "task-backup-compat": {
      "plan": "备份双向兼容实现",
      "roots": ["EncryptDrive/Shared/Storage/Backup*.swift", "EncryptDrive/Features/Main/SettingsView.swift"],
      "exceptions": ["EncryptDrive/Resources/Localizable.xcstrings"]
    }
  },
  "default_policy": "warn"  // warn | block | off
}
```

**校验时机**（v1.12 修正——社区用 merge 时校验，我们用 commit 时校验）：

| 社区 | 我们 | 原因 |
|------|------|------|
| merge 回主干时检查 `--diff-filter` | **commit 时**检查 `git diff --name-only` | 我们的 agent 直接 commit 到 main，不存在 merge 分支 |

**校验逻辑**（hook 层 PostToolUse 阶段）：

```
agent 执行 git commit
→ PostToolUse hook 触发
→ 读 .pmai/scope.json，找到该 session 对应的 task 的 roots + exceptions
→ git diff --name-only HEAD~1..HEAD 获取当前 commit 的变更文件
→ 对于不在 roots 目录树且不在 exceptions 列表的文件：
   - policy=warn → 注入提示“⚠️ X.swift 超出文件域，建议确认是否属于本 task”
   - policy=block → deny commit（强拦截，需用户配置才启用）
```

**与 task 元数据打通**：aipm 创建 task 时若指定文件域（新增字段），自动写入 scope.json，形成 `任务分配（人）→ PM 记录（aipm）→ commit 校验（hook）` 的完整闭环。

**粒度决策**：目录为主 + 例外清单（混合）。纯目录太粗（跨目录重构漏检），纯文件列表太脆（维护成本高）。例外清单解决共用文件（如 `Localizable.xcstrings`）被多 task 声明的场景。

**被动生效与主动拦截的区别**（v1.12 补充）：
- `policy=warn`（默认）：**对双方 agent 均生效**——PostToolUse hook 双方均有，commit 时输出越界 warn 提示。不绑定 task（降级为「全量越界检测」），也能拦截「完全没有声明的文件改动」
- `policy=block`：**Claude 专属**——需要 PreToolUse 拒绝 commit，Codex 无此能力

**文件级运行时锁与域边界的区分**（v1.12 补充）：
scope.json 是**域边界**（声明性的——"这个 task 只该碰 Contacts/"），与**文件级运行时锁**（操作性的——"session X 现在在改 Y.swift"）是互补关系，不是替代关系：

| | scope.json（域边界） | file-level lock（运行时锁） |
|---|------|------|
| 用途 | 声明 task 的文件域，越界即 warn | 标记当前文件正在被谁操作 |
| 粒度 | 目录 + 例外清单 | 单个文件 |
| 维护者 | 人（task 创建时） | hook 自动维护 |
| 借鉴 | 社区 scope-policy.json | agentlocks 文件锁 |

两者各司其职——域边界是 guardrail，运行时锁是 turn signal。不建议用一个替代另一个。

**实施**：Phase 1c 附带（与 PreToolUse deny + git 锁同期）。`hook/hook_claude.go` 的 PostToolUse 阶段扩充分支。

---

### 4.6 编译隔离：worktree 的定位修正

**v1.11→v1.12 认知修正**：

| | v1.11 定位 | v1.12 修正 |
|------|-----------|-----------|
| worktree 核心价值 | 隔离「文件冲突」 | 隔离「**编译环境**」——文件冲突已被文件域 + 感知注入覆盖 |
| 触发信号 | 未定义 | **编译互踩频率**（非 git 竞态——git 竞态随 wrapper 锁 + 域隔离自然消失） |
| 适用 agent | 全 agent | **Claude 专属**（`claude --worktree` 原生支持；codex/cursor 无等价能力） |

**worktree 门槛（硬条件，防占位）**：

> 连续 **2 个 sprint** 内，编译互踩 ≥ **3 次**（即一个 agent 的半成品导致另一个 agent 无法编译）→ 评估为 Claude 开 worktree。
> **加速条件**（v1.12 补充）：**同一文件**发生第 **2 次**编译互踩时立即触发（不等 3 次）——同文件反复互踩是最痛的场景，应加速响应。
> 不满足则不启动——避免成为「始终在计划里、永远不会执行」的占位项。

**试点路径**：
- **Phase 1b 后（aipmc 项目）**：验证 `RuntimeDir()` 跨 worktree 向上遍历能找到 `.pmai`（纯技术验证，成本低）
- **EncryptDrive（感知层上线后）**：以编译互踩频率为触发，验证「worktree 降低 agent 编译阻塞」

**Claude 原生支持**：`claude --worktree feature-auth` 一个 flag 创建隔离工作区。共享 `.git` 对象库使合并/换基廉价。**注意**：`claude --worktree` 只对 Claude 生效，codex/cursor 需另寻路径——worktree 不能成为全 agent 统一架构。

**编译互踩与 worktree 的关系**（v1.12 明确定义）：
- Xcode / 单二进制 Go 项目：全项目 BUILD ALL，一个文件报错阻塞所有 agent → worktree 是**唯一物理解**
- 感知注入的 op 字段（「Edit，未提交」）能让对方**知道需要等**，但不知道**等多久**（未提交状态可能持续半小时）
- 域隔离 / git wrapper 锁 / scope.json 都解决**文件/索引冲突**，不解决编译互踩

---



---

## 5. INJECT 设计

### 5.1 extractFilePaths 补 tool_use（Phase 1 前置）

现有实现只解析 `text` block（`context_inject.go:457-476`）。补 `tool_use` 分支：

- `Write` / `Edit` / `Read` 的 `input.file_path` / `input.filePath`
- 目的是抓"正在修改的文件"（text 里不一定回显路径）

**cwd 临时方案（阻塞 Phase 1，必须随 Phase 1 做）**：

`extractPaths`（`context_inject.go:518-519`）用 `cwdPrefix` 把绝对路径转相对。若 proxy
不在项目根启动，转换失败，路径保留绝对形式，和 discussion_log 里的 `rel_path` 对不上，
`[同行 agent]` 段查不到任何东西。

**代码事实（已验证）**：

- **serve 模式**（`aipmc serve` → `serveCommand()` main.go:347 → 379 行 `os.Chdir(projectPath)`
  → 同进程启动 proxy）→ cwd = 项目根 ✅
- **独立 proxy 模式**（`aipmc proxy` → main.go:210-229 → 直接 `proxy.Run(...)`，
  **无 Chdir**）→ cwd = 用户启动 proxy 时的目录 ❌（README 明确说「Proxy must be
  running: aipmc proxy」，独立 proxy 是官方用法）
- `proxy.Run` 内部无 Chdir（proxy 目录全量 grep 只有 context_inject.go:507 的
  `os.Getwd()` 和 :580 的 `pmdb.RuntimeDir()`）
- 但 `proxy.Options` 没有 projectPath 字段 → 显式透传需要跨 5 层签名改动
  （Options → NewHandler → InjectSessionContext → resolveFileContext →
  extractFilePaths → extractPaths）

**临时方案（修正，8/4 终审第二轮）**：**用 `pmdb.RuntimeDir()` 定位项目根**，
替代「缓存 os.Getwd()」。

理由：
- `RuntimeDir()`（db/db.go:47）从 cwd **向上遍历**找 `.pmai`，**两种启动模式都成立**
- `context_inject.go:580` 的 `loadGuidelines` 已经在用它定位 guidelines.md，机制已验证
- 「缓存 os.Getwd()」在独立 proxy 模式下失效（cwd 非项目根，缓存的是错误值）——已否决

实现：proxy 初始化时调用 `RuntimeDir()` 得到 `.pmai` 目录，取 `filepath.Dir()`
为项目根，替代 `extractPaths` 内部的 `os.Getwd()`。显式透传（Options 加字段）
作为长期方案记录，不阻塞 Phase 1。

### 5.2 时间桶化

peer 段的时间显示必须桶化，保证 INJECT hash 稳定（不每请求触发注入）：

```
<10min → "<10min"
10-30min → "10-30min"
30-60min → "30-60min"
60-120min → "1-2h"
>120min → ">2h"
```

**注意**：时间桶化只稳定"时间维度"。peer 数据必须进 `sessionCache`（5 分钟 TTL）才能稳定"内容维度"
（当前 session 自己的 activity 变化不会每请求触发重新注入）。

### 5.3 条件段设计

| 段 | 预算 | 出现条件 |
|----|------|---------|
| `[项目编码规范]` | 600（独立预算） | 恒定 |
| `[文件关联]` | 200（豁免） | 恒定 |
| **`[文件碰触]`** | 100 | **有文件重叠时（第一优先）** |
| `[同行 agent]`（折叠计数） | **50** | 有活跃同行时 |
| `⚠️ 待处理` | ~150 | 事件存在时 |
| `最近的 session` | 剩余 | 有空间时 |

**关键**：`[文件碰触]` 和 `[同行 agent]` 是条件段，平时零占用，不污染上下文。

**优先级定案（8/4 终审第六轮，EncryptDrive 实测）**：**`[文件碰触]` 第一优先**，
`[同行 agent]` 降为折叠计数（`2 agents active` 一行，不列详情）。理由见 5.3.2。

**active 窗口（v1.12 修正：两窗口语义分离）**：

- **折叠计数 active 窗口**：**30 分钟**（感知端——"知道有人在旁边"，宽窗口避免刚离开的 agent 立刻消失）
- **碰触展开 + PreToolUse deny 窗口**：**统一 5 分钟**（判定端——"是否仍在冲突中"，窄窗口精确判定；决策 9 与 26 合并为单一常量 `COLLISION_WINDOW`）

> v1.11 的「折叠计数和碰触判定共用一个窗口 30 分钟」与 deny 的 5 分钟冲突——现已定案分离为两个独立参数，语义清晰
（`created_at >= now-30min` 内有过活动的 session 算 active），与 guessSelf 时间窗口
共用同一参数（§6.3 Phase 2 实测调参）。窗口太长为挂机 session 虚报 active，太短漏掉
刚活跃的——30min 是事件 A 场景（stash 发生在对方改动后几分钟）的安全覆盖值。

### 5.3.1 预算重构（纳入 Phase 1a，不可后置）

实测：guidelines 满 600 字符时，现有代码就把 `[最近的 session]`（goals）和
`⚠️ 待处理`（actions）挤到零（`warnings=1/1, actions=0/1, goals=0/3, suppressed=4`）。

这不是「peer 段上线后才有」的新问题——**现有代码就是这个行为**。Phase 1 加
`[同行 agent]` 段后，在完整 guidelines 的项目里会进一步挤压。

**决策**：预算模型重构纳入 Phase 1a（独立验收），与 `[同行 agent]` 段（Phase 1c）分开。

**分配机制（具体算法，需实现）**：

```
现有 buildContextBlock（context_inject.go:241-319）是「逐行顺序写 + 超限跳过」
分层重构：
1. 收集阶段：先把所有段的条目收集到内存（guidelines / fileAssoc / warnings /
   actions / goals / peer）
2. 排序阶段：按优先级写入（guidelines > fileAssoc > warnings > actions > goals > peer）
3. 总量控制：800 字符上限，高优先级写满为止，低优先级降级
```

**⚠️ 分层解决的边界**：分层只保证「相对优先级」（warnings 优先于 goals），
**不解决「总量不够」**。guidelines=600 时 warnings 先填满，actions/goals 仍可能为 0。
这是总量约束的物理限制，不是 bug。Peer 段作为最低优先级，最坏情况下被截断
（条件段性质保证它平时不出现，出现时被截断可接受）。

### 5.3.2 真实场景验证（EncryptDrive 3 agent，8/4 终审第六轮）

用真实工作流（EncryptDrive，claude-code + codex-cli 并行）逐条验证方案：

**实测事实**：

1. **并行冲突真实发生**：claude-code（USB/蓝牙修复 + 资产移动/粘贴审查）与
   codex-cli（扫码卡顿 + paste 功能）在 **paste 功能区域交叉**——claude 加
   `PasteOperation` setter，codex 同时改成公开 API；claude 明确提示
   「codex 还在动同一个文件,建议尽早提交避免冲突」
2. **stash 事件的本质**：claude 为编译通过 stash 了 codex「未完成 + 带编译错误」
   的改动——冲突不只是文件重叠，还有**状态冲突**（对方改动未完成/编译坏）
3. **用户手动隔离**（「你不需要关注别的session的问题」）：用户要隔离的是
   「其他 session 的问题细节」，不是「知道旁边有谁」——claude 知道 codex
   在动同一文件但仍在专注自己的 USB 修复

**方案验证结论**：

- ✅ **文件碰触第一优先成立**：paste 区真实交叉，若 claude 动这些文件时注入
  「⚠️ codex-cli 也在操作 X.swift」，就不会盲目加 setter、也不会等 stash 后才
  发现冲突
- ✅ **折叠计数不违反隔离**：claude 已证明「知道 codex 在旁边」不分散注意力；
  用户要隔离的是细节不是存在感。折叠计数（`2 agents active`）正好满足
  「知道存在、不给细节」
- ✅ **被动 INJECT 优于主动查询**：claude 知道 codex 在动同一文件，靠的是用户
  转述/自己去读讨论——不稳定、不及时；方案改为请求时被动注入是系统化
- ✅ 单 agent 降级空段：codex 大部分时间在独立文件（AddContactView.swift），
  claude 在 USB/资产区，无重叠时零注入——条件段平时零占用成立

**⚠️ 两级方案时序分工（8/4 终审第七轮补，Claude 严格审核）**：
**折叠计数管「预先感知」，碰触告警管「冲突确认」**——`extractFilePaths(body)` 只能
拿到当前请求提到的文件，碰触判定发生在请求时，所以碰触告警本质是「事后」（agent
已经在碰 A 才看到告警）；折叠计数提供「预先知道有人」的第一层。**实施时不能只做
碰触段不做折叠段**，否则落回「冲突发生才知道」的坑（事件 A：claude 先 stash 才发现）。

**暴露的缺口（L1 增强项，8/4 终审第七轮定位修正）**：文件碰触告警只告诉「谁碰了」，
给不了「对方的改动处于未完成/编译错误状态」——claude stash 是为了编译。**这是 L1
碰触告警的信息完整度问题，不是 L2/L3 新能力**（Claude 严格审核修正：stash 的决策
依据是编译坏，告警不含它则 agent 无法判断「该不该动」）。增强方向：碰触告警附带
对方「最后操作类型 + 编译状态」。技术上后置（hook 需记录编译结果），不阻塞 Phase 0。

### 5.4 peer 缓存（解决 hash 漂移）

- peer 段数据预渲染，放进 `sessionCache`（5 分钟 TTL）
- **缓存条件不能依赖 `len(sessionCache.goals) > 0`**（goals 空时缓存失效）
- peer 缓存独立于 goals 缓存，各自 TTL

### 5.5 注入位置与提示词（定稿，8/4 终审第八轮重写）

**注入位置：user prompt 末尾，不是 system prompt**（缓存根治——最新 user message
每轮本就是新增的，不在缓存前缀里，INJECT 加其末尾**不改变任何已缓存内容**）。

| agent | 请求格式 | 注入点 |
|-------|---------|--------|
| claude | `/v1/messages` | **最后一条 role=user 的 message content 末尾追加**；无 user message 时跳过（不注入） |
| codex | `/v1/responses` | 最后一个 user role 消息 content 末尾追加 |
| gemini | `:generateContent` | `contents[]` 最后一条 user 的 parts 末尾追加 |
| cursor/opencode | `/chat/completions` | 最后一条 role=user 的 message content 末尾追加 |

**实现**：`injectAnthropic`（`context_inject.go:628`）从「prepend system」改为「末尾 user
message content 追加」；四分支统一在 `injectIntoPrompt` 分派处处理。

**提示词模板**（同行段折叠计数 + 文件碰触展开两级）：

```
[同行 agent]
2 agents active   ← 折叠计数（50 预算，集合驱动，不列详情）

[文件碰触]
⚠️ codex-cli [d4e5f6] 也在操作 AddContactView.swift  <5min   ← 碰触时展开（100 预算）
```

**设计要点**：
- **缓存**：user prompt 不在缓存前缀 → INJECT 内容变化**完全不影响命中**，90%+ 达成
  （对比 system 注入：任何变化都 miss 整块）
- **每轮注入的成本**：user message 每轮必新增，INJECT 无 content-hash 去重可跳——
  每轮带一次折叠计数（50 字符），token 成本可忽略；且「每轮都看到同行状态」正是所需
- **顺序自然**：agent 先看用户的话，再看 INJECT 提示——「用户要改 X，旁边 codex
  也在改 X」相关性最强
- 不加引导语：现有段均无引导语，标题即语义
- **Phase 1 注入含当前 session 自己**（未决问题 2），Phase 2 guessSelf 后排除（见 §6）

**⚠️ 缓存影响分析（8/4 终审第六→八轮收敛，最终定案）**：

| 方案 | 缓存影响 | 结论 |
|------|---------|------|
| system prompt 注入 + 时间桶 | INJECT 变 → 缓存前缀变 → 每 10min miss 一次 | ❌ 90% 要求下不达标 |
| system 注入 + 集合驱动（去时间桶） | session 集合变才 miss（新 agent 加入/离开仍 miss） | ⚠️ 改善但非根治 |
| 固定 INJECT + peers.md 拉取 | 永不 miss | ⚠️ 踩 5% 主动使用率死穴 |
| **user prompt 末尾注入** | **INJECT 变不影响已缓存前缀（最新 user message 每轮本来就在变）** | ✅ **根治**，采用 |

**验证要点**：Anthropic 前缀 = system + 断点前 messages；最新 user message 在该
前缀之外。INJECT 加其末尾，已缓存部分字节级不变。

---

## 6. 身份识别：guessSelf（Phase 2）

### 6.1 启发式

```
请求体 → extractFilePaths → 文件集 F
discussion_log（方案 C，metadata.rel_path）→ 每个活跃 session 最近碰过的文件集 Fi

候选当前 session = (source 相同 + Jaccard(F, Fi) 最高 + 最近活跃)
```

### 6.2 失败模式即正确行为

| 场景 | 启发式行为 | 对错 |
|------|-----------|------|
| 各改各的文件 | 不重叠 → 高置信度识别自己 | ✅ |
| 改同一文件（冲突） | 重叠 → 无法区分 → 全部展示 | ✅ 恰好需要 |
| 纯讨论无文件 | 无文件 → 全部展示 | ✅ 降级安全 |

### 6.3 时间窗口

**默认 30 分钟**（与折叠计数 active 窗口共用，见 5.3；碰触窗口 5 分钟不适用 guessSelf），Phase 2 用实测数据调参
（30min / 60min 候选）。

---

## 7. 分阶段实施（每阶段独立可用）

| Phase | 内容 | 验收标准 | 适用 agent |
|-------|------|---------|:---:|
| **0** | 方案 C：hook 写入时补 `rel_path`（含 Read metadata）+ discussion_log 查询函数封装 | 查询「谁最近碰了 X 文件」返回 session 列表；Claude 的 Edit/Read、Codex 可解析操作都有 `rel_path`；Codex 复合命令无 `rel_path` 为预期（覆盖边界）；Cursor 低优先 | 全部 |
| **1a** | 预算重构（分层收集 + 优先级排序 + 总量降级） | guidelines 满 600 时 warnings 优先，actions/goals 不被低优先级挤掉 | 全部 |
| **1b** | cwd 临时方案（proxy 用 RuntimeDir() 定位项目根）+ extractFilePaths 补 tool_use + **GIT_GC_AUTO=0** | 路径转换正确，与 discussion_log 归一化后的相对路径对得上；serve 和独立 proxy 两种模式都成立；git gc 被禁用防 worktree 并发损坏 | 全部 |
| **1c** | user prompt 末尾注入（同行段 + 文件碰触段 + op 状态）+ PreToolUse 双条件 deny（Claude 专属）+ **git wrapper 锁**（agentlocks 模式）+ **scope.json 域校验机制** | 两 agent 同跑，各自请求末尾看到对方（含"Edit，未提交"状态）；缓存命中率 90%+；Claude 编辑冲突文件时被 deny；`git commit` 等写操作先取锁防 index.lock；commit 越界文件被 scope.json warn | 注入/锁/scope-warn 全部；deny Claude 专属；scope-block Claude 专属 |
| **2** | guessSelf + `[文件碰触]` 段 | 改同一文件双方看到告警；各改各的只看到同行 | 全部 |
| **3** | mcpLogDiscussion 内提取 task_id → discussion_log（content 前缀 `📡`，**不进同行感知**） | task-001 能查到最近有 MCP 调用关注它；同行感知里无「unknown」幽灵 session | 全部 |
| **4** | `aipm_get_session` MCP 工具 + INJECT 引导钩子 | agent 可深挖指定 session 的完整上下文 | 全部 |

**说明**：Phase 1 拆为 1a/1b/1c，各自独立验收。理由：预算重构和 cwd 修复是
**基础设施修复**（影响现有 warnings/actions/goals 展示），混进同行段（功能）会导致
验收失败时无法归因。1a/1b 先单独验证，再上 1c 功能。

**冷启动预期**：方案 C 依赖 discussion_log 的 `rel_path` 字段（Phase 0 开始写入），
历史记录无 `rel_path` 需回填或忽略。Phase 1 的 `[同行 agent]` 段在 Phase 0 落地后
积累 1-2 小时数据才会出现。这不是 bug，验收标准按此设定。

**⚠️ MergeOrphans 窗口边界（8/4 终审第五轮核验）**：`MergeOrphans`
（`session/auto.go` → `runOnce`）只在 **24h 窗口**内把 `📡 aipm_%` 孤儿回填真实
session_id（`store/session.go:117` 的 `ListOrphanMCPRows`）。实测 claude-code 的
465 条 unknown：6 月 365 条 + 7 月 95 条 + 8 月 5 条——**6/7 月那批已过期，永远不会
被回填**，会永久停留在 `unknown`。因此幽灵过滤**不能依赖 MergeOrphans 的回填**，
必须靠查询端过滤（见 3.4）。

---

## 8. 远期愿景（本次不实施）

### 分层路线

| 层级 | 名称 | 含义 | 用户角色 |
|------|------|------|---------|
| L1 | 感知 | agent 知道谁在做什么、有没有冲突 | 无感 |
| L2 | 异步通信 | A 的发现自动进入 B 的上下文 | 触发 B，消息自动传递 |
| L3 | 事件驱动响应 | A 完成/失败 → 系统主动唤醒 B | 设规则 |
| L4 | 自主协商 | agent 讨论、分工、互相 review | 只裁决 |

### L2 方向（已修正）

**实体绑定标注（annotation），不做路由（to=agent B）。**

```
agent A 发现 "NoteType 缺 pinned 字段"
→ 标注在 NoteType.swift 上（谁碰这个文件谁看到）
→ 标注入口: 用户 CLI（aipmc annotate），不依赖 agent 主动调用
```

理由：
- 7/31 已否路由（精度不可控），今天保持一致性
- 标注不依赖 MCP 使用率，用户即可写入
- 消息"从哪来"的问题通过用户入口绕开

**数据模型**：L2 用**独立 `annotations` 表**，不复用 discussion_log。

理由：
- 标注是**持久存在**（用户 5 天前的标注还要在），discussion_log 是**滚动活动日志**（会被清理/归档）
- Phase 3 的 MCP task_id 记录是「有调用看了 task」的活动记录，与「文件有个人注」语义不同
- 复用会混淆两种生命周期

### L1 增强项：文件碰触带状态维度（EncryptDrive 实测暴露，8/4 终审第六轮 + 第七轮定位修正）

**文件碰触告警缺「状态维度」**：EncryptDrive 事件 A 中 claude stash codex 的改动，
为的是编译（对方改动未完成 + 编译错误）。当前 L1 文件碰触只告诉「谁碰了」，给不了
「碰的状态」——agent 无法判断「该不该动」。

**这是 L1 碰触告警的信息完整度问题，不是 L2/L3 新能力**（Claude 严格审核修正，决策 25）。

**v1.12 定案**：**op 字段提前到 Phase 1c 附带**——hook 已有操作类型（Edit/Read/Bash），在 metadata 记 `"op":"edit"` 零额外成本。注入端碰触告警从：
```
⚠️ codex-cli 也在操作 FilesDecryptTab.swift <5min
```
变为：
```
⚠️ codex-cli 也在操作 FilesDecryptTab.swift（Edit，未提交）<5min
```
多了「未提交」三字，agent 就能判断是否该等待。编译状态（编译成功/失败）仍后置 Phase 3（需 hook 外信息）。

### L3 的天堑

agent 是被动进程，没有"自己醒来"的能力。需要：

- 常驻后台循环（headless worker），或
- 系统在 Stop 事件时伪造请求发给其他 agent（有风险：对方可能正忙）

这是产品形态决策，不是技术细节。

---

## 9. 已确认决策（Decision Log）

| # | 决策 | 状态 |
|---|------|------|
| 1 | **方案 C**：不建表，hook 写入时在 metadata 补 `rel_path`，查询端 json_extract 精确匹配 | ✅ 共识（8/4 Claude 终审定案，第三轮改 json_extract） |
| 2 | 项目外文件不写入（宁缺勿脏） | ✅ 共识 |
| 3 | peer 数据进 sessionCache，缓存独立于 goals | ✅ 共识 |
| 4 | excludeSessionID 延后到 Phase 2 | ✅ 共识 |
| 5 | ~~MCP 判断用 mcp__ 前缀（hook 侧）~~ → MCP 调用对 hook 不可见，改在 mcpLogDiscussion 内提取 | ✅ 共识（8/4 自审修正） |
| 6 | L2 用实体绑定标注，不用路由 | ✅ 共识（8/4 修正） |
| 7 | `[同行 agent]`/`[文件碰触]` 为条件段，平时零占用 | ✅ 共识 |
| 8 | 预算模型重构（guidelines 满 600 时 actions/goals 被挤掉） | ✅ 纳入 Phase 1（8/4 Claude 审核修正） |
| 9a | **active 窗口分离**：折叠计数 30min（感知）+ 碰触展开 / PreToolUse deny 统一 5min（判定）→ 常量 `COLLISION_WINDOW` | ✅ 共识（8/5 v1.12——合并决策 9 与 26 的窗口冲突） |
| 9 | ~~guessSelf 时间窗口~~ → 见 9a | 已被 9a 替代 |
| 25 | **op 字段提前 Phase 1c**：hook metadata 记 `op` 字段零成本，告警变「Edit，未提交」 | ✅ 共识（8/5 v1.12——从后置提升；编译状态仍后置 Phase 3） |
| 10 | cwd 依赖（proxy 非项目根启动时路径转换失败） | ✅ 临时方案纳入 Phase 1：proxy 启动时记录项目根传给 extractPaths（8/4 Claude 审核修正） |
| 10a | cwd 临时方案（修正）：用 RuntimeDir() 定位项目根，替代「缓存 os.Getwd()」（后者在独立 proxy 模式失效） | ✅ 共识（8/4 终审第二轮修正） |
| 11 | Codex hook 文件操作 metadata 完整（供 discussion_log 查询） | ✅ 纳入 Phase 0（8/4 Claude 审核修正） |
| 12 | L2 标注用独立 `annotations` 表 | ✅ 共识（8/4 Claude 审核修正） |
| 13 | Phase 1 拆为 1a（预算）/1b（cwd）/1c（同行段），各自独立验收 | ✅ 共识（8/4 终审修正） |
| 14 | 幽灵 session 规避：MCP 记录（content 前缀 `📡`）不进同行感知查询，仅作 task 关注度数据；实测幽灵值为 `unknown`（store 归一化，非空串） | ✅ 共识（8/4 Claude 终审 + 8/4 四轮数据核验） |
| 21 | **幽灵过滤不用 `IsOrphanSessionID`**：实测它会误伤 unknown 的真实活动（gemini-cli 4 条 👁 + aipmc-vision 32 条对话）；只过滤 `📡` 前缀 + 依赖 `HAVING users>0`（现有代码天然排除无 user 消息的幽灵） | ✅ 共识（8/4 终审第五轮，Claude 结论对/论据错） |
| 22 | **MergeOrphans 不能作为幽灵过滤依赖**：只回填 24h 窗口内 `📡 aipm_%` 孤儿，历史 unknown（6 月 365 条）永久滞留 | ✅ 共识（8/4 终审第五轮核验） |
| 15 | Read 记录补 metadata（现在为空，否则 Read 无法参与同行感知） | ✅ 共识（8/4 方案 C 定案） |
| 16 | 查询用 `json_extract(metadata,'$.rel_path')` 替代 LIKE（解决转义 + 格式耦合） | ✅ 共识（8/4 终审第三轮，实测 12650 条 metadata 100% valid JSON） |
| 17 | 历史数据不回填 rel_path，同行感知只覆盖 Phase 0 后新记录 | ✅ 共识（8/4 终审第三轮） |
| 18 | **三 hook metadata 异构实测**：Claude Edit 有/Read 无；Cursor 全有（403 Read 全含 file_path）；Codex 无结构化 file_path | ✅ 共识（8/4 终审第四轮实测） |
| 19 | **Codex rel_path 覆盖边界**：结构化工具 + 可稳定解析的简单 Bash 操作补 rel_path；复合命令不写（宁缺勿脏）；验收按漏检接受 | ✅ 共识（8/4 终审第四轮，Claude 建议定案） |
| 20 | Cursor hook 纳入 Phase 0 但低优先（6/29 后停用；metadata 已有 file_path，补 rel_path 成本最低） | ✅ 共识（8/4 终审第四轮） |
| 23 | **缓存根治 = user prompt 末尾注入**（8/4 终审第八轮推翻第六轮定案）：system 注入任何变化都 miss 缓存块；user prompt 是每轮新增、不在缓存前缀，INJECT 变**不影响命中**（90%+ 达成）。无累积膨胀（proxy 透明中间人，`emit_claude.go:247`） | ✅ 共识（8/4 终审第六轮发现 + 第八轮定案） |
| 24 | **同行段折叠计数 + 文件碰触展开两级**：默认 `2 agents active` 一行（50 预算），碰触时展开详情（100 预算）；文件碰触第一优先 | ✅ 共识（8/4 终审第六轮，EncryptDrive 实测） |
| 26 | **PreToolUse 双条件 deny**（Phase 1c，Claude 专属）：`5 分钟内碰过 X AND session 仍活跃` 才 deny + reason；节流 5min/文件、reason 含绕过说明；宁漏勿误（漏拦由 user prompt 注入兜底）。`additionalContext` 对 PreToolUse 未实现（issue #6965），deny 是唯一传达通道 | ✅ 共识（8/4 终审第八轮，Claude 修正「活跃即拦」过宽） |
| 28 | **git wrapper 锁**（Phase 1c，借鉴 agentlocks）：hook 层 Bash 命令拦截检测 `git commit/stash/reset/checkout` 先取锁，读写分离（读永远放行），TTL 租约；提示只进注入不进 deny（Bash 误判代价高） | 共识（8/5 v1.12——替代 `isGitMutatingOp` 命令解析） |
| 29 | **scope.json 域校验**（Phase 1c）：目录为主 + 例外清单；commit 时校验（非 merge 时——agent 直接 commit 到 main）；与 task 元数据打通，policy=warn（默认）/ block | 共识（8/5 v1.12） |
| 30 | **worktree 定位修正**：核心价值从「隔离文件冲突」修正为「隔离编译环境」；门槛只看编译互踩频率（>=3 次/2 sprint）；Claude 专属（`claude --worktree` 原生） | 共识（8/5 v1.12） |
| 31 | **GIT_GC_AUTO=0**（Phase 1b 附带）：防止 worktree 并发时 `git gc` 损坏共享对象库（借鉴 auto-worktree #174） | 共识（8/5 v1.12） |
| 32 | **scope.json 粒度**：目录为主 + 例外清单（混合） | 共识（8/5 v1.12） |
| 27 | **三层注入分工**：user prompt 注入（普适底座）+ PreToolUse deny（Claude 专属）+ PostToolUse 记录（现有，Codex 兜底）；平台不对称是能力差异非方案缺陷 | ✅ 共识（8/4 终审第八轮定形） |
| 33 | **机制生效边界**（v1.12 §2.4）：感知注入普适、git 锁通过 index.lock 被动生效（双方）、scope-warn 双方 PostToolUse 均生效、PreToolUse deny + scope-block + worktree Claude 专属。不追求全 agent 统一机制，接受架构不对称 | ✅ 共识（8/5 Claude/Codex 三轮 challenge 收敛） |
| 34 | **git 锁 checkout 移除**：`git checkout` 切换整个文件树在共享单工作区是结构性冲突，锁不能解决，从锁表移除 + 加「同工作区禁 checkout」纪律 | ✅ 共识（8/5 v1.12——Claude challenge #1 + Codex 接受） |
| 35 | **git 锁 TTL 120s + 自动续期**：原 30s 在 xcstrings 假 diff（8000+ 行）场景误伤长时间 commit；改为 120s + 每 10s 续期 + 结束释放 | ✅ 共识（8/5 v1.12——Claude challenge #2 + Codex 接受） |
| 36 | **首次 commit 回退到 `git diff --cached`**：`HEAD~1..HEAD` 在首次 commit 时 `HEAD~1` 不存在 → 回退累积 diff 方案 | ✅ 共识（8/5 v1.12——Claude challenge #3 + Codex 接受） |
| 37 | **worktree 同文件加速**：同文件第 2 次编译互踩即触发 worktree 评估（不等 3 次全局门槛） | ✅ 共识（8/5 v1.12——Claude challenge #5 + Codex 接受） |
| 38 | **scope.json + 文件级运行时锁互补**：scope.json = 域边界（声明性，人维护），文件级锁 = 运行时协调（操作性，hook 维护）。两者各司其职，不用一个替代另一个 | ✅ 共识（8/5 v1.12——Claude 收网轮补充） |
| 39 | **scope.json 被动生效**：warn 模式不依赖 task 绑定（降级为全量越界检测），PostToolUse 双方 hook 均生效；block 模式 Claude 专属（需 PreToolUse） | ✅ 共识（8/5 v1.12——Codex challenge #6 回应 + Claude 接受被动生效定位） |

---

## 10. 未决问题

1. `aipm_get_session` 的返回结构（activity + user_prompts + L2 goal 的组合）
2. `[同行 agent]` 段是否排除当前 session（Phase 1 不排除，Phase 2 再看）

**已解决（8/4 Claude 审核）**：

- ~~Codex hook 是否同步写 session_activity~~ → 纳入 Phase 0
- ~~L2 标注用独立表还是复用 session_activity~~ → 独立 `annotations` 表
- ~~预算模型重构是否后置~~ → 纳入 Phase 1
- ~~cwd 依赖是否后置~~ → 临时方案纳入 Phase 1

**8/4 Codex 自审新增**：

- ~~MCP 调用能由 Claude hook 记录~~ → 推翻：MCP 对 hook 不可见，由 mcpLogDiscussion
  记录且 session_id 传空串 → store 归一化为 `unknown`（见 2.2 缺陷 3、4.2）
- ~~session_activity 表必要性存疑~~ → 已定案：方案 C（不建表 + metadata 补 rel_path，
  见 3.1）

**8/4 Claude 终审新增**：

- ~~4.1 表格残留 MCP 行~~ → 已删（MCP 对 hook 不可见，与 4.2 矛盾）
- ~~幽灵 session 问题~~ → 已定规避：MCP `'task'` 记录不进同行感知（决策 14）

**8/4 终审第三/四轮（数据核验 + 修正）**：

- ~~Claude：「Read metadata 6 月有、7 月后无，是回归」~~ → **不成立**：Claude Code
  Read 从未有 metadata（0/926，一贯设计非回归）
- ~~我上轮：「全库 0 条 Read 含 file_path」~~ → **错误**：只查了 `role=tool`
  （Claude Code），漏了 Cursor（role=assistant，👁 前缀，403/403 含 file_path，2026-06）
- ~~Claude：「407 条 Cursor Read 含 file_path」~~ → **定性成立、数量差 4**（实测 403）
- ✅ Claude：「三 hook metadata 异构，方案 C 需明确 Codex 覆盖边界」→ **成立**，
  定案见决策 18/19/20、§3.2、§4.1
- Claude 指出的 LIKE 转义、历史数据、格式耦合 → 已处理：json_extract 替代 LIKE
  （决策 16）、历史不回填（决策 17）、json_extract 只依赖字段名（3.1）

**8/4 终审第五轮（幽灵过滤核验，Claude 结论对/论据错）**：

- ✅ Claude：「IsOrphanSessionID 过滤会误伤 unknown 真实活动，幽灵过滤不能过滤所有 unknown」
  → **成立**（gemini-cli 4 条 👁 Read、aipmc-vision 32 条对话会被误伤）
- ❌ Claude：「464 条 claude-code unknown 不全是 MCP，可能有真实对话/文件操作」
  → **实测错**：465 条 **100% 是 📡 MCP**，claude-code 的 unknown 零误伤
- ✅ Claude：「与 MergeOrphans 职责重叠、时序未定义」→ **成立**，且进一步实测：
  MergeOrphans 只回填 24h 窗口（6 月 365 条永久滞留），查询端不能依赖它
- 定案：幽灵过滤 = `content NOT LIKE '📡%'` + `HAVING users>0`（现有代码行为），
  移除 v1.8 误加的 `IsOrphanSessionID` 过滤层（决策 21/22、§3.4）

**8/4 终审第六轮（缓存 + 真实场景，定稿前收敛）**：

- ✅ Claude 上轮两处纠正被验证成立：prepend 位置（collapse 后 INJECT 在末尾）、
  累积膨胀不成立（proxy 透明中间人，`emit_claude.go:247` 只回发 assistant）
- ✅ 缓存影响定案：会 miss 但频率受控（每 10 分钟最多一次）；`injectAnthropic`
  改顶层 system 列为 Phase 1c 优化项（决策 23）
- ✅ EncryptDrive 真实场景（claude + codex 并行）验证：文件碰触第一优先 +
  同行段折叠计数两级方案成立（决策 24）；单 agent 零注入降级正确
- ⏸ L1 增强项：文件碰触附带「编译状态」（决策 25，记 §8，不阻塞 Phase 0）

**8/4 终审第七轮（Claude 严格终审，定稿前收尾）**：

- ✅ v1.10 整体达成成熟度：文档内部一致、方案可定稿
- ✅ 决策 25 定位修正：编译状态是 **L1 碰触告警的信息完整度**问题，不是 L2/L3
  新能力（§8 标题同步改「L1 增强项」）
- ✅ 时序分工补明：折叠计数管「预先感知」、碰触告警管「冲突确认」——碰触判定
  只能拿到当前请求文件，本质事后；实施必须两级都做（5.3.2）
- ✅ active 窗口补默认值：30 分钟，与 guessSelf 共用（5.3、§6.3 Phase 2 调参）

**8/4 终审第八轮（缓存根治 + PreToolUse 拦截，最终形态定案）**：

- 🔄 **缓存解法推翻重构**：system 注入（时间桶/集合驱动）→ **user prompt 末尾注入**
  （最新 user message 不在缓存前缀，INJECT 变不影响命中，90%+ 达成）——决策 23 重写
- ✅ **PreToolUse 双条件 deny**：`5min 碰触 AND session 活跃` 才拦（决策 26）；
  唯一传达通道是 deny（`additionalContext` 对 PreToolUse 未实现，issue #6965）
- ✅ **三层分工**：user prompt 注入（普适）+ PreToolUse deny（Claude 专属）+
  PostToolUse 记录（Codex 兜底）（决策 27）；平台不对称是能力差异
- ✅ 覆盖边界验证：Codex 无 PreToolUse、Cursor 自定义 permission、OpenCode 只有事后

**8/5 终审第九轮（社区调研借鉴 + EncryptDrive 实况验证 + Claude/Codex 三轮 challenge 收敛，定稿）**：

- ✅ **EncryptDrive 实况 4 冲突验证**：文件碰触第一优先 + 折叠计数 + 用户手动转述痛点全部成立
- ✅ **Bug 检测与修复**：DDL 写锁风暴（`f61e845`）+ create_task 错误吞并——多 agent 场景 aipmc 基础设施先出问题
- 🔄 **社区调研**：agentlocks（git wrapper 锁）、scope.json（域校验）、flightdeck（activity ledger）——验证 v1.11 感知+注入+deny 方向正确
- ✅ **git wrapper 锁**（§4.4）：借鉴 agentlocks，hook 层 Bash 拦截取锁——**不需要 Codex 配合**，git 自身 `index.lock` 提供互斥（Codex 关键修正——被动生效 ≠ 退化）
- ✅ **scope.json 域校验**（§4.5）：warn 模式双方 PostToolUse 均生效；block 模式 Claude 专属。文件级运行时锁与域边界互补（不互相替代）
- ✅ **worktree 定位修正**（§4.6）：核心价值从「文件冲突隔离」→「编译环境隔离」；门槛只看编译互踩频率 + 同文件第 2 次加速；Claude 专属
- 🔄 **Claude/Codex 三轮 challenge 收敛**：4 条直接接受（checkout 移出锁表、TTL 120s+续期、HEAD~1 修正、同文件加速）、1 条可修（首次 commit 回退）、1 条驳回但 Claude 接受修正（git 锁被动生效——index.lock 是 git 自身互斥）
- ✅ **新增 §2.4 机制生效边界**：感知注入普适、git 锁/scope-warn 双方被动生效、PreToolUse deny + scope-block + worktree Claude 专属
- ✅ **决策 33-39**：机制边界、checkout 移除、TTL 续期、首次 commit、同文件加速、scope+lock 互补、scope 被动生效
- ✅ **Phase 表加「适用 agent」列**——区分「全部」与「Claude 专属」
- ⏸ **部署顺序**：f61e845 重启生效 → Phase 0 实施（避免 DDL 写锁影响 rel_path 写入可靠性）


---

## 修订记录

- **2026-08-05（v1.12 定稿）**：终审第九轮——社区调研借鉴（agentlocks git wrapper 锁 + scope.json 域校验）+ EncryptDrive 实况验证（4 冲突全部命中）+ Claude/Codex 三轮 challenge 收敛。新增 §2.4 机制生效边界（感知注入普适 / git 锁 scope-warn 被动生效 / deny scope-block worktree Claude 专属）。决策 25 op 字段提前 Phase 1c。窗口统一 CONST_COLLISION_WINDOW。worktree 定位修正为编译环境隔离 + 同文件加速门槛。决策 28-39 覆盖 git 锁细节（checkout 移除、TTL 续期、HEAD~1 修正）+ scope 被动生效 + 文件级锁互补。Phase 表加适用 agent 列。遗留：部署时先重启 f61e845 再上 Phase 0
- **2026-08-04（v1.9–v1.11）**：Codex/Claude 五轮讨论 + 八轮终审收敛。方案 C 数据层（hook metadata 补 `rel_path`、JSON_EXTRACT 精确匹配）、user prompt 末尾注入（缓存根治）、PreToolUse 双条件 deny、幽灵过滤三层定案、三 hook metadata 异构核验、EncryptDrive 真实场景验证。详见解构在上文的终审评述段落。
