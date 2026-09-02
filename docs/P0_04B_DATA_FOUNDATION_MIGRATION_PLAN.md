# P0 ④b 数据地基迁移方案

> 归属：P0 四层计划 `docs/P0_FOUR_LAYER_PLAN.md` **层④ Phase B** | 关联 `task-20260828-171125-67755b`（P0）| 关联决策 `decision-20260902-133133-d776ed`（Goodhart 分离，§7 执行细则）
> 状态：**proposal（待用户确认后开工）**。节奏：今天出方案，明天一整块做扎实（Claude 共识）。
> 依据：`db/db.go:375` bugs 表 schema + 四层计划 Phase B 目标 + ED 最高频需求（验证台账）。

## 0. 目标

补齐层④「工具输出形状」的**数据地基**，让 `agent_briefing` 上下文卡的确定性关联可复算、并补上北星计分板（D1）的证据/理由捕获。范围 = **三件事**：

1. `bugs.task_id` 外键（去 2 跳转，bug→task 直连）
2. **验证台账实体**（`verification_log`，scene × device × KSN × result）——ED 最高频需求，AIPM 零结构
3. MCP 接口暴露 + 存量数据回填

**明确不做**（本次范围外，见「风险」）：`bugs.files` 文本索引化（脆弱但非本 Phase 阻塞）、语义「相关」匹配（四层计划 Phase C 排除）、注入 provenance 标记位（是独立小改）。

## 1. 现状 schema（实锤）

```
COMMITS  (已有 task_id, decision_id, files_json)
BUGS     id, title, description, severity, status, commit_id(FK), error,
         files(TEXT, 逗号分隔, 无索引), root_cause, fix, tags, created_at, updated_at
VERIFICATION_* 不存在（仅 commits.test_status 布尔）
```

- bug→task 当前需 **bugs→commits→task 两跳**（四层计划标注 ⚠️ 绕）。
- 验证台账（scene/device/KSN/result）**零结构**（❌ 需新建）。
- `bugs.files` 逗号分隔文本、无索引（⚠️ 脆弱，本次不动）。

## 2. 变更设计

### 2.1 `bugs.task_id`
- 列：`task_id TEXT`，`FOREIGN KEY(task_id) REFERENCES tasks(id)`，可空。
- 写路径：`aipm_record_bug` 新增可选 `task_id` 参数（bp：`store.RecordBug` 签名加 task_id，空则容错）。
- 读路径：`agent_briefing` / context card 用 `bug.task_id` 直连 task，替代两跳。
- 回填：
  ```sql
  UPDATE bugs SET task_id =
    (SELECT c.task_id FROM commits c WHERE c.id = bugs.commit_id)
  WHERE commit_id IS NOT NULL AND commit_id != ''
    AND (task_id IS NULL OR task_id = '');
  ```
- 回填后校验：`SELECT COUNT(*) FROM bugs WHERE commit_id IS NOT NULL AND task_id=''` 应≈0；无法解析的（commit_id 无对应 commit）记报告。

### 2.2 `verification_log` 实体
- 表：
  ```sql
  CREATE TABLE IF NOT EXISTS verification_log (
    id TEXT PRIMARY KEY,
    scene TEXT NOT NULL,           -- 验证场景（session 开工/commit/大变更等）
    device TEXT NOT NULL DEFAULT '',  -- 目标设备/平台
    ksn TEXT NOT NULL DEFAULT '',     -- 密钥序列号/标识
    result TEXT NOT NULL,          -- pass / fail / skip
    detail TEXT NOT NULL DEFAULT '', -- 证据/理由（补「证据在源点被丢弃」缺口）
    session_id TEXT NOT NULL DEFAULT '',
    project TEXT NOT NULL DEFAULT '',
    task_id TEXT,
    created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
    FOREIGN KEY(task_id) REFERENCES tasks(id)
  );
  ```
- store：`CreateVerificationLog` / `ListVerificationLogsByProject` + 需要时 `GetLatestByScene`。
- model：`db/models.go` 加 `VerificationLog` struct + schema_guard 白名单表。
- MCP：新增 `aipm_record_verification`（写入）+ `aipm_list_verifications`（查询，project/task 过滤）。
- **设计取舍（待确）**：schema 用「scene/device/ksn/result」四要素（对应 ED 需求）还是更通用的 `kind + key + value`？——**先按四要素**（接地气、可直接喂上下文卡），通用化作为后续演进，避免过度设计。

### 2.3 上下文卡接线
`agent_briefing`（`analyze/agent_briefing.go`）把 `bug.task_id` 直连 + 最近 `verification_log` 刷进上下文卡「相关 bug / 验证台账」section。

## 3. 迁移步骤（明天一整块）

1. schema：`db/db.go` 迁移函数加 `bugs.task_id` 列 + `verification_log` 表（guarded `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` 语义，迁到 `migrate_test.go` 覆盖）。
2. model + store：`RecordBug` 加 task_id；新增 `CreateVerificationLog`/`ListVerificationLogsByProject`；补 `store_test.go`（PMAI_HOME 隔离，见 decision 2026-08-26 写路径测试）。
3. MCP：`mcp/mcp.go` 加 `aipm_record_verification`/`aipm_list_verifications`；`aipm_record_bug` 加可选 `task_id`。**遵守 checklist（decision 2026-08-18 新工具/MCP 上线 checklist，含热路径 prefix-cache 敏感性检查）**。
4. 回填脚本：`UPDATE bugs SET task_id=...` 跑 + 报告（可复算）+ 失败清单。
5. 上下文卡接线 + `agent_briefing` 测试。
6. `go build ./... && go test ./store/... ./db/... ./mcp/...`。
7. `go vet ./...`。

## 4. 验收指标

- bug→task 直连命中率（回填后 `bugs.task_id` 非空占比，目标 ≥ 现状两跳可解析率）。
- `verification_log` CRUD 全通过 + schema_guard 白名单通过。
- `agent_briefing` 上下文卡「相关 bug」用 task_id 直连（不再两跳）。
- 回填报告：原始 commit_id 数 / 直连数 / 失败数（≤ 预期）。

## 5. 风险

- **bugs.files 无索引**：本次不动，但上下文卡若按文件查 bug 仍脆弱——记为层④ Phase B 后续项（单独 task）。
- **provenance 标记位**（注入诱发 vs 自发）：是独立小改，留给 P0①/④b 顺带；本方案不扩产。
- **回填不可解析的 commit_id**：如实报告（可能为历史脏数据），不强行指派 task。
- **新 MCP 工具热路径敏感性**：`aipm_list_verifications` 若高频走 prefix-cache 热路径，须评估；写入工具低频无碍。

## 6. 待确认

- [ ] verification_log 字段用「scene/device/ksn/result」还是通用 `kind/key/value`？（默认四要素）
- [ ] 范围是否含「bugs.files 索引化」？（默认否，独立后续）
- [ ] 是否今天只做方案、明天整块开工？（Claude 共识：是）
