# SQLITE_BUSY 根因诊断报告 — 2026-08-27

> D 线产出物 1（task-20260827-111104-c39042，决策 `decision-20260827-144909-c3d06b`）。
> 数据口径：`~/.aipmc/logs/aipmc.log` [MCP]/[INJECT]/[HOOK] 行（8/14-8/27）。

## 1. 时间线（三次排查，仍在复发）

| 时间 | 结论 | 缺口 |
|---|---|---|
| 8/17 | "261 次 SQLITE_BUSY" 排查 | **误读**：261 是扩展错误码 `SQLITE_BUSY_RECOVERY(5|1<<8)`，不是次数（8/25 纠正） |
| 8/25 | store 裸调用无 retry、busy_timeout 覆盖 261 无效 | 只补了诊断，未全覆盖 |
| 8/26-8/27 | **仍复发**：inject_log write_err、HOOK post-commit、MCP-ERR read_discussions | C2 14:00 补 store 读路径 3 处 + CreateDecision 后，**14:00 后全日志 locked 失败归零** |

## 2. 根因分层（三层叠加）

**架构层：WAL 单写者 × 多进程 × 多连接池**
- 数据库 `journal_mode=WAL`：**同一时刻只有一个写事务**，所有写串行化。
- 写方至少 4 类进程：proxy（每请求写 inject_log + file_assoc 读）、pipeline（30m auto-run 批量 L2）、hook（每 commit 写）、MCP server（agent 每次工具调用写实体）。
- 代码里 **145 处 `pmdb.Open*` 调用点**（store 82 + discussion 15 + session 7 + mcp 4 + …），每次操作 `sql.Open` 新连接池 + `defer Close`——连接反复重建，无进程内复用。

**代码层：BUSY_RECOVERY 不等待 + 裸写无 retry**
- `busy_timeout(15000)` 只覆盖普通 `SQLITE_BUSY(5)`；`SQLITE_BUSY_RECOVERY(261)` 的 busy handler **不等待**，必须调用方重试（db.go:185-191 已注明）。
- C2 前 store 大量写操作裸 `db.Exec` 无 retry：多 agent 并发写决策/任务时 261 直接失败（8/27 10:50 HOOK、11:10-11:21 inject_log 实测）。

**行为层：注入热路径高频写竞争**
- codex 双 session 密集工作时段（8/27 11:05-11:25）每 10-20s 一次 file_assoc + inject_log 写，与 MCP 工具写、hook 写撞车（8/26 09:07-10:14、8/27 11:10-11:21 集中爆发）。

## 3. 实测数据

- 主日志 locked 事件按天：8/17=6、8/18=10、8/19=21、8/20=20、8/24=27、8/25=10、8/26=15、**8/27=11**。
- 8/27 分布：09:59 search_context（非失败）、10:21 **MCP-ERR read_discussions**（C2 前）、10:50 HOOK post-commit (261)、11:10/11:19/11:21 inject_log write_err（EncryptDrive）。
- **8/27 14:00（C2 生效）后：locked 失败事件 = 0**（[MCP]/[INJECT]/[HOOK] 全路径）。唯一含 "SQLITE_BUSY" 的 [MCP] 行是查询词（status=OK）。

## 4. 已生效修复（14:00 后归零的构成）

1. WAL + `busy_timeout(15000)` + `synchronous(NORMAL)`（db.go Open 系列）
2. `EnsureSchemaIfNeeded`：user_version 检查跳过 DDL（8/5 bug-20260805-134225-4f214f 修复，DDL 写锁风暴消除）
3. `pmdb.RetryBusy`：3 次指数退避（100/200/400ms），覆盖 BUSY_RECOVERY（db.go:191）
4. C2（8/27 14:00）：`ReadDiscussions`/`ListRecentDiscussions`/`ListActiveSessions` 读路径 + `CreateDecision` 写路径包 RetryBusy

## 5. 三选一决策（accepted，`decision-20260827-144909-c3d06b`）

| 方案 | 裁决 | 理由 |
|---|---|---|
| A 进程内复用连接 | **辅** | 减少 Open/Close 抖动，但**不解决跨进程写竞争**（proxy/pipeline/hook/mcp 是不同进程） |
| B 加 retry 全覆盖 | **主** | C2 已验证有效（14:00 后 MCP 层 0 locked）；剩余裸写点（Update*/Record*/Link*/mark 类）统一包 RetryBusy |
| C 降级 WAL | **否决** | 回滚 journal 读写互斥，并发更差 |

**验收锚点**：连续 3 天 [MCP] 日志无 SQLITE_BUSY——8/27 14:00 起已开始计数，**8/30 复验**。INJECT/HOOK 路径 locked 属注入域（非 MCP 验收），转 T3b/日志规范线跟踪。

## 6. 后续实施（B 线：retry 全覆盖清单）

- store 写操作：`UpdateTask*`/`UpdatePlan`/`UpdateBug`/`UpdateCommit`/`RecordBug`/`LinkEntities`/`AddToThread`/`MarkConsumed`/`MarkEventProcessed` 等裸 `db.Exec` → 包 `retryOnBusy`。
- MCP 写路径由 store 覆盖即生效（mcp/mcp.go 调用 store 函数）。
- 高频读（file_assoc edges）：确认只读路径在 WAL 下不锁，异常时包 RetryBusy 兜底。
- 进程内连接复用（辅）：`inject_log`/`file_assoc` 热路径单例连接池（独立 task）。
