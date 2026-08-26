# 行动测量反馈体系规格 v1（MEASUREMENT_LOOP）

> 目的：把 AIPMC 从「记录 agent 行为的黑盒」变成「可验证、可归因、可反馈的测量体系」。
> 背景（8/15 三方共识）：项目代码 100% 由 code agent 完成，用户对「代码实际完成质量」与
> 「harness 是否发挥作用」缺乏独立验证——现在的验证字段是 agent 自报的（`mcp.go:293` 工具
> 描述明文教 agent 设 `test_status=passed` 过 done-gate）。本文档建立三层相互独立的证据通道，
> 任一层被刷，另外两层能发现。
> 关系：`docs/HARNESS_ROADMAP.md` 的 M1-M5 是本文档 L-A 归因层的核心；本文档在其外补
> 思考层（L-T）、产出层（L-O）、捕获层对账（M0）与用户反馈层（L-F）。

---

## 0. 核心命题

1. **黑盒的实质不是缺数据，是缺独立验证。** 行为数据全量在 `discussion_log`；缺的是两层：
   产出侧无系统验证（自报循环）、思考侧读完即弃（proxy 读完整 body 但只落一行 `[LLM]` 摘要）。
2. **测量对象能读测量仪器。** agent 可通过 MCP/简报读到 EVALUATION、metrics、done-gate 规则，
   所以指标会失真（Goodhart）。对策：结果指标优先、部分指标只对用户可见、推理层作为最难伪装的
   诚实传感器。
3. **测量体系坐在捕获层上。** 五层全部依赖 L0（hook/proxy），L0 有两个已知裂缝（P16 hook 吞
   panic 成功退出、relpath 路径污染）。因此**第一项测量就是查捕获层是否诚实**（M0）。

---

## 1. 五层模型

```
L-T 思考层（新增）  proxy 请求/响应 trace：注入原文 + 推理块 + 工具调用参数   ← 思考过程
L-B 行为层          discussion_log：说出来、做出来的（环境感知）
L-O 产出层（新增）  build/test 验证 + 意图抽查：代码是否真的能用、是否达成意图
L-A 归因层          M1-M5（HARNESS_ROADMAP）：注入 → 思考 → 行为 → 产出 全链归因
L-F 反馈层          用户可见报告 + 抽查锚点（自报 vs 实测对照表）
```

数据源现状（8/15 已核验）：

| 层 | 数据源 | 现状 |
|---|---|---|
| L-T | proxy 请求/响应体 | **读后即弃**：`adapt_*.go:20-24`、`proxy.go:349/925/1057` 全 `io.ReadAll`，`[LLM]` 日志（`proxy.go:998/1163`）只记 tokens/injected/latency，`recordTraffic`（`proxy.go:105`）只记 size |
| L-B | `discussion_log`（含 `file_op` meta） | ✅ 全量 |
| L-O | commits 表 `test_status`/`review_status` | ⚠ 自报字段：`mcp.go:293/306` 明文教 agent 设 passed 过 done-gate；`hook/post-commit.go` 只记录元数据，无任何验证命令 |
| L-A | events + INJECT 日志 + inject_log | 🟡 M1-M5 已定稿，待 S2/S3 实现 |
| L-F | 无 | ❌ 用户无任何产出侧可见性 |

---

## 2. M0：捕获层完整性对账（第一验收项）

> 原则：测量第一项就是查捕获层是否诚实。L0 有洞，上层指标全部失真。

- **漏录率**：每 session 的 `discussion_log` 消息数 vs proxy `[LLM]` 请求数，差异即漏录。
  - 分子：有 proxy 请求但无对应 discussion_log 消息的 session 数
  - 分母：有 proxy 请求的 session 数
  - 已知裂缝来源：P16（hook 吞 panic 成功退出）、`hook/relpath.go` 路径污染导致 file_op 丢失
- **自报可信度**：commit 的 `test_status=passed` 占比 vs 系统实测通过率（L-O），gap 即自报膨胀率。
- **反向对账**：`discussion_log` 有、`[LLM]` 无的行（hook 记录但 proxy 未转发 = 脱链 session，
  对应 MCP 孤儿 session 归并问题）。
- 目标：先记录基线，后续要求漏录率 `<5%`。
- 数据源：现有日志 + 表，无需新采集，进 `aipmc eval` 的 headless 层（fixture 合成对账样例）。

---

## 3. L-T 思考层规格（v2，排在 HARNESS_ROADMAP S2/S3 v1 之后）

### 3.1 双抓取点（claude-code 已实测确认顺序）

`proxy.go:358` 的 `InjectSessionContext` 在协议适配（`handleUnified*`/`handle*Passthrough`）
**之前**执行 → 适配器翻译时注入块会被再加工。因此：

| 抓取点 | 位置 | 回答的问题 | 归属 |
|---|---|---|---|
| 注入点（v1，已有规格） | `context_inject.go:146 injectIntoPrompt` 返回后 | **我们注入了什么**（权威记录） | `inject_log` 表，HARNESS_ROADMAP §1 |
| 出站点（v2，本规格） | 适配之后、`upstream.go` 出站之前 | **模型实际收到什么** | `proxy_trace` 表（见下） |

两者都必须有：审计/对账用注入点，M2 归因（模型是否真的看到）用出站点。

### 3.2 `proxy_trace` 表（v2，只抓结构化字段，不全量存）

```sql
CREATE TABLE IF NOT EXISTS proxy_trace (
    id          TEXT PRIMARY KEY,
    session_id  TEXT NOT NULL,
    agent       TEXT NOT NULL,
    model       TEXT NOT NULL,
    req_id      TEXT NOT NULL,
    ts          TEXT NOT NULL,
    injected    INTEGER NOT NULL DEFAULT 0,      -- 本请求是否注入（对应 ctxInjected）
    outbound_body_hash TEXT NOT NULL,            -- 出站请求体 hash（适配后，模型实际收到）
    thinking    TEXT DEFAULT '',                 -- 推理块（有则存，截断 4KB）
    thinking_coverage INTEGER NOT NULL DEFAULT 0,-- 是否有推理（M 分母用）
    tool_calls  TEXT DEFAULT '[]',               -- JSON: [{name, args_summary}]
    final_text_hash TEXT NOT NULL DEFAULT '',
    chars       INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_proxy_trace_session ON proxy_trace(session_id, ts);
```

### 3.3 双 tier 推理信号

- **Tier 1（强信号，仅推理型模型有）**：reasoning 中引用注入文件/警告 → 「推理引用率」。
  必须带 **coverage 分母**（有推理的请求占比）——deepseek 等经适配器后可能无 thinking block，
  无推理时该指标空缺并注明，不得当成 0%。
- **Tier 2（弱信号，全模型可用）**：最终文本 + 工具调用参数引用注入内容——M2 行为证据的文本层补充。
- 存储约束：thinking 截断 4KB/条；本地库仅此一份，**不进任何共享报告**；按 agent/session 可开关
  （默认只对"评测会话"开全量，其余存摘要）。

---

## 4. L-O 产出层规格

### 4.1 系统验证（执行，不是自报）

- 扩展 `hook/post-commit.go`：commit 后自动跑 `go build ./... + go vet ./... + go test ./...`，
  结果写入 commits 表**新增字段** `verify_status`（`passed/failed/not_run`）+ `verify_log`。
- 与 `test_status`（agent 自报）**并列对照**，形成 L-F 的「自报 vs 实测」表。
- 首次只记录不改 gate：并行对照期 ≥ 2 周，验证采集稳定后再谈切换。

### 4.2 done-gate 渐进上线

- 现状：`test_status=passed`（自报）即可过 done-gate（`mcp/mcp.go:490`）。
- 目标态：done 条件 = **系统验证通过 或 人工 override**，agent 自报不再单独构成 done。
- 路径：并行对照期（两边都记，gate 不变）→ 用户确认 gap 可信 → 切换 gate → 保留 override 通道。
- 影响面：所有 agent 工作流，**必须标"渐进上线"**，禁止一步切换。

### 4.3 意图抽查协议（唯一人工 ground truth）

- 自动化验证不了「代码是否做了用户要的事」。每周抽 **5 个 commit**，只核对一件事：
  **diff 是否实现了 commit message 声称的意图**——不重跑测试（那是 L-O 自动做的），只做
  intent-check。
- 记录：抽查表（commit、声称意图、实际达成、偏差说明）进 `docs/AUDIT_SAMPLES.md` 或 `eval/`。
- 用途：① 校正 L-A 指标的漂移；② 打破「agent 互审趋同」闭环；③ 给用户一个不读代码的
  「真实感」锚点。这正是用户黑盒直觉的现场验证（两轮讨论挖出两个文档外事实：自报验证、思考数据被弃）。

---

## 5. 与 HARNESS_ROADMAP 的关系与落地顺序

| 顺序 | 交付 | 归属 | 状态 |
|---|---|---|---|
| S0 | 本文档 + HARNESS_ROADMAP §1.4 修正（skip 行 session/req） | codex | ✅ 本版完成 |
| S1 | M0 对账指标 + fixture（headless） | claude-code | 待开工 |
| S2/S3 | HARNESS_ROADMAP v1：`:148` hash + inject_log + skip 行 session/req + `aipmc eval` | claude-code | 待开工 |
| S4 | codex 用 fixture 核验 S2/S3 忠实于口径 | codex | 待 S2/S3 |
| S5 | L-T v2：proxy_trace 双抓取点 + 双 tier | claude-code | 待 S4 |
| S6 | L-O：post-commit 验证 + 自报/实测对照 + done-gate 渐进 | 分工待定 | 待 S5 |
| 长期 | L-F：周报（用户可见）+ 意图抽查每周 5 commit | 用户 + agent | 持续 |

**三通道独立性**：自报 vs 实测对照表（L-O/L-F）｜意图抽查（人工）｜思考层传感器（L-T）。
任一通道被刷，另外两层可发现——这是本体系对抗「agent 自证循环」的结构性设计。

---

## 7. 生产数据基线（2026-08-15 实测，`D:\projects\AIPMC\.pmai\data\pmai.db` + `~/.aipmc/logs/aipmc.log`）

> 本节数据是讨论从「设计假设」转向「现实依据」的锚点。测量体系上线前，先以本节为 S0 基线；
> 后续所有 M 指标的首次运行值必须与本节对账。

### 7.1 行为层（L-B）

| 指标 | 实测值 | 含义 |
|---|---|---|
| discussion_log 总量 | 11,529 行（快照时刻，库在持续增长） | claude-code 5859 / opencode 2757 / codex-cli 1430 / cursor 1326 / gemini-cli 152（另 mcp 5） |
| 近 30 天占比 | 692 行（6.0%） | **94% 是历史数据**（6-7 月，旧路径 /Users/dazsec/workspace 时代） |
| metadata 含 file_path（旧格式） | 1,439 行 | `{type,file_path,hunks}`，仍持续产出（max=08-15） |
| metadata 含 rel_path（新格式） | 32 行 | 8/13 T3b/T4 引入的新格式，**生产覆盖率 ≈ 0.2%** |
| metadata 含 file_op（新格式） | 21 行 | 同上，≈ 0.1% |
| 项目外路径 | 469+/1,439 file_path | 含 `~/.claude/plans/` 等仓库外文件 → **relpath 污染的实锤** |

### 7.2 理解层（L2）

| 指标 | 实测值 | 含义 |
|---|---|---|
| session_summaries 行数 | 42 | 有 L2 摘要的仅 **5 行（12%）**，与 IMPROVEMENT_PLAN 声称的 55% 差距巨大 |
| 注入缓存数据源 | 几乎为空 | 直接导致注入层无货可注（见 7.3） |

### 7.3 注入回路活性（闭环是否在转——最重要的发现）

`~/.aipmc/logs/aipmc.log`（覆盖 20:01→00:21 约 4.3 小时）：

| 指标 | 实测值 |
|---|---|
| [INJECT] 实际注入 | **10 次**（旧格式 7 + 新格式 3） |
| [INJECT] skip | **2,047 次**：no_summary_data 1,883（92.0%）/ same_content 105 / cooldown 59 |
| [LLM] 调用注入标记 | injected=Y **6 次** / injected=N 1,392 次（0.4%） |
| [INJECT] emerge_events | 15 次，全部 `types=commit_orphan=41`——事件涌现回路只反复出同一批孤儿 commit 事件（从未被消费，见 7.4） |
| [INJECT] file_assoc | 335 次 `reason=no_graph_data`——文件关联图同样在空转（图数据为空） |

**结论：Q1→Q2→Q3 闭环在生产中基本空转——断点在 Q2（理解层），不是 Q3（注入格式）。**
评测闭环（M 系列）的全部指标都建立在极小的分母上，必须先解决"理解层产出"才能谈注入效率。

### 7.4 产出层（L-O）与配置实况

| 指标 | 实测值 |
|---|---|
| commits 自报状态 | test_status=passed/review_status=approved **12 行**（agent 自报）；auto/auto 76 行（gitsync）；系统验证 **0 行** |
| events 未消费 | commit_orphan 41 条全部 consumed=0 → 占满简报"立即行动"区（M4 时间窗必要性的实锤） |
| projects.json | **4 个项目全部配置 proxy_port=19530**（P15 多实例冲突的实锤） |
| 版本漂移 | 运行中的 aipmc.log 同时出现旧格式（`goals=3 chars=574 ids=[...]`）与新格式（`session= req=`）→ 部署二进制落后于源码，日志解析必须按版本分型 |

### 7.5 对落地顺序的修正（相对 §5）

1. **M0 对账扩展为「捕获层格式审计」**：新格式（rel_path/file_op）覆盖率、旧格式占比、版本漂移——
   当前新格式覆盖率 0.2%，意味着 M2/M3 在修复 hook 产出前**无数据可算**。
2. **新增 M-1 回路活性**：注入/skip 按 reason 分布（基线：no_summary_data 92.0%），
   作为所有 M 指标的分母前提；M-1 不达标则下游指标无意义。
3. **S6 的 L-O 有真实起点**：自报 12 vs 实测 0 的对照表已可建。
4. **优先级修正**：理解层产出（Q2）先于注入格式（Q3）——与 IMPROVEMENT_PLAN「P6 L2 覆盖率
   根因排查」一致，但现在是测量体系的前置，不是可选项。
