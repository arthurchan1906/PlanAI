# 项目完善方案与计划 v1（2026-08-07）

> 汇总 Codex（评估框架 + 效果分析）、Claude（Q1→Q4 效果链 + 框架审核）、Codex 复核（L2 脏缓存根因）三份意见后形成的共识计划。
> 原则：**修复不急，但排期明确；每个修复必须可复测对应评估目标**（见 `docs/EVALUATION.md`）。

---

## 1. 共识结论

### 1.1 系统是有效的
- 完整效果链 **Q1 数据 → Q2 理解 → Q3 注入 → Q4 行为改变** 是通的
- 已达标：L2 goal 大部分 unnest、L2 缓存 100% 命中、reconcile 709 条链接、事件消费率 94.8%、guidelines 注入、cross-session knowledge、Agent 行为改变有可观测证据（3988 次 MCP 调用 / 主动修 orphan）

### 1.2 最大的系统性问题（Claude 判断，Codex 认同）
> **Q2→Q3 瓶颈**：Pipeline 产出 50 个 emerge 事件，INJECT `cap=3` 只注入 3 个。理解层产出的"记忆"在最后一公里被压缩，Agent 行为改变因此不够显著。

### 1.3 共识问题清单（三份分析交叉验证）

| # | 问题 | 根因 | 评估目标 | 严重度 |
|---|------|------|:--:|:--:|
| P1 | commit_orphan 事件重复率 480%（660 事件/137 唯一） | `dupEvent` 只查 unconsumed，消费后重复创建 | B6 | 🔴 |
| P2 | codex/cursor file-assoc 完全失效（注入 0） | `extractFilePaths` 对非 Claude 请求体解析失败（`body_parse=err` 2104 次/31%） | C2 | 🔴 |
| P3 | FTS5 搜索覆盖缺口（plan/bug 索引为 0） | `CreateBug` 缺 `SyncFTS5Entity`；历史数据未回填 | 搜索质量 | 🔴 |
| P4 | L2 脏摘要被缓存复用（8/63 嵌套 goal 残留 12.7%） | `unnestGoal` 只作用于新生成；缓存读取无兜底 | B2/B3 | 🟡 |
| P5 | 注入率仅 22.7%，50 事件只注入 3 个 | `maxActions=3` + char_limit 800，无优先级排序 | C1/C3 | 🟡 |
| P6 | L2 覆盖率 55% < 85% 验收线 | 51/114 session 无摘要，根因未排查 | B1 | 🟡 |
| P7 | 新旧进程并存，评估数据被稀释 | 旧二进制（含 cooldown）与新版本并发写日志 | E2 | 🟡 |
| P8 | graph_edges 覆盖率 26.7%（233/871 commit） | 设计意图未确认（可能仅处理有 session 关联的 commit） | B5 | 🟡 |
| P10 | 观测缺口：C1 无法按 hash 计算、E4 无法测协议错误 | 成功注入日志无 hash 字段；[LLM] 日志无错误字段 | E1/E4 | 🟡 |
| P11 | L2 摘要准确率未评估 | 无人工抽样验证，幻觉风险未知 | B9 | 🟡 |
| P12 | 无用户反馈回路 | `detectUserFrustration` 存在但未纳入评估 | D4 | 🟡 |
| P13 | 部署流程无防重复机制 | 新旧进程并存靠人工停，下次 deploy 复现 | E2 | 🟢 |
| P14 | mark_consumed 全量消费，D2 指标失真 | `UPDATE events SET consumed=1 WHERE consumed=0` 一次清空全部 | D2 | 🟡 |
| P15 | 6 个 aipmc 进程多实例冗余 + proxy_port 冲突 | 3 个无监听幽灵进程；两项目 projects.json 均配 19530 | E2 | 🟡 |
| P16 | **hook 层静默失败（最大蚁穴，Claude 细节审查）** | 全平台 hook 用 `recover()+os.Exit(0)` 吞 panic 以**成功码**退出，失败仅写 stderr 不写 LogShared——Agent 无感知，L0 捕获静默停止 | A1/E1 | 🔴 |
| P17 | store.go 类型断言无 ok 检查 → panic 风险 | `existing["status"].(string)` 等 4 处（store.go:152/169/271）+ `acc[index]["done"]` nil map 写入（:275） | 稳定性 | 🔴 |

**8/7 Claude 细节审查（补充 Codex 未覆盖的崩溃/静默失败）**
- 4 个 panic/崩溃点 + 1 并发竞争 + 5 静默失败路径（Claude 识别，Codex 计划未覆盖）
- 最大的蚁穴：**hook 层静默失败**——hook JSON 格式变化 → `os.Exit(0)`，L0 捕获对那个 Agent 静默停止，后面 pipeline/INJECT 全白费
- 已验证属实：hook_claude/gemini/codex/opencode 均有 `recover()+os.Exit(0)`；失败仅 stderr 不落日志；store.go:152/169/271 无 ok 断言、:275 nil map 写入

**8/7 细节审计修正（推翻此前审查结论）**
- C2「codex/cursor file-assoc 完全失效」→ **修正**：codex 779 次 / cursor 235 次成功；失败率 claude 17.8% / codex 52.4% / cursor 66.3%，所有 agent 均有 body_parse=err
- E2「新旧进程并存」→ **修正**：当前 6 个进程全部为 dist/aipmc 新版，cooldown 为 17:34 前历史残留；真实问题是多实例冗余
- D2「事件消费率 94.8%」→ **语义修正**：mark_consumed 全量消费，是「已读」非「已处理」

**已关闭/设计范围（Claude Challenge 5 确认，不再作为问题）**
- `trace_context` 低频（27 次）→ **已关闭**：7/28 已确认"Agent 不会主动查图"，转向 pipeline 主动注入
- `graph_edges` 覆盖率 26.7% → **设计范围**：只对有 session 关联的 commit 建边是设计如此（`session/graph.go`）
- cursor 无 L2 → **降低优先级**：仅 4 个无 L2 且消息量少（30-130 条）；真问题是 codex 17 个大 session（500+ 条）无 L2

## 2. 分阶段完善方案

### Phase 0 — 评估能力建设（不写核心逻辑，先让评估可信）
**执行顺序（Claude 审核修正）：0.3 → 0.1 → 0.2**
| 任务 | 说明 | 产出 |
|------|------|------|
| 0.3 统一运行版本 + 启动自检 + 端口冲突检查（最先） | **8/7 细节审计：当前 6 个 aipmc 进程全部为新版，但 3 个无监听幽灵进程 + 两项目 proxy_port 均 19530**。清理幽灵进程、确认端口归属；`main.go serve` 启动自检：检测重复实例 + 端口占用时 warn/退出 | E2 干净：每项目 ≤2 进程 |
| 0.1 B4 reconcile baseline 数据收集（其次） | 已有 task `task-20260727-133003-10d121`：统计自动链接数、置信度分布、**假阳性抽样**（709 条链接里有多少是错的，是评估 L3 质量的前置） | baseline 报告 + EVALUATION.md B4 基线 |
| 0.2 E1 日志查询指南 + 观测缺口补埋点 | 沉淀 tag 清单、查询命令、BSD grep 误用；**补两个埋点**：① INJECT 成功注入日志带 `hash=`（使 C1 可按唯一 hash 计算注入率）② [LLM] 日志加错误字段（使 E4 可测） | docs/LOG_GUIDE.md + 埋点 diff |

### Phase 1 — 数据质量修复（🔴 P1-P4，改动小、收益确定）
| 任务 | 修改点 | 复测标准 |
|------|--------|----------|
| 1.1 `dupEvent` 去重修复 | 检查所有历史事件（含已消费）或加 entity 级去重标记；清理 660→137 重复事件 | B6：重复率 < 10% |
| 1.2 `extractFilePaths` 解析容错 | **修正范围（8/7 细节审计）**：所有 agent 均有 body_parse=err（claude 817/codex 856/cursor 463），非 codex/cursor 专属。需：① 非 JSON body 降级提取 ② 失败日志带 agent 标识 ③ 修 `extractPaths` 对 `os.Getwd()` 的依赖（proxy cwd 非项目根时绝对路径失效） | C2：各协议解析成功率 ≥ 90% |
| 1.3 FTS5 索引补齐 | `CreateBug` 补 `SyncFTS5Entity`；触发 `RebuildFTS5Index` 回填 plan/bug | 搜索：plan/bug 覆盖率 100% |
| 1.4 L2 缓存 unnest 兜底（含容错修复） | **8/7 诊断：8 条残留全部是活 bug**——模拟当前 `unnestGoal` 全部 `PARSE_FAIL`（字符串转义伪 JSON + 无效 UTF-8/截断），简单重跑不能修复。需：① 增强 `unnestGoal` 容错（宽松提取 `"goal":"..."`）② 缓存读取路径补兜底 ③ 清理历史 8 条 | B2：嵌套率 = 0 |
| 1.5 L2 准确率抽样验证 | 随机抽 5 条 L2 对照原始 session 人工验证（Claude Challenge 2）；< 80% 则 L2 优先级转为修质量 | B9：准确率 ≥ 80% |
| 1.6 store.go panic 风险修复 | `existing["status"]/["last_note"]/["acceptance_json"].(string)` 加 ok 断言（store.go:152/169/271）；`acc[index]` nil map 检查（:275）（Claude 细节审查 #1） | 稳定性：无 panic 崩溃 |
| 1.7 hook 层静默失败修复 | hook panic 时记录 `LogShared`（而非仅 stderr）+ 非 0 退出码；失败可见化（Claude 细节审查 #4/#5——**L0 是第一环，断了后面全白费**） | A1：hook 失败可观测 |

### Phase 2 — 注入效率优化（🟡 P5-P6）
| 任务 | 修改点 | 复测标准 |
|------|--------|----------|
| 2.0 注入内容质量验证（前置） | **Challenge 1 回应**：先验证"瓶颈是数量还是内容相关性/可操作性"——Agent 已主动获取信息（read_discussions 630 次），可能注入内容不够有用。抽样检查注入内容的可操作性后再决定 2.1 投量 | 归因结论：数量 vs 质量 |
| 2.1 事件注入按优先级而非数量 | `maxActions` 与 char 预算解耦。**聚合策略按类型区分（Claude 审核细化）**：`hotspot_untracked`/`mcp_error` 可聚合（"N 个文件被多 session 修改无 task 跟踪"）；`commit_orphan`/`tentative_link` **不聚合**（每个对应具体实体，需逐条处理） | C3：suppressed 占比 < 30%；C1 按唯一 hash 注入率 ≥ 80% |
| 2.2 L2 覆盖率根因排查 | 区分"AI 不可用 / session 过短 / 归类失败"三类无摘要原因 | B1：覆盖率 ≥ 85% |

### Phase 3 — 持续评估闭环（长期机制）
- 每次修复后：更新 EVALUATION.md 对应目标基线 → 差距归零即关闭
- 新增功能门槛：合入前必须定义可量化目标（引用 EVALUATION.md 编号）
- 每 2 周：跑一次全量基线采集，产出趋势报告
- 工具使用率：持续追踪 D1（get_briefing 87 次 vs 决策基线 36 次，目标 30%+）

## 3. 执行节奏建议

| 阶段 | 内容 | 节奏 |
|------|------|------|
| Phase 0 | 评估前置 3 项（0.3→0.1→0.2） | 本周（0.3 立即执行） |
| Phase 1 | 4 个数据质量修复 | 下一迭代（不紧急，改动小） |
| Phase 2 | 注入效率优化 | 再下一迭代（需设计，改动大） |
| Phase 3 | 持续闭环 | 长期 |

## 4. 建议的 AIPMC 落地

- 新建 plan「质量评估与完善闭环 v1」（挂 rdm-20260615-172601-024f61）
- tasks：0.1 / 0.2 / 0.3 / 1.1 / 1.2 / 1.3 / 1.4 / 2.1 / 2.2（P0=0.3，P1=Phase 1，P2=Phase 2）
- 复用已有 task：`task-20260727-133003-10d121`（0.1）、`task-20260728-101318-69ed87`（D1 工具使用率）
