package main

const skillMD = `---
name: pmai
description: AIPM project management — use MCP tools for all operations. Triggers: before coding, after commits, bug discovery, creating tasks/plans.
---

# AIPM — AI Project Manager

AIPM is the project's knowledge base. Every task, commit, plan, bug, decision lives here.
**Use MCP tools — they are always visible in your tool list. No CLI memorization needed.**

---

## 完整工作流 (CHECKLIST — 每个会话必须按顺序完成)

### Phase 1: 会话开始 (START — 每次新对话/新任务前)

1. **aipm_get_briefing** — 获取项目状态快照
   - 当前进行中的任务 (in_progress tasks)
   - 活跃计划 (active plans)
   - 活跃线索 (active threads) — 了解正在进行的工作线
   - PM 未读提醒 (unconsumed alerts)
   - 风险提示 (risks)、重复检测 (duplicates)
   - 建议的线程 (thread suggestions) — 从上一次 commit 推断的模式
2. **aipm_mark_consumed** — 确认已阅读 PM 提醒（读完立即标记，不要跳过）
3. **aipm_search_context** — 如果你要开始一个新任务，先搜索是否有相关已有工作
4. **决策**: 用 briefing 中的信息决定：
   - 继续已有任务？→ 更新任务状态 (aipm_update_task)
   - 创建新任务？→ Phase 2
   - 此任务属于哪个 Plan？哪条 Thread？

### Phase 2: 创建任务 (CREATE — 写代码之前)

1. **aipm_create_task** — 创建任务（不要手动拼命令）
   - 必须提供: title, plan_id（不能创建孤儿任务）
   - 可选: description, priority
   - 工具会自动检测重复、自动回填父级、校验 plan 状态
2. **aipm_link_entities** — 如果任务与其他实体有因果/依赖关系，建立 link
3. 不要跳过创建 — 哪怕是很小的改动也要有对应的 task

### Phase 3: 编码过程中 (DURING — 每次 git commit 后)

1. **aipm_record_commit** — 每次 commit 后立即记录
   - 必须提供: commit_id, task_id
   - 可选: message, files
   - 工具会自动检测 scope drift（变更超出 plan 范围）、跨任务文件冲突
2. 如果发现 bug → **aipm_record_bug**（必须提供 error 和 root-cause，不要省略）
3. 如果需要做决策 → **aipm_record_decision**
4. 如果有新想法 → **aipm_record_idea**
5. 如果任务完成 → **aipm_update_task**（status 改为 done/completed）
6. 如果遇到了当前 task 未覆盖的问题 → 回到 Phase 2 创建新 task

### Phase 4: 会话结束 — 每日复盘 (REVIEW — 每天结束时必须执行)

1. **aipm_daily_review** — 获取今日所有 commits 的完整上下文
   - 每条 commit 包含: task_title, plan_title, files, 已有线索归属
   - 工具会同时返回启发式建议（算法聚类结果）
2. **分析 commits 之间的语义关联** — 这是 Agent 的判断工作，不是算法工作：
   - 哪些 commits 虽然在不同的 plan/task 下，但实际属于同一个工作流？
   - 哪些 commits 是同一个 feature/bugfix 的不同部分？
   - 哪些 commits 是独立的一次性修改？
3. **创建/更新线索**:
   - 对有意义的发现 → **aipm_create_thread**
     - 标题要具体、有意义（不是 "Work on frontend"，而是 "Threads 线索功能前端可视化"）
     - summary 要写清楚：这条线索在做什么、为什么重要、涉及哪些模块
   - 对匹配已有线索的 commits → **aipm_add_to_thread**
     - entity_type 可以是: task, commit, decision, idea, plan, bug
     - note 参数可以简要说明为什么归入此线索
4. **检查遗漏**:
   - 有没有 in_progress 但今天没 commit 的任务？→ 用 **aipm_update_task** 更新状态或备注
   - 有没有完成的 task 没更新状态？→ **aipm_update_task** 标记 done
   - 有没有新 bug 没记录？→ **aipm_record_bug**
5. **aipm_suggest_threads** — 可选，作为启发式参考（不要盲从，优先相信自己的语义分析）

---

## 线索 (Threads) — 概念说明

Threads 是跨 plan/task/commit 的回溯性工作聚合：
- **什么时候创建**: 工作完成之后（retrospective），不是开始之前
- **创建标准**: 至少 2 条相关的 commit（或 task），它们之间有逻辑关联
- **不是按 plan 分**: 同一个 plan 下的 commits 不一定要归入同一条线索（可能做的是不同的事）
- **也不是按目录分**: frontend/ 下有多个 feature，各自是独立的线索
- **正确的归因**: 工作完成后复盘时，Agent 用自己的语义理解分组

### 线索的生命周期
1. Agent 调用 aipm_daily_review 获取 commits
2. Agent 分析 commits 的语义关联
3. Agent 调用 aipm_create_thread 创建新线索（或 aipm_add_to_thread 追加到已有线索）
4. 线索创建后，PM 在前端 Web UI 确认/调整
5. 超过 7 天无活动的线索会被标记为 paused

---

## 实体层级 (STRICT)

commit → task → plan → roadmap
**Every record must have a parent. No orphans.**

---

## 实体关联 (Link Relations)

使用 **aipm_link_entities** 连接实体：
- **causes** — A 的工作直接导致需要 B
- **enables** — 完成 A 为 B 解锁了前提条件
- **blocks** — A 阻碍 B 的进展
- **supersedes** — B 取代了 A（但 A 的经验仍可能相关）

---

## 会议行为准则 (MEETING RULES)

会议有两种模式，Agent 必须先确认模式再行动。

### 模式 A：讨论模式（默认）
- ✅ 可以：搜索代码、阅读文件、分析逻辑、提出方案、架构推理
- ❌ 禁止：修改代码、执行终端命令、创建/修改文件、git 操作
- 适用：方案评估、架构决策、风险分析、需求讨论

### 模式 B：会诊调试模式
- ✅ 可以：修改代码、执行命令、测试修复、实时调试
- ✅ 每步操作后通过 **aipm_speak_in_meeting** 汇报结果
- 适用：PM 明确要求现场修复 bug、联调问题排查

### 参与方式
- **PM 点名**：aipm_get_briefing → 看到 inbox 提示 → aipm_get_meeting_turn → aipm_respond_in_meeting
- **主动发言**：aipm_get_briefing → 检查会议新消息 → aipm_get_meeting_turn 同步上下文 → aipm_speak_in_meeting

### 发言前检查
1. 先调 aipm_get_briefing(agent_id) 看看有无待回应的 PM 点名 —— 优先回应
2. 再调 aipm_get_meeting_turn 获取最新上下文 —— 不要遗漏别人的发言
3. 确认当前模式（讨论/会诊）—— 不确定时默认为讨论模式
4. 发言后等待 PM 反馈或继续观察

---

## 禁止事项 (NEVER)

- ❌ 在 aipm_get_briefing 之前开始写代码
- ❌ 创建没有 plan_id 的 task（孤儿任务）
- ❌ 在 aipm_daily_review 之前结束会话
- ❌ 忽略 MCP 工具返回的 reflection 提示
- ❌ 记录 bug 时不提供 error 和 root-cause
- ❌ 盲从 aipm_suggest_threads 的启发式建议（用自己的分析判断）
- ❌ 会议中在不确认模式的情况下修改代码（默认用讨论模式）
- ❌ 发言前不同步 aipm_get_meeting_turn 获取最新上下文
- ❌ PM 点名后不优先回应（PM 点名优先于主动发言）
- ❌ 创建无意义标题的线索（如 "Work on frontend"、"Recent commits"）
- ❌ commit 后不调用 aipm_record_commit

---

*Installed by aipmc. To update: aipmc init*
`
