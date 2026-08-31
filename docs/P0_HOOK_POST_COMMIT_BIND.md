# P0 ① 确定性代码钩子：post-commit 自动绑定（2 周主线终点）

> 状态：**draft**（Claude 8/31 17:05 背书「先落盘方案再实现」）
> 依据：`docs/P0_FOUR_LAYER_PLAN.md` ①层 + `docs/D1_ATTRIBUTION_PROTOCOL.md` §7.1 四层失效模型 + `hook/post-commit.go` 现状 + 8/26 约束 A/B + 8/31 指标快照
> 原则：**「prompt 注入会被压缩吃掉，代码钩子不会」**（ED 8/28 实证）；只记行为事实、不注入检测结论（约束 A）。

## 0. 一句话目标

把「agent commit 后忘了 `aipm_record_commit` 绑 task」这一自动化缺口闭合——post-commit 钩子在**高置信确定性匹配**时自动把 commit 绑定到 task，**否则留空 + 提示**（不替 agent 做决策、不注入检测结论）。直接降低 `commit_orphan`（当前 78/182）并抬升 `D2/F1 事件→动作漏斗`（29%→≥40%）。

## 1. 这条主线是「2 周演进」的终点（回链脉络）

P0 ① 不是今天的新发现，而是最近两周主线的**自然终点**：

```
8/12 三方收敛    → 注入对 codex 出局（到达率 4.7% + cap=800 压制 + 无写前钩子）
8/18 决策        → fileAssoc 500B 独立预算（注入了但内容被挤）
8/25 INJECT 去重 → same_content 语义（注入「发生」≠ 注入「到达」）
8/26 约束 A/B    → 只注入行为事实、不注入检测结论（Goodhart 边界）
8/26 自我评估    → AIPM 最强=协作可见性，最弱=主动帮助（记录仪 vs 助手）
8/27 十轮收敛    → 第一交付物 =「让 agent 想起来用 AIPM 的机制」（软触发 1-6）
8/28 D1 协议     → 三层失效根因（动作点/规范语义/compaction）→ 四层模型
8/28 ED 实证     → 「prompt 注入会被压缩吃掉，代码钩子不会」
8/31 codex+Claude → P0 ① 确定性代码钩子（硬确定性，自动用）
```

**关系**：8/27 的「想起来」（软触发机制 1/3/4）是软层；ED 8/28 实证软触发会被 compaction 吃掉；故演进为 P0 ① 硬钩子——**不是替代，是演进**。

## 2. D1 四层对应

| D1 层 | 落地 | 状态 |
|---|---|---|
| ① 动作点未固定 | P0 ① 确定性代码钩子 | **本方案**（v1 record_bug 提示已落地 77c2243；剩余=post-commit 自动绑） |
| ② 规范语义模糊 | P0 ② guidelines 工具直映射 | in_progress |
| ③ compaction/到达率 | E 线段优先级治理 | ✅ 已落 + 实证（warn/act 被裁=0、guidelines 保底 202B） |
| ④ 工具输出形状 | P0 ④a agent_briefing 上下文卡 | ✅ 已上线（确定性关联） |

## 3. 根因（代码级）

`hook/post-commit.go` 的 `ProcessPostCommitHook()` → `store.StoreGitCommit(projectPath, title, hash, date, files)` 插入 commit 时 **`task_id=""`**（见 `store/store.go:720` INSERT 语句），commit 落库但**不绑 task**。需要 agent 再调 `aipm_record_commit(task_id=...)` 补绑才消除孤儿。正是 `commit_orphan 78/182`（仅 43% 被绑）的来源。

## 4. 约束 A/B 边界（8/26 accepted）

- **自动绑定 = 中性记录动作**（把已发生的 commit 挂到 task 上）——符合约束 A（只记行为事实），非「检测结论注入」（不说「你这个 commit 是孤儿」）。
- **不得替 agent 做决策**（Goodhart 边界）：禁止「谁都有可能 → 猜一个绑上」，也禁止「自动更新 task 状态」（那是决策，不是记录）。
- **高置信才绑**，否则留空 + 提示（沿用 v1 的 stderr 提示模式）。

## 5. 设计：post-commit 高置信自动绑定

在 `ProcessPostCommitHook()` 成功 `StoreGitCommit` 后，接着做确定性绑定（先落盘写入，无数据库写入也安全）：

**高置信匹配规则（按优先级，命中即绑、命中多个任务则视为不确定）**：
1. **commit 消息含 task 引用**：正则 `task-\d{8}-\d{6}-[0-9a-f]{6}` 提取 task_id → 绑（确定性、无语义）。
2. **文件唯一命中**：commit 触碰的文件，若恰好唯一命中某个 **in_progress** 任务的 graph 关联文件（复用 `store.ListTaskFileAssoc("in_progress")`），且该任务非空 → 绑；命中 0 个或多个任务 → **不绑**（不确定，转提示）。
3. 均不满足 → **留空 + stderr 提示**（沿用 v1 提示风格，引导 agent 判断）。

**绑定实现**：`store` 新增 `BindCommit(projectPath, commitHash, taskID)`（确定性 UPDATE `commits.task_id`，幂等），供 post-commit 与未来共用；绑定成功记 `[HOOK] status=BIND` 日志。

**防误绑**：只对**本次刚刚插入/更新的 commit**绑定；不为历史 commits 回填（避免大面积改写既有归属，防 Goodhart 反噬）。

## 6. 验收

- `commit_orphan` 计数下降（182 → 显著低于 43% 未绑率）。
- `D2/F1 事件→动作漏斗` 29% → ≥40%。
- `aipm_record_commit` 中的「补绑孤儿」占比下降（自动绑占用量）。
- 回归测试：`TestProcessPostCommitAutoBind`（msg 含 task 引用绑 / 文件唯一命中绑 / 多处命中不绑 / 无匹配留空提示）。
- `go vet ./...` + `go test ./...` 全过。

## 7. 实现范围

`hook/post-commit.go`（插入后接自动绑定逻辑）+ `store/store.go`（`BindCommit` helper）+ `hook/post_commit_test.go`（回归）。
