# E 线：注入段优先级治理方案（draft）

> 状态：**draft**（待 Claude 审核，重点看「写序重排 + 预算保底 + 验收」三处）
> 真实来源：`docs/agent-collab-design.md` §5.3 条件段设计 + §5.3.1 预算重构（Phase 1a）+ 8/18 决策（`task-20260818-134522-c6d5e9`，fileAssoc 200→动态上限 500）
> 背景：P0 ③ 重定义后并入本 E 线（`decision-20260831-121601-4b3768`）。本方案是 E 线「注入块到达率/裁剪治理」核心实现。

## 1. 问题：文档与实现漂移

| 维度 | 表定（§5.3） | 当前实现 | 漂移 |
|---|---|---|---|
| 写序 | 文件碰触第一优先、fileAssoc 200 豁免 | `[AIPM Context]→[文件关联](:602)→[当前进行任务](:622)→[项目编码规范](:642)→warn→act→goals→vision` | 文件关联写序在前但**无真正保底** |
| 预算 | guidelines 600 独立、fileAssoc 200 豁免 | guidelines 600 先占（:642）、fileAssoc 动态 ≤500、warn/act 仅靠 reserve 200 | guidelines 600 挤占 warn/act/fileAssoc |
| 数据 | — | 8/27 warn=7631 / act=4800 / file_cut=9404；`inject_log` 全局裁剪 49.7%、8/31 94% | 高价值段（warn/act/fileAssoc）被挤 |

**核心**：表定「文件碰触/文件关联高优先」与实现「guidelines 先占」矛盾——即项目长期存在的「文档与实现漂移」（与 M1a 对账、D1 口径同族）。

## 2. 段优先级定案（修订 §5.3 表）

基于 8/18（fileAssoc 高优）+ 8/27（warn/act 被挤），**高优 =「agent 当下任务上下文」**，guidelines/goals 降为可让位段：

| 段 | 优先级(新) | 预算 | 保底 |
|---|---|---|---|
| [文件关联] fileAssoc | 1 | ≤500 动态（8/18） | ✅ `min(200+30n,500)`，且先写 |
| [当前进行任务] anchor | 2 | 180 | ✅ `anchorBudget=180` + cap guard |
| ⚠️ warn/act | 3 | reserve 200+ | ✅ `warnActReserve=200`（在 guidelines 前预留） |
| [项目编码规范] guidelines | 4（降） | 剩余 | ❌ 用剩余、被裁时先裁 |
| 最近的 session goals | 5（降） | 剩余 | ❌ 有空间才写 |

**策略调整**：guidelines 从「600 独立（恒定）」降为「可让位段」——通用规范价值低于实时任务上下文，且 600B 全量常是占位（Claude 已同意 8/31）。

## 3. 写序重排设计

新写序（高优先先写、占预算；低优最后写、被裁优先）：

```
[AIPM Context] → [文件关联](fileAssoc) → [当前进行任务](anchor)
→ warnings → ⚠️待处理(actionItems) → [项目编码规范](guidelines)
→ [最近的 session](goals) → [vision tip]
```

- **高优段**（fileAssoc/anchor/warn/act）先写，各自设保底（现有实现已具备：fileAssoc `min(200+30n,500)`、anchor 180、warn/act reserve 200）。
- **低优段**（guidelines/goals/vision）最后写、被裁时先裁——把 guidelines 从「独立 600B 先占」改为「在 warn/act 之后用剩余预算」。
- **全序确定性**：重排为一次性确定性写序，部署后 block 逐字节稳定，fullHash 不破 SP 缓存（与 8/18 fullHash 确定性保证一致）。

## 4. 写序与预算一致性检查表（防再漂移，Claude 8/31 建议）

> 每次改动 `buildContextBlock` 段后，逐行对照下表；任何一行不一致即为回归。

| 段 | 表定优先级 | 写序位置 | 保底预算 | 一致性 |
|---|---|---|---|---|
| fileAssoc | 1 | 第2段（Context header 后） | `min(200+30n,500)` | 目标 |
| anchor | 2 | 第3段 | 180 + cap guard | 目标 |
| warn/act | 3 | 第4/5段 | reserve 200 | 目标 |
| guidelines | 4 | 第6段 | 剩余 | 目标（降级后） |
| goals | 5 | 第7段 | 剩余 | 目标 |

**检查规则**：先在表填「表定优先级」（设计意图），再用测试/日志核对「实际写序/保底」——不一致即 fail。

## 5. 验收

- `inject_coverage ≥80%`（基线 v1.13 期 96.7%，需重启后重采）
- 关键段（warn/act/fileAssoc/anchor）**到达率提升**（对照 8/27 warn=7631/act=4800/file_cut=9404）
- 注入 `chars ≤800`（已达标 758/748，须保持）
- 预算回归：**guidelines 满 600 时高优段（warn/act/fileAssoc）不被挤掉**（§5.3.1 / v1.13 回归）
- 新增 `TestBuildContextBlockPriorities`（段到达校验）

## 6. 实施范围

`proxy/context_inject.go buildContextBlock`（段写序 + guidelines 降级 + 预算归位）+ `proxy/context_inject_test.go`（新增优先级回归测试）。

> 依据：以上（1）段顺序/常量均逐行核对当前源码；(2) acceptance 均引用既有验收口径（§5.3.1、v1.13 §4、8/27 实测）。
