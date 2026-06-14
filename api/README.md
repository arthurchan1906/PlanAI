# HTTP API (`aipmc/api`)

Web UI 与外部集成的 REST 入口，挂载于 `/pmai/*`。

## 结构

| 文件 | 职责 |
|------|------|
| `server.go` | `Server`、路由分发顺序 |
| `deps.go` | 注入 `*app.App` 共享服务 |
| `entity.go` | 单实体 GET/PATCH（`/pmai/tasks/{id}` 等） |
| `lists.go` | 资源列表 GET |
| `create.go` | POST 创建与嵌套路由 |
| `query.go` | search/dashboard/discussions/config 等 |
| `web.go` | `GET /pmai/web/*` 按页面拆分的数据接口 |
| `bootstrap.go` | `GET /pmai/web/bootstrap` 已废弃，返回迁移提示 |
| `chat.go` | Code Agent 会话 API |
| `config.go` | AI/Web 配置读写 |
| `patch.go` | PATCH/DELETE、会议 typing |
| `util.go` | 请求体解析、git 辅助 |

## 扩展新接口

1. 在对应域文件增加 `func (s *Server) handleXxx(...)`，或新建 `api/xxx.go`。
2. 在 `server.go` 的 `ServeHTTP` 中按**具体路径优先**插入调用（参考现有顺序）。
3. 业务逻辑优先放在 `store/`、`search/`、`project/`、`agent/` 等包，`api` 层只做 HTTP 适配。

## 依赖

- `app.App` — CLI/Web/MCP 共享的运行时（AI 客户端、搜索、快照等）
- `store` — 数据库 CRUD
- `web.SendJSON` / `web.SendError` — 统一响应格式

## Chat API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/pmai/chat/sessions` | 会话列表 |
| GET | `/pmai/chat/session?id=` | 加载会话 |
| POST | `/pmai/chat/send` | 发送消息，触发 `agent.ChatService` |
