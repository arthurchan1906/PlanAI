# AIPM 设计文档

> AI Project Manager — 连接 AI Coder 与产品经理的协作中枢

---

## 1. 项目定位

AIPM 是一个独立于 Code Agent 之外的项目管理工具，其核心使命是**填补 Code Agent 微观执行与 PM 宏观决策之间的真空地带**。

### 时间尺度的断层

```
分钟～小时  │  Code Agent 编排（做得好）
           │  "这个函数怎么写？这个文件怎么改？"
           │
天～周     │  ← 真空地带（本项目要填补的位置）
           │  "Task 做完了吗？进度跟 Plan 差多少？"
           │  "新发现推翻了什么前提假设？"
           │
周～月     │  PM 决策（信息严重滞后）
           │  "方向对不对？还要不要继续投入？"
```

Code Agent 的上下文窗口天然是短视的——窗口之外的等于不存在。AIPM 作为外部中间层，**主动填补 Agent 的认知盲区**，同时为 PM 提供**经过提炼的结构化进度视图**。

---

## 2. 现状分析

### 2.1 当前架构

```
AI Coder (CLI)                     PM (Web UI)
─────────────                      ──────────
aipmc task add     ──write──►      TasksView 查看
aipmc commit add   ──write──►      CommitsView 查看
aipmc bug add      ──write──►      BugsView 查看
aipmc idea capture ──write──►      IdeasView 查看
                                  DashboardView 看统计

                  ◄── 几乎没有反向通道 ──
```

### 2.2 核心问题

1. **AIPM 是纯被动的**：不能拦、不能推、不能分析、不能告警
2. **PM → AI 方向几乎没有能力**：PM 只能看，不能通过 AIPM 影响 Agent 的优先级和方向
3. **AI → PM 方向信息密度低**：`Task.blocked` 只是一个字符串，没有结构化原因
4. **规则执行力为零**：`skill.go` 生成的提示词是软建议，Agent 可以选择忽略
5. **缺少决策闭环**：PM 做了决策，Agent 不知道，继续按旧方案写代码

### 2.3 技术栈

| 层级 | 技术 |
|------|------|
| 后端 | Go 1.21 |
| 数据库 | SQLite（`modernc.org/sqlite`，纯 Go） |
| 前端 | React 18 + Vite 5 + Ant Design 5 |
| 通信 | CLI（`aipmc` 命令） |

---

## 3. 设计目标

### 3.1 核心原则

> **AIPM 不需要自己变聪明，它需要变得擅长"让 Agent 变聪明"。**

AIPM 不集成自己的 LLM，而是通过**精心构造的上下文**来引导 Code Agent 的 LLM 做出更好的决策。

```
AIPM 准备高质量上下文
  → 注入 Code Agent 的视野
  → Agent 的 LLM 自然做出正确决策
  → Agent 感觉是自己做的决策（事实上也是）
```

### 3.2 目标架构

```
                     PM 意图层
                    ┌─────────────────┐
PM ──提出──►  Idea / Priority / Decision
                    └────────┬────────┘
                             │ 影响
                    ┌────────▼────────┐
AI ──执行──►  Task → Commit → Bug     │  ← 记录
                    └────────┬────────┘
                             │ 反馈
                    ┌────────▼────────┐
PM ◄──查看──  Dashboard / 分析 / 告警 │  ← 增强
                    └─────────────────┘

          AIPM 分析引擎（新增）
          ┌─────────────────────────┐
          │ 漂移检测 / 冲突发现      │
          │ 进度分析 / 决策影响追踪   │
          │ 简报生成（Prompt 工程）  │
          └─────────────────────────┘
```

### 3.3 三个核心职能

| 职能 | 描述 |
|------|------|
| **上下文注入** | Agent 启动时把关键信息塞进它的视野，不只是返回数据，而是生成一段"简报" |
| **漂移检测** | 检测实际产出 vs 计划方向的偏差：文件范围超出 scope、方案不一致等 |
| **速度缓冲** | 对 Agent 产出的 commit 做预审过滤，提炼摘要，减少 PM 的 review 负担 |

---

## 4. 与 Code Agent 的协作模式

### 4.1 核心洞察

独立的工具无法"干预" Agent。但可以**确保每次 Agent 与 AIPM 交互时，都获得一份精心准备的"情报简报"**。Agent 自己读了简报后做的决策，就是被 AIPM 影响的决策。

### 4.2 CLI vs MCP

| 维度 | CLI（现状） | MCP（目标） |
|------|:--:|:--:|
| 可见性 | Agent 需要"想起"调用 | 工具始终在 Agent 的 tool list 中 |
| 信息密度 | 返回最小 JSON | 可附带丰富的关联上下文 |
| 交互模式 | 一问一答，用完即忘 | 持续在场，每次决策都可见 |
| 事件推送 | 不支持 | 支持 notification（状态变更主动通知） |
| 认知负担 | Agent 需要记忆 | Agent 只需要选择 |

**结论：选择 MCP。** 不是在 CLI 和 MCP 之间二选一，而是**MCP 作为主接口，CLI 作为后备**。

### 4.3 MCP 工具设计（规划）

```
aipm_get_briefing    → Agent 启动时获取简报 ← 核心
aipm_search_context  → 搜索 + 返回跨实体关联上下文
aipm_create_task     → 创建 task + 返回重复/冲突提示
aipm_record_commit   → 记录 commit + 返回关联性分析
aipm_check_context   → 作业中随时检查"当前方向对吗？"
```

### 4.4 简报（Briefing）输出示例

Agent 每次获取简报时，得到的不是原始 JSON，而是一段结构化的上下文：

```markdown
🏗️ 项目简报 — 2026-05-30

## 你当前的任务
Plan「用户认证重构」(plan-auth-01) → Task「实现 argon2 密码哈希」(task-auth-02)
进度: 40% | 剩余 deadline: 5 天

## ⚠️ 需要你注意的变化
1. [新决策] Decision #44: API 错误格式统一用 RFC 7807
   → 影响你当前的 error handling 代码
2. [PM 标记] task-session-05 紧急，建议 task-auth-02 完成后优先

## 🔗 相关性提醒
- task-auth-01 已完成，用了方案 A，但你的 task 方案是 B
  → 建议保持一致或记录决策理由
- 你的上一个 commit 引入了新依赖，但未被 plan scope 覆盖

## 📋 下次提交时请记录
- 变更的文件是否在 task scope 内
- 是否有跨 task 的副作用
```

### 4.5 反射检查（Reflection）机制

每次 Agent 执行关键操作后，MCP 返回中附带"反思提示"：

```
Agent: 创建 commit
AIPM 返回:
  { "commit": {...},
    "reflection": "你修改了 session.go，但当前 task scope 只含 auth.go。
                   task-session-03 也在进行中。
                   ⚠️ 考虑：是否应关联到 task-session-03？"
  }
```

这不是命令，而是把矛盾摆在 Agent 面前，让它自己发现。

---

## 5. 新增模块设计

### 5.1 总体分层

```
┌─────────────────────────────────────────────┐
│                  AIPM                        │
│                                              │
│  ┌──────────┐  ┌──────────┐  ┌───────────┐ │
│  │ 数据层    │  │ 分析层    │  │ Prompt 层 │ │
│  │ (已有)   │  │ (新增)   │  │ (新增)    │ │
│  │          │  │          │  │           │ │
│  │ SQLite   │  │ 漂移检测  │  │ 把分析结果 │ │
│  │ CRUD     │  │ 冲突检测  │  │ 转换为     │ │
│  │ Schema   │  │ 进度分析  │  │ Agent 可   │ │
│  │          │  │ 决策影响  │  │ 执行的     │ │
│  │          │  │          │  │ Prompt     │ │
│  └──────────┘  └──────────┘  └─────┬─────┘ │
│                                     │        │
│  ┌──────────────────────────────────┘        │
│  │ MCP Server 层（新增）                     │
│  │  - 工具注册与调度                          │
│  │  - 结构化返回 + 上下文注入                  │
│  └─────────────────────────────────────────── │
└──────────────────────────────────────────────┘
```

### 5.2 分析引擎

需要实现的检测能力：

| 检测类型 | 触发条件 | 输出 |
|---------|---------|------|
| **重复检测** | 新 Task 标题与已有 Task 相似度 > 阈值 | 提示可能重复，建议查看已有 task |
| **漂移检测** | Commit 涉及的文件不在 Task scope 内 | 警告：改动超出范围 |
| **方案冲突** | 同一 Plan 下两个 Task 采用了矛盾的技术方案 | 建议对齐或记录决策 |
| **进度预警** | Plan 完成率 vs 剩余时间不匹配 | 风险提示：可能无法按时交付 |
| **阻塞超时** | Task blocked 超过 N 天 | 告警：需要 PM 介入 |
| **决策影响** | Decision 更新后，关联的 Task 标记为"需重新评估" | 影响报告 |
| **孤立检测** | Task in_progress 但无对应的 Commit | 提醒：无代码产出 |

### 5.3 简报生成器（Prompt 工程层）

职责：把分析引擎的原始输出，翻译为 Agent 的 LLM 能高效消化的 Markdown 格式。

关键原则：
- **优先级分层**：紧急 > 重要 > 参考
- **可操作**：每条信息附带建议动作
- **克制**：不超过 Agent 的注意力预算（3-5 条核心信息）
- **叙事化**：不是罗列数据，而是讲一个"项目现在处于什么状态"的故事

### 5.4 变更追踪

新增 `events` 或 `notifications` 表，记录 PM 的意图变更：

```sql
CREATE TABLE events (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,          -- 'decision_changed', 'task_priority_changed', 'pm_note'
    entity_type TEXT NOT NULL,   -- 'task', 'plan', 'decision'
    entity_id TEXT NOT NULL,
    summary TEXT NOT NULL,       -- 人类可读的变更摘要
    created_at TEXT NOT NULL,
    consumed_by_agent INTEGER DEFAULT 0  -- Agent 是否已读取
);
```

Agent 每次获取简报时，自动拉取 `consumed_by_agent = 0` 的 events 并注入提示。

---

## 6. Schema 增强（基于现有表结构）

现有 Schema 已有良好基础，需要增强的字段：

### 6.1 Task 表增强

```sql
-- 阻塞原因结构化
ALTER TABLE tasks ADD COLUMN block_reason TEXT;
ALTER TABLE tasks ADD COLUMN blocked_by TEXT;   -- 'pm' / 'external' / 'tech_unknown'
ALTER TABLE tasks ADD COLUMN needs_from TEXT;   -- 解除阻塞需要什么
```

### 6.2 Commit 表增强

```sql
-- 自动预审标记
ALTER TABLE commits ADD COLUMN scope_check TEXT;  -- 'pass' / 'warn' / 'fail'
ALTER TABLE commits ADD COLUMN scope_notes TEXT;  -- 超出 scope 的具体说明
```

---

## 7. 实施路径

### Phase 1：分析引擎 + CLI 增强（不改变通信协议）

- [ ] 实现漂移检测、进度预警、阻塞超时等规则引擎
- [ ] `aipmc start` 返回从纯 JSON 升级为"数据 + 分析结果"
- [ ] `aipmc analyze` 新增分析命令，供 PM 使用
- [ ] Web Dashboard 集成分析结果展示
- [ ] 新增 `events` 表，追踪 PM 意图变更

### Phase 2：MCP 入口（新增通信通道）

- [ ] 实现 MCP Server（基于 Go，`stdio` 传输）
- [ ] 注册核心 MCP Tools：`aipm_get_briefing`、`aipm_search_context`、`aipm_record_commit`、`aipm_create_task`
- [ ] MCP Tool 返回附带 `related_context` 和 `reflection`
- [ ] 简报生成器实现（规则引擎 → Markdown prompt）
- [ ] CLI 保留作为后备

### Phase 3：双向事件流 + 深度集成

- [ ] MCP notification 机制：PM 操作后主动推送
- [ ] Agent 消费追踪：`consumed_by_agent` 标记
- [ ] 反射检查机制：commit 后自动触发关联性分析
- [ ] PM 意图 → Agent 优先级的完整闭环

### Phase 4（远期）：高级分析

- [ ] 外部 LLM 辅助生成简报（可选，非必需）
- [ ] 跨项目的模式识别（什么类型的 task 容易延期）
- [ ] 项目健康度评分模型

---

## 8. 设计决策记录

### Decision 1：不内置 LLM

**决策**：AIPM 本身不集成 LLM Agent。

**理由**：
- AIPM 没有执行权，分析出问题也无法干预 Agent
- Code Agent 的 LLM 能力天然可用，通过 prompt 工程引导比另起炉灶更有效
- 节省 API 成本和复杂度
- 分析引擎用硬编码规则 + SQL 查询即可覆盖大部分场景

### Decision 2：MCP 作为主接口，CLI 保留

**决策**：新增 MCP 通道作为 Agent 交互的主接口，现有 CLI 保留作为后备和调试。

**理由**：
- MCP 提供持续可见性（工具始终在 Agent tool list 中）
- MCP 支持结构化返回 + 上下文注入，信息密度远高于 CLI
- CLI 在生产环境中仍有价值（CI/CD 集成、脚本、调试）

### Decision 3：渐进式实施

**决策**：不分阶段全量重构，而是逐步增强。

**理由**：
- 数据层和 Schema 已有良好基础，不需要推倒重来
- 每个 Phase 都能独立交付价值
- 分析引擎的规则可以从简单开始，逐步丰富

### Decision 4：简报而非命令

**决策**：AIPM 对 Agent 的输出永远是"简报"（信息 + 建议），而非"命令"。

**理由**：
- Agent 的自主性是 Code Agent 的设计特征，不应破坏
- 信息充分的 Agent 自然会做出更好的决策
- "感觉是自己做的决策"是 LLM 有效工作的前提条件

---

## 9. 附录：当前实体关系

```
Vision (愿景)
  └── Roadmap (路线图)
        └── Plan (计划)
              └── Task (任务)
                    ├── Commit (提交) — 关联代码提交
                    ├── TaskNote (备注)
                    └── Bug (缺陷)
Decision (决策) — 独立，可被 Commit 引用，可更新 Canon
Idea (想法) — 可转化为 Task 或 Decision
Principle (原则) — 治理规则
Docs (文档记录) — 文档追踪
Canon (规范) — 当前产品目标/工程重点/架构
DailyNotes (日常笔记)
Links (关联) — 实体间关系链接
Events (事件) — [Phase 1 新增] PM 意图变更追踪
```

---

*最后更新：2026-05-30*
