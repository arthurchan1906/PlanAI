# 多 Agent 协作设计 v1

> 第三方 Code Agent 协调系统的产品设计。  
> 基于 2026-06-06 早期会议原型 + **2026-06-15** PM ↔ Claude Code ↔ Cursor 三方讨论及 Live Case Study 收敛。  
> 替代原「实时同步会议 + 自动仲裁」作为 **v1 默认路径**。

**相关文档：**

- 原会议原型（已归档）：[`MEETING_DESIGN.md`](MEETING_DESIGN.md)
- 多 Agent 角色模型：[`MULTI_AGENT.md`](MULTI_AGENT.md)

---

## 0. 一句话定位

**第三方 Code Agent 的协作 = Hook 透明采集 + discussion 共享记忆 + PM 异步巡视 + 工具消除路由劳动。**

- 不是 Agent 自动开会
- 不是 PM 全程当传话员
- 不是同步会议室

---

## 1. 核心约束

| 约束 | 含义 |
|------|------|
| **Agent 不可控** | Claude Code / Cursor / Gemini 等是请求-响应产品；不保证调 MCP、无后台循环 |
| **PM 随机参与（巡视员）** | PM 不定时出现、扫一眼、说一句、离开；**非**全程在线主持 |
| **Agent 不空转** | PM 离开后 Agent 继续现有任务；**不**因 wait / 等路由 / 等仲裁而 idle |
| **同项目优先** | v1 假设同一工作目录、共享 `.pmai`；跨机/跨目录后续做 |
| **API 要简单** | 一步工具、少量参数；Agent 不会执行复杂多步 workflow |
| **v1 无自动仲裁** | 线性同步「AI 选下一发言人」与 PM 随机参与 **模型不符**；见 [§8 废弃项](#8-v1-明确废弃) |

---

## 2. 产品隐喻

```
协作主题 (topic)  = 持续活动线程（可跨小时/天），不是离散同步会议
discussion_log   = 共享记忆 / 审计录音
PM               = 巡视员 + 决策人（写实质内容），不是 SMTP 服务器
Agent            = 各自窗口工作；Hook 无感写入
工具             = 帮 PM 附上下文、帮 Agent / PM 互读全文
Skill + Hook     = 行为约定 + 讨论模式告警（v1 告警，不硬拦截 Write）
```

**Agent 身份：** 统一使用 Hook 的 `source`（`claude-code`、`cursor` 等）。PM 从 `DISTINCT source` 列表选择，不自由输入字符串。

---

## 3. 协作场景

| 场景 | PM 参与 | v1 支持 |
|------|---------|---------|
| **协调 + 低路由**（默认） | 定题、巡视、`catchup`、`prompt` 实质一句、拍板 | ✅ 主路径 |
| 高参与设计评审 | 逐条手工路由、反复引导 Agent 行为 | ⚠️ 可用但不优化 |
| 全自动轮流会议 | PM 几乎不在；AI 自动轮转 | ❌ v1 out of scope |

**PM 参与定义：** 参与 **议题与质量**；不参与 **找上下文、复制长文、教 Agent 用 API**。

---

## 4. PM 工作流（v1 目标体验）

```
1. aipmc topic create --title "..." --plan-id xxx
2. 各 Agent 窗口已通过 aipmc init 加载 pmai Skill（一次性）
3. PM 巡视循环：
     a. aipmc topic catchup --topic xxx     # 30 秒补课
     b. aipmc topic prompt --topic xxx --to cursor \
          --refs latest:claude-code \
          --say "同意 plan+时间窗，请补充跨目录风险"
     c. 复制输出 → 粘贴到 Cursor 窗口 → 离开
4. Agent 工作，Hook 写入 discussion_log
5. PM 随时回来重复 3；Agent 不因 PM 缺席而空转
6. aipmc topic close --topic xxx           # 可选 --summarize；提示是否 record decision
```

**低路由含义：** PM 在 `prompt` 里 **只写 `--say` 一句**；上下文附录与 Agent 须知由 CLI 生成。

---

## 5. P0 能力

### 5.1 `aipm_read_discussions`（MCP；REST/CLI 同构）

**设计目标：** 一个工具、一个心智模型；一步拿到全文。替代 Agent 直查 `sqlite3`。

**v1 参数（5 个，不再增加）：**

| 参数 | 说明 |
|------|------|
| `source?` | 看谁：`claude-code` / `cursor` / … |
| `last_n?` | 最近 N 条（与 `since` 二选一或组合：有 `since` 时在时间窗内取最近 N 条） |
| `since?` | ISO 时间下限；**`topic catchup` 依赖此参数** |
| `full?` | `false`（默认）预览 ~200 字；`true` 全文 |
| `topic_id?` | 可选；限定协作主题的时间窗 + plan 绑定范围 |

**v1 不暴露（实现层默认）：**

- `include_tools` → 默认 **false**（仅 user/assistant 正文；tool 行折叠）
- `query`、`expand_session`、`until`、`limit` → v2

**典型调用：**

```
# Agent 互读
aipm_read_discussions(source="cursor", last_n=10, full=true)

# PM / catchup 补课
aipm_read_discussions(since="2026-06-15T21:48:00", topic_id="topic-xxx", full=false)
```

**与现有 `aipm_search_discussions`：** 合并实现；旧名可作 alias，Skill 与文档只写 `read_discussions`。

---

### 5.2 `aipmc topic` CLI（无 Web）

#### `topic create`

```bash
aipmc topic create --title "Multi-Agent 协作模型" --plan-id plan-xxx
```

创建协作线程；写入 `pm_last_visit_at = now`；物理表可仍用 `meeting_rooms`。

#### `topic catchup`（P0，优先于 prompt）

```bash
aipmc topic catchup --topic topic-xxx
```

等价于：

- 更新/读取 `pm_last_visit_at`
- `read_discussions(since=pm_last_visit, topic_id=xxx, full=false)` 按 source 分组
- 输出示例：

```text
自你 21:48 离开后:
  claude-code  +2 条  (最新: "取消自动仲裁…")
  cursor       +3 条  (最新: "v1 设计文档…")
```

#### `topic prompt`（低路由核心）

```bash
aipmc topic prompt --topic topic-xxx --to cursor \
  --refs latest:claude-code \
  --say "同意 plan+时间窗，请补充跨目录 sync 风险"
```

**输出结构（PM 复制粘贴到目标 Agent 窗口）：**

```markdown
[协作上下文 - 自动附加]
--- claude-code 自上次路由 (2 条) ---
disc-xxx 21:55  (全文或预览)
...

[PM 指令]
同意 plan+时间窗，请补充跨目录 sync 风险。

[Agent 须知 - 来自 Skill]
讨论模式：勿改代码。互读用 aipm_read_discussions(full=true)。禁止 sqlite3。
```

`--refs` 支持：`latest:<source>`、`disc-id` 列表、或 `since-last-route`（P1）。

#### `topic close`

```bash
aipmc topic close --topic topic-xxx [--summarize]
```

- **不强制** `record_decision`；关闭前提示：`未记录 decision，确认关闭？`
- 可选 AI 摘要（P2）

---

### 5.3 Skill：`pmai-collab`（并入 `skill.go` / `pmai.md`）

```markdown
## 协作模式（默认）

- 互读：aipm_read_discussions(source="...", last_n=10, full=true)
- 禁止 sqlite3 读取 .pmai/data/pmai.db
- 讨论模式：禁止创建/修改代码；仅分析、记录、调 MCP
- 回应：① 引用对方一点 ② 明确同意/反对 ③ 结论或开放问题

## v1 不包含

- aipmc wait 循环
- 自动仲裁 / turn 状态机
- register_agent / respond_in_meeting（MCP 已卸注册）
```

---

### 5.4 MCP meeting 工具（v1）

**从 Agent 可见 MCP 注册中移除：**

- `aipm_register_agent`、`aipm_confirm_attendance`
- `aipm_get_meeting_turn`、`aipm_respond_in_meeting`、`aipm_speak_in_meeting`
- `aipm_arbitrate_next`

CLI / HTTP / 表结构可保留 **deprecated**，文档与 Skill 不再引导使用。

---

## 6. P1 / P2 路线图

| 优先级 | 能力 |
|--------|------|
| **P1** | `route_log`：`topic prompt` 自动记录 pm→to、refs、say |
| **P1** | `--refs since-last-route` |
| **P1** | 讨论模式 Hook **告警**（Write 非白名单路径 → 写入 discussion，PM 巡视可见） |
| **P2** | Web 协作主题页（`catchup` + `prompt` 可视化） |
| **P2** | `read_discussions(query=)`、`expand_session` |
| **P2** | 关 topic AI 摘要；跨目录 `aipmc push` |
| **—** | 自动仲裁、默认 wait → **见 §8，v1 不做** |

---

## 7. 数据模型（概念）

### `collaboration_topic`（表名 v1 仍可为 `meeting_rooms`）

```
id, title, goal?, plan_id,
started_at, closed_at?,
pm_last_visit_at,
status: active | closed
```

### `route_log`（P1）

```
id, topic_id, created_at,
to_source, ref_discussion_ids[], pm_say, prompt_snapshot
```

### `discussion_log`（已有，核心）

Hook 自动写入；所有互读与 catchup 的数据源。

---

## 8. v1 明确废弃

| 项 | v1 状态 |
|----|---------|
| 回应后 8s **自动仲裁** goroutine | **不做** |
| AI **自动**选下一发言人 | **不做** |
| Agent 必须 **register + wait** 才能协作 | **不做** |
| **turn** 状态机作为默认流程 | **不做** |
| **`aipmc wait`** 作为产品路径 | **不出现在 v1 文档与 Skill**；二进制可 deprecated 保留供实验 |
| Web Meetings **实时**聊天作为 v1 | **不做** |

原设计见 [`MEETING_DESIGN.md`](MEETING_DESIGN.md)（归档）。

---

## 9. v1 试跑成功标准

**不看 PM 消息条数**，看 **消息类型占比**：

| 类型 | 目标 |
|------|------|
| 物流型（去看/去读/复制/用 API） | **0%** |
| 行为引导型（别写代码/用 XX 工具） | **0%**（Skill + Hook 覆盖） |
| 实质型（观点/追问/决策/纠偏） | **100%** |

**附加指标：**

- Agent 读全文 via `read_discussions` ≥ 80%（不再 sqlite3）
- 讨论模式下违规 Write：告警可见，目标 0 次未纠正
- 关 topic 时至少有 0–1 条 `decision`（不强制，但 Best practice）

---

## 10. 附录 A — 2026-06-15 决策纪要

### 背景

约 82 分钟三方讨论（PM + Claude Code + Cursor）；discussion_log 记录完整交互；**未使用** wait / turn / 自动仲裁，仍达成架构共识。

### 决策

1. 定位：第三方 multi-agent **协调系统**，非传统可控 Agent 会议室。  
2. 默认：**Hook + discussion + 低路由 PM 参与**。  
3. PM 模型：**巡视员**；`topic catchup` 支持随机参与。  
4. **取消 v1 自动仲裁**；wait **不进 v1 产品叙事**。  
5. **`read_discussions`**：5 参数，一步全文；Agent 禁止依赖 sqlite3。  
6. **`topic prompt`**：PM 只写实质一句；工具附上下文。  
7. **Skill + Hook 告警**（v1 不硬拦截 Write）。  
8. 聚合：**plan_id + 时间窗**；不用 commit 作锚点。  
9. UI/CLI：**发言层默认、tool 层折叠**。  
10. **source 统一**；MCP meeting tools **卸注册**。  
11. **P0**：`read_discussions` + `topic catchup` + `topic prompt`。  
12. 物理表 **v1 不改名**；对外文案用 collaboration / topic。

### 已拍板（原「未决」）

| 项 | 决定 |
|----|------|
| Hook 告警 vs 硬拦截 | **v1 告警** |
| close 强制 decision | **不强制，关闭前提示** |
| meeting_* 表 rename | **v1 不做 migration** |

---

## 11. 实现顺序

| 步骤 | 内容 |
|------|------|
| 1 | 本文档落库 ✅ |
| 2 | `read_discussions` MCP + 合并 search 实现 |
| 3 | `aipmc topic create / catchup / prompt / close` |
| 4 | 更新 `skill.go` 协作节；MCP 卸 meeting 注册 |
| 5 | P1：`route_log`、Hook 讨论模式告警 |
| 6 | 真实 topic 试跑，对照 §9 成功标准 |

---

*关联 Roadmap: rdm-20260606-105154-2ab007 (多 Agent 协作)*  
*最后更新: 2026-06-15*
