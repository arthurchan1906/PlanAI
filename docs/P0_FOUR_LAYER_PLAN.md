# P0 四层机制落地实施方案（draft）

> 状态：**draft**（Claude 已背书 2026-08-28 17:00 + 给出修订建议；待用户确认后 create aipm task / 开工实现）
> 依据：`docs/D1_ATTRIBUTION_PROTOCOL.md` §7.1 四层失效模型 + 本次代码勘察 + ED 实证 + D1 双标数据
> 原则：**「prompt 注入会被压缩吃掉，代码钩子不会」**；不以 agent 自述为口径，以日志为权威（M1a 对账）。

## 0. 一句话目标

把「agent 想不起来用 AIPM」这一 P0 痛点，拆成 **四层**，每层一个**代码层确定性动作**（非 prompt 提示）+ 一个**可复算验收指标**。四层都成立后，自发率从 0/15%（纯/含半自发）向 30% 双目标爬坡才有依据。

## 1. 四层总览（证据 → 动作 → 验收）

| 层 | 代码现状证据 | 落地动作 | 涉及文件 | 验收指标 |
|---|---|---|---|---|
| ① **动作点未固定** | `hook/hook_claude.go` 只做元数据收集（`collectWriteFiles`/`postToolMetaJSON`），**无**「引导/自动调用 aipm 工具」的确定性动作清单 | **确定性代码钩子**：session 开工注入上下文卡 / git commit 完成自动 `record_commit` / 大变更文件无 bug 记录则提示 `record_bug` | `hook/*.go`、`proxy/*.go` | commit→task 覆盖率提升；bug→commit 闭环率提升 |
| ② **规范语义模糊** | `proxy/context_inject.go` 注入 `guidelines`（仅 600B），指令映射错（「decision 先查」→ 执行成 `search_context`） | **规范措辞对齐**：改写 guidelines/CLAUDE.md 指令为「工具名直映射」，消除语义相近工具歧义 | `proxy/context_inject.go`、guidelines 源 | `search_context` 误用（本当走 `list_decisions`）次数下降 |
| ③ **compaction 后注入过期** | `proxy/context_inject.go` 有 `injectTracker`（content-hash 去重、`same_content` 跳过）；compaction 后 SessionStart 成早间快照 | **注入随 PM 变更刷新 + compaction 时清缓存强重注入** | `proxy/context_inject.go` | 后半程 PM 变化可见率（compaction 后 agent 仍能感知新 bug/decision） |
| ④ **工具输出形状不匹配** | `analyze/analyze.go` `BuildBriefingLevel` 只输出 PM 事件流（PM 最新变更/阻塞/孤儿/进行中/Scope 漂移/重复）；`agent_briefing`/代码上下文卡/验证台账**零实现** | **改工具输出形状，分层落地**：Phase A MVP 用**确定性关联**（当前 task × 文件 × decision）；Phase B 补数据地基（`bug.task_id` + 验证台账实体）；Phase C **不做**语义「相关」 | `analyze/analyze.go`、`mcp/mcp.go`、`db/store` | agent_briefing 上下文卡命中率（确定性关联，可复算） |

## 2. 每层细节（有事实依托）

### ① 动作点未固定 → 确定性代码钩子
- **证据**：ED-claude T1 3 例漏用全是「不在思维候选集」，无「犹豫时刻」；hook 现状只收元数据，无里程碑引导。
- **设计**：aipmc 在 `hook`/`proxy` 层检测**里程碑事件**（session 开始 / git `post-commit` / 单次会话文件大变更），由**代码直接**触发 `record_commit`、`record_bug`，或注入开工上下文卡。**不依赖 agent 主动想起**，故不被 compaction 吃掉。
- **关键约束**：触发是「记录/引导」而非「替 agent 做决策」，避免假阳性噪音；记入 `[INJECT]`/`[HOOK]` 日志便于 M1a 对账。

### ② 规范语义模糊 → 规范措辞对齐
- **证据**：ED-claude B：「decision 先查」被映射成 `search_context`；`get_briefing` 被误当 PM 管理视图。
- **设计**：`loadGuidelines()` 修正为「指令 → 唯一工具名」直映射短语（如「查决策 → `list_decisions`/`get_decision`」），并给语义相近工具加「何时不用」的排除句。注入预算仍守 600B，重在消除歧义而非加量。

### ③ compaction 后注入过期 → 注入刷新
- **证据**：ED-claude C 实测 compaction 后完整忘记 `record_bug` 但 43 工具全在手；`injectTracker` 以 content-hash 去重，系统级压缩后不会自动重注入。
- **设计**：当 `discussion_log` 出现**系统级 compaction/summary 新数据**，或 session 摘要变化时，清除该 agent 的 `injectTracker`，强制下一次请求重注入最新 PM 状态。

### ④ 工具输出形状不匹配 → 分层落地（Claude 从 AIPM 数据地基背书的可行性）
- **证据**：`BuildBriefingLevel` 只输出 PM 事件流/管理视图；agent 真正需要「相关文件 × bug × decision × 验证台账」。四要素在 AIPM 数据地基的**可落地性完全不同**（见 §2 表）。
- **设计（Claude 建议，AIPM 自身逻辑决定）**：`agent_briefing` 上下文卡**分层**，避免语义「相关」滑向理解层（D1 核心纪律：零语义）：
  - **Phase A（零新数据，直接可做）**：上下文卡 = 当前 task × 文件（复用 `resolveFileContext` 确定性路径匹配）× 相关 decision（`related_decisions_json`）——全部确定性关联，不经语义。
  - **Phase B（先补数据地基）**：① bug→task 直连（`bug.task_id` 外键，或暂用 commit_id 两跳转给「近似」）；② **验证台账实体**（scene × device × KSN × result）——ED 最高频需求但 AIPM 零结构，作为**独立新实体**，不进 briefing MVP。
  - **Phase C（不做）**：把「相关」做成全语义匹配——会撞理解层/语义依赖，明确排除。
- **四要素可落地性**（Claude 核实 AIPM 数据结构）：

| 要素 | AIPM 现状 | 可落地性 |
|---|---|---|
| 文件×task | `resolveFileContext` 已有确定性路径匹配，briefing 未接 | ✅ 可接 |
| task×decision | `related_decisions_json` / `decisions.related_tasks_json` 双向 | ✅ 可接 |
| bug×task | bug 表仅 `commit_id`，无 `task_id`，需 bugs→commits→task 两跳转 | ⚠️ 绕 |
| bug×file | `bugs.files` 逗号分隔字符串，无索引 | ⚠️ 脆弱 |
| 验证台账 | 不存在（仅 `test_status` 布尔） | ❌ 需新建实体 |

## 3. task 分解映射（待确认后 create）

| 层 | task（挂 P0 plan `plan-20260615-172601-479092`） | task_id | 优先级 | owner | 依赖 |
|---|---|---|---|---|---|
| 前置A | MCP 工具健壮性：get_decision 前缀匹配 + trace_context 校验 + get_commit 回归 | `task-20260828-171119-e3c9d3` | P0 | codex（mcp/store） | — |
| ① | 确定性代码钩子（开工/commit/bug 里程碑自动触发） | `task-20260828-171120-f289f0` | P0 | codex（hook/proxy） | 前置A（工具好用是钩子前提） |
| ② | 指南措辞对齐（guidelines 指令→工具名直映射） | `task-20260828-171122-a576b3` | P0 | codex（context_inject） | ① 的钩子（避免重复提示） |
| ③ | compaction 重注入（扰动 injectTracker / 摘要变化刷新） | `task-20260828-171123-4b49aa` | P0 | codex（context_inject） | — |
| ④a | `agent_briefing` 上下文卡 MVP（task×file×decision，确定性关联） | `task-20260828-171124-29e793` | P0 | claude（analyze/mcp） | 前置A（读工具需稳定） |
| ④b | 数据地基：`bug.task_id` 外键 + 验证台账实体（scene×device×KSN×result） | `task-20260828-171125-67755b` | P0 | claude（db/store） | ④a 设计定稿 |

> **已创建**：2026-08-28 17:11 六个 task 全部落盘 P0 plan，依赖关系 = 前置A → ①②④a → ④b。

> **owner 说明**：Claude 建议明确文件所有权避免多 agent 撞文件。此处按 aipmc 当前 repo 分工——`hook/proxy/context_inject`（① ② ③）归 codex；`analyze/mcp/store`（④a ④b）归 claude 主实现 + codex 复核；实现时以「写集不相交」为硬约束。

> 收敛说明：P0 plan 下已有 54 个 task，大多数为 done/deleted/paused；四层落地应作为**新 task** 追加到 P0 主线，而非混入旧 Phase0/Phase1 实现。此前散落的机制方案（旧 v1.13 / EXECUTION_PLAN）归档，四层为本期唯一权威方案。

## 4. 验收与下一步

1. **Claude 复核本方案**（重点：④ 的 `agent_briefing` 与上下文卡是否覆盖「相关文件×bug×decision×验证台账」四要素）。
   - ✅ **已完成**（2026-08-28 17:00）：背书 + 修订④为分层 MVP；17:01 从 AIPM 数据地基确认四要素可落地性分层。
2. **用户确认** → **已创建** 6 个 aipm task（§3 映射，2026-08-28 17:11）→ 按依赖开工实现（前置A 先行）。
3. 实现完成后跑 `go build` + 相关单测；**禁止 git push**（除非明确要求）。
