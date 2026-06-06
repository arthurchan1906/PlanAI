# 会议系统设计文档

> 多 Agent 会议协作的完整设计。基于 2026-06-06 讨论总结。

---

## 1. 核心原则

- **PM 驱动会议** — PM 创建会议、点名 Agent、自由发言、决定何时结束
- **Agent 无拒绝权** — 被点名的 Agent 必须回答，即使很简短
- **状态保护** — 正在处理的 Agent 不会被重复点名或 AI 仲裁打断
- **PM 优先** — PM 可随时发言，PM typing 期间 AI 仲裁暂停
- **Agent 身份临时** — 每次注册都是新身份，用完即弃

---

## 2. Agent 身份模型

### 原则

- Agent 身份是**临时的、per-session 的**
- 每次 `aipm_register_agent` 创建新 ID，**不按名去重**
- 会议结束后身份即弃，下次可以换名字/角色重新注册
- PM 在 Web UI 点名时选择当前活跃的 Agent

### 实现

```go
// mcp.go — handleRegisterAgent
// 去掉了"按 name 查找并覆盖"的逻辑，始终创建新资源
```

---

## 3. Turn 状态机

```
PM 点名 → waiting ⏳
Agent 调 get_meeting_turn → processing 🔄
Agent 调 respond → responded ✅
```

### 各状态含义

| 状态 | 含义 | Web UI 显示 | AI 仲裁 | `aipmc wait` |
|---|---|---|---|---|
| `waiting` | Agent 尚未读取 | ⏳ 等待中 | 可以选中 | **能匹配到** |
| `processing` | Agent 已读，正在思考 | 🔄 处理中 | **跳过** | 不匹配 |
| `responded` | Agent 已回应 | 显示回复内容 | 触发仲裁 | 不匹配 |

### 崩溃恢复

- `processing` 超过 5 分钟无变化 → 重置为 `waiting`（待实现自动清理）
- PM 可手动强制重新点名

---

## 4. PM 发言保护

### 机制

`meeting_rooms.pm_typing` 标志位：

| pm_typing | AI 仲裁 |
|---|---|
| 0 | 正常，Agent 回应后 8 秒触发仲裁 |
| 1 | **暂停**，不计时，不仲裁 |

### 操作

Web UI 提供 `[✋ 我要发言]` 开关。PM 点击后仲裁暂停，组织好语言、发送消息后自动解除。也可以手动切换 `[✅ 我说完了]`。

---

## 5. Agent 轮询机制 (`aipmc wait`)

### 原理

```
Agent: aipmc wait --agent-id claude-审查员
  → 阻塞循环，每 2 秒查询 meeting_turns
  → 条件: speaker_id=agent_id AND status='waiting'
  → 找到 → 输出 JSON → 退出
  → 超时（默认 300s）→ 输出 {"status":"timeout"}
```

### 优势

- 秒级感知（2 秒轮询）
- Agent 只在 idle 时被触发
- 不影响正常编码工作流

### 使用方式

```
$ aipmc wait --agent-id claude-审查员
{"status":"turn_waiting","turn_id":"turn-xxx","room_id":"meeting-xxx","question":"..."}

# Agent 拿到输出后:
aipm_get_meeting_turn(room_id, turn_id)  → 使 turn 变为 processing
aipm_respond_in_meeting(turn_id, response) → 使 turn 变为 responded
aipmc wait --agent-id claude-审查员         → 继续等待下一轮
```

---

## 6. 会议工具清单

### Agent 侧 (MCP)

| Tool | 作用 |
|---|---|
| `aipm_register_agent(name, role)` | 临时注册 Agent 身份 |
| `aipm_confirm_attendance(meeting_id, agent_id)` | 确认参会 |
| `aipm_get_briefing(agent_id)` | 检查 inbox + 个性化简报 |
| `aipm_get_meeting_turn(room_id, turn_id, since_turn?, agent_id?)` | 获取上下文 → turn → processing |
| `aipm_respond_in_meeting(turn_id, response)` | 提交回应 |
| `aipm_speak_in_meeting(room_id, agent_id, content, reply_to?, address_to?)` | Agent 主动发言（保留但很少用） |
| `aipm_arbitrate_next(room_id)` | 触发 AI 仲裁 |

### PM 侧 (Web UI)

| 操作 | 效果 |
|---|---|
| 创建会议 | 填主题、背景、模式、仲裁开关 |
| 直接发消息 | 不点名，speaker_type=human |
| 点名 Agent（提问框可选） | 选 Agent，可留空提问框 |
| `[✋ 我要发言]` | pm_typing=1，仲裁暂停 |
| `[🔮 仲裁]` | 手动触发 AI 选下一个发言人 |
| 结束会议 | 关闭会议 |

### CLI

| 命令 | 作用 |
|---|---|
| `aipmc wait --agent-id <id>` | Agent 阻塞轮询 inbox |

---

## 7. 典型会议流程

```
1. PM 创建会议 (Web UI)
   填写主题 + 背景 + 设置模式/仲裁

2. Agent 注册身份 (MCP)
   aipm_register_agent(name="claude-审查员", role="reviewer")

3. PM 点名 Agent (Web UI)
   选 "claude-审查员" → 系统创建 waiting turn

4. Agent 感知并回应
   aipmc wait --agent-id claude-审查员  → 检测到 waiting turn
   aipm_get_meeting_turn               → turn → processing
   理解上下文 → aipm_respond_in_meeting → turn → responded

5. 仲裁或 PM 继续点名
   Agent 回应后等待 8s
   - 有其他 Agent 主动发言 → 继续
   - PM 点名下一个 → 继续
   - PM 点 [✋] 发言 → 仲裁暂停
   - 8s 无动作 → AI 仲裁选下一个

6. 循环 3-5 直到 PM 结束会议
```

---

## 8. 待完成

- [ ] PM typing 前端开关按钮（API 已就绪）
- [ ] processing 状态超时自动重置（> 5min）
- [ ] AI 仲裁定时器（goroutine per meeting）
- [ ] 会议自动摘要（AI 端点配置后）
- [ ] MCP 连接验证与调试

---

*关联 Roadmap: rdm-20260606-105154-2ab007 (多 Agent 协作)*
*关联 Plan: plan-20260606-165231-08b1a7 (会议系统增强)*
*最后更新: 2026-06-06*
