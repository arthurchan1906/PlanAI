# 会议室模块 (`aipmc/meeting`)

多 Agent 协作的 **早期会议原型**（turn / wait / 仲裁）。**v1 产品默认路径**见 [docs/COLLABORATION_DESIGN.md](../docs/COLLABORATION_DESIGN.md)。

原实时会议设计（已归档）：[docs/MEETING_DESIGN.md](../docs/MEETING_DESIGN.md)。

## v1 与本文档的关系

| v1 默认（协作） | 本包（归档原型） |
|----------------|------------------|
| `topic` + `catchup` + `prompt` CLI | turn 状态机、wait |
| `read_discussions` | MCP get/respond/arbitrate |
| 无自动仲裁 | `ArbitrateNext` |
| PM 巡视员 | PM 同步点名 |

本包代码 **保留**供实验与后续可选「会议模式」；**不在 v1 Skill / MCP 注册中引导**。

## 当前代码状态

| 能力 | 状态 |
|------|------|
| 会议室 CRUD | 部分（`store/meeting.go`） |
| Turn 状态机 | 部分（MCP get/respond） |
| `aipmc wait` | 已实现（`wait.go`，v1 不文档化） |
| AI 仲裁 | 部分（`arbitration.go`，v1 不做自动触发） |
| Web UI MeetingsView | 原型 |

## 目录结构

```
meeting/
  status.go       — 状态常量
  turn.go         — 点名 / PM 发言 / Agent 主动发言
  wait.go         — aipmc wait CLI
  arbitrator.go   — 仲裁 prompt
  arbitration.go  — ArbitrateNext
  README.md
```

持久化：`store/meeting.go`（`meeting_rooms`, `meeting_turns`, …）。

## 开发约定

- 新业务逻辑优先写在 v1 协作路径（`discussion`、`topic` CLI），而非扩展 turn/wait
- 本包仅 bugfix / deprecated 维护，直到 v2 重新评估「结构化轮流发言」场景
