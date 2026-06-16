# 多 Agent 协作平台 — 设计文档

> 将 PlanAI 从单 Agent 被动简报工具升级为多 Agent 协作调度中枢

---

## 1. 角色模型

### 三种参与者

| 角色 | 身份 | 交互方式 |
|---|---|---|
| **PM (Human)** | 项目管理者 | Web UI — 创建会议、点名、分配任务、做决策 |
| **Code Agent** | AI 编码助手 | MCP — 接收分配、参与会议、记录产出 |
| **Insight Agent** | AI 分析助手 | MCP — 监控进度、发现风险、建议线索 |

### Agent 能力档案

```sql
CREATE TABLE agent_profiles (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'coder',  -- coder | reviewer | insight
    capabilities TEXT NOT NULL DEFAULT '[]',
    status TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
```

---

## 2. 人驱动会议系统

### 核心原则

- **会议由人发起和推动**——PM 点名，Agent 响应；PM 不点名，Agent 不发言
- **上下文隔离**——每个被点名的 Agent 只看到：会议主题 + PM 对自己提的问题 + 前面所有人的 Q&A + 相关项目实体
- **多轮持续**——PM 可以追问同一个 Agent、点名下一个 Agent、或结束会议

### 数据模型

```sql
CREATE TABLE meeting_rooms (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    topic TEXT NOT NULL,           -- 讨论主题
    context TEXT NOT NULL,         -- 背景材料 (Markdown)
    status TEXT NOT NULL DEFAULT 'active',
    created_by TEXT NOT NULL,
    created_at TEXT NOT NULL,
    closed_at TEXT
);

CREATE TABLE meeting_turns (
    id TEXT PRIMARY KEY,
    room_id TEXT NOT NULL,
    turn_number INTEGER NOT NULL,
    speaker_type TEXT NOT NULL,    -- 'human' | 'agent'
    speaker_id TEXT NOT NULL,
    question TEXT NOT NULL,        -- PM 对 speaker 的提问
    response TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'waiting',
    created_at TEXT NOT NULL
);
```

### 会议流程

```
1. PM 创建 Meeting Room (Web UI)
   - 填写 title, topic, context (Markdown 背景)

2. PM 点名 Agent-A (Web UI)
   - 选择 Agent, 输入 question
   - meeting_turns 新增一条 (status=waiting)

3. Agent-A 通过 MCP 响应
   a. 调 aipm_get_meeting_turn(room_id, turn_id)
      返回:
        - 会议 topic + context
        - PM 对 Agent-A 的提问
        - 本轮之前所有 turns 的 Q&A (按顺序)
        - 关联的项目实体 (通过 AI 搜索 topic 匹配)
   b. 理解上下文后
   c. 调 aipm_respond_in_meeting(turn_id, response)
      → turn status 变为 responded

4. PM 查看 Agent-A 的回应 (Web UI)
   可以:
   - 追问 Agent-A (创建新 turn, 引用上一条)
   - 点名 Agent-B (基于 A 的回答继续讨论)
   - 结束会议

5. PM 结束会议 (Web UI)
   - room status → closed
   - 可选: AI 生成会议摘要
   - 可选: 写入 decision 表
```

### 上下文分发（aipm_get_meeting_turn 返回）

```markdown
## 会议: <title>

### 会议背景
<topic + context>

### PM 对你的提问
<question directed to this agent>

### 本会议之前的发言 (按时间顺序)
- [PM] 对 Agent-A 的提问: "..."
- [Agent-A] 回应: "..."
- [PM] 对 Agent-B 的提问: "..."
- [Agent-B] 回应: "..."

### 相关项目实体
- Task: <related tasks found by AI search on topic>
- Decision: <related decisions>
- Thread: <related threads>
```

**Agent 看不到的**：
- 其他会议的内容
- PM 对别的 Agent 的追问（如果 PM 没有选择"公开"）
- 内部分析数据

---

## 3. 角色分配与自驱动工作

### 数据模型

```sql
CREATE TABLE agent_assignments (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    task_id TEXT,
    role TEXT NOT NULL,            -- implementer | reviewer | insight
    scope TEXT NOT NULL,           -- 工作范围 (Markdown)
    status TEXT NOT NULL DEFAULT 'assigned',
    assigned_by TEXT NOT NULL,
    assigned_at TEXT NOT NULL,
    claimed_at TEXT,
    completed_at TEXT
);
```

### 工作流

```
PM 分配:
  在 Web UI 选择 Agent, 选择 Role, 填写 Scope
  可以绑定到具体 Task, 也可以是全局角色 (如 "审查所有新提交的代码")

Agent 自驱动:
  1. 启动时调 aipm_get_briefing
     → 看到自己的角色 + 分配清单
  2. 调 aipm_get_my_assignments 获取详情
  3. 调 aipm_claim_assignment 认领
  4. 执行工作 (review 代码 / 分析进度 / 写代码)
  5. 通过 aipm_record_commit 等记录产出
  6. 调 aipm_complete_assignment 标记完成
```

### 角色类型

| Role | 职责 |
|---|---|
| `implementer` | 执行具体 task，写代码，修 bug |
| `reviewer` | 审查 commit 代码质量、scope drift、安全性 |
| `insight` | 全局分析进度、发现风险、建议线索、生成报告 |

---

## 4. 个性化简报

`aipm_get_briefing` 根据调用者的 `agent_id` 过滤内容：

```markdown
## 你的角色: reviewer

### 待审查
- commit-abc (task-auth-02) — 等待审查, 提交于 1h 前
- commit-xyz (task-session-03) — 等待审查, 提交于 3h 前

### 需要你关注
- Decision #44: API 错误格式变更 — 可能影响你审查的代码

### 项目动态
- task-auth-02 进度 80%, 预计 2 天后完成
- task-session-05 被 PM 标记为紧急

### 待参与的会议
- Meeting: "认证模块架构方案讨论" (Agent-A 已发言, 等待你的意见)
```

---

## 5. 审计日志

```sql
CREATE TABLE audit_log (
    id TEXT PRIMARY KEY,
    actor_type TEXT NOT NULL,           -- human | agent
    actor_id TEXT NOT NULL,
    action TEXT NOT NULL,               -- meeting_turn_responded | assignment_claimed | ...
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    summary TEXT NOT NULL,              -- 人类可读
    detail_json TEXT NOT NULL,          -- 完整数据
    created_at TEXT NOT NULL
);
```

所有关键操作自动记录。Web UI `/audit` 可搜索、筛选、导出。

---

## 6. 与现有系统的关系

已有的实体层级 **不变**：

```
Vision → Roadmap → Plan → Task → Commit → Bug
                              ↑
              agent_assignments 关联到这里
```

新增实体是**横向切面**，不是替代：

```
meeting_rooms ──→ decisions (会议产出决策)
meeting_turns ──→ 孤立实体，仅会议内可见
agent_assignments ──→ tasks (可选的绑定)
agent_profiles ──→ 独立实体
audit_log ──→ 所有实体的操作记录
```

---

*关联 Roadmap: rdm-20260606-105154-2ab007 (多 Agent 协作)*
*关联 Plan: plan-20260606-105217-c013fe, plan-20260606-105217-26f0d7, plan-20260606-105218-dfc1c5, plan-20260606-105218-4ce5ea*
