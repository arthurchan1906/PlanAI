# 会议室模块 (`aipmc/meeting`)

多 Agent 会议协作的领域层。完整设计见 [docs/MEETING_DESIGN.md](../docs/MEETING_DESIGN.md)。

## 当前状态

**骨架已就位，核心流程尚未完整实现。** 现有代码主要是早期原型：

| 能力 | 状态 |
|------|------|
| 会议室 CRUD | 部分（`store/meeting.go`） |
| Turn 状态机 waiting→processing→responded | 部分（MCP get/respond 有，缺自动清理） |
| PM typing 暂停仲裁 | 部分（API `/typing`，缺定时仲裁） |
| AI 自动仲裁 | 部分（`ArbitrateNext`，缺 8s 触发器） |
| `aipmc wait` 轮询 | 已实现（`wait.go`） |
| Web UI 会议聊天 | 原型（`frontend/.../MeetingsView.jsx`） |

## 目录结构

```
meeting/
  status.go       — 状态常量
  turn.go         — 点名 / PM 发言 / Agent 主动发言
  wait.go         — aipmc wait CLI
  arbitrator.go   — 仲裁 prompt + pickNextSpeaker
  arbitration.go  — ArbitrateNext 编排（store + AI）
  README.md
```

持久化仍在 `store/meeting.go`（表：`meeting_rooms`, `meeting_turns`, `meeting_participants`）。

## 入口映射

| 入口 | 应调用 |
|------|--------|
| `aipmc wait` | `meeting.RunWaitCLI` |
| REST `/pmai/meetings/*/typing` | `meeting.SetPMTyping` |
| REST `/pmai/meetings/*/arbitrate` | `meeting.ArbitrateNext` |
| MCP `aipm_arbitrate_next` | `meeting.ArbitrateNext` |
| MCP `aipm_speak_in_meeting` | `meeting.AgentSpeak` |

## 待实现（按设计文档）

1. `processing` 超过 5 分钟 → 重置为 `waiting`
2. Agent 回应后 8 秒内无人发言 → 触发 `ArbitrateNext`（需尊重 `pm_typing`）
3. 会议创建表单的 `auto_arbitrate` / `meeting_mode` 字段贯通
4. MCP `handleGetMeetingTurn` 迁入本包（格式化 briefing 文本）
5. 前端 MeetingsView 轮询 / WebSocket 刷新 turns

## 开发约定

- **业务逻辑**放 `meeting/`，不要散落在 `api/`、`mcp/`、`main.go`
- **SQL / CRUD** 放 `store/meeting.go`
- **AI 仲裁 prompt** 在 `meeting/arbitrator.go`（依赖 `ai.Client.Summarize`）
