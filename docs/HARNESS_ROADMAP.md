# Harness 评测闭环与注入可观测性规格 v1（2026-08-14 三方收敛）

> 本文档是 codex 在 8/14 三方讨论（codex ↔ claude-code ↔ 用户）收敛后负责交付的**规格**，
> 供 claude-code 实现「注入可观测性补全 + `aipmc eval` 提取器」时作为实现依据。
> 验收链：codex 写规格 → claude-code 实现 → codex 用 fixture 核验实现是否忠实于口径。

---

## 0. 背景与定位

AIPMC 作为 harness 的独特能力是「观察 → 理解 → 注入 → **再观察**」闭环：
hook/proxy 记录 agent 行为，pipeline 理解，INJECT 注入，下一轮 hook 又能看到注入对行为的
影响。本文档把「再观察」这条腿量化——用**生产日志**（而非编排任务）计算注入是否真的改变了
agent 行为，并修复评测所依赖的**注入可观测性缺口**。

已收敛的三方共识：

| 事项 | 结论 |
|---|---|
| 评测数据来源 | 生产日志（`discussion_log` + INJECT 日志），不依赖真实 API |
| 评测形态 | 日志提取器（headless，fixture 进 CI）+ 受控实验（可选、低频、最便宜模型） |
| 指标性质 | **关联指标**，非因果；用 W1 的 inject/suppressed 两条臂做**准实验对照组** |
| 前置缺口 | INJECT 日志只有计数没有内容（`:148` 无 hash、无 inject 表），须先补 instrumentation |
| CI 口径 | CI 只测提取器逻辑（合成 fixture）；真实指标跑开发机生产库（`aipmc eval` 手动/定时） |

---

## 1. 注入可观测性补全规格（claude-code 实现，热路径）

### 1.1 现状缺口（已核验）

`proxy/context_inject.go` 现有日志：

- 注入行 `:148`：`agent=%s session=%s req=%s goals=%d warnings=%d actions=%d file_total=%d guidelines=%d guide_del=%d chars=%d` —— **只有计数，无 hash，无注入内容**
- 抑制行 `:153`：`suppressed=%d reason=char_limit cap=%d ... segments=file_cut:%d warn:%d act:%d goals:%d guide:%d` —— 有分段裁剪计数（W1）
- skip 行 `:166`：`skip agent=%s reason=same_content hash=%s` —— **有 hash，但注入行反而没有**（8/13 埋点漏网）
- `metrics.go:405-418` 已能解析上述日志并产出 C1 双口径（`inject_rate` / `inject_coverage`，`metrics.go:501-503`）

### 1.2 改动 1：注入日志行补 hash

`:148` 注入行追加 `hash=%s`（取 `fullHash` 前 8 位，与 `:166` 一致），使 C1「按唯一 hash 计算注入率」
成为可能（EVALUATION.md E1 观测缺口之一）。

### 1.3 改动 2：新建 `inject_log` 表（结构化，供提取器直查）

```sql
CREATE TABLE IF NOT EXISTS inject_log (
    id          TEXT PRIMARY KEY,          -- uuid
    agent       TEXT NOT NULL,             -- codex-cli / claude-code / gemini-cli / cursor / opencode
    session_id  TEXT NOT NULL,
    req_id      TEXT NOT NULL,             -- r<pid>-<seq>（与日志一致）
    ts          TEXT NOT NULL,             -- ISO8601，与 created_at 同格式
    hash        TEXT NOT NULL,             -- fullHash 前 8 位
    source      TEXT NOT NULL DEFAULT '',  -- '' 正常注入 / guidelines_only
    segments_json TEXT NOT NULL DEFAULT '{}',
    chars       INTEGER NOT NULL DEFAULT 0,
    suppressed  INTEGER NOT NULL DEFAULT 0 -- 1 = 本次请求有内容被 cap 裁剪（对应 :153）
);
```

- `segments_json` 记录**实际注入的内容**（提取器重建「注入了什么」的唯一来源）：
  ```json
  {"fileAssoc":["src/a.go","src/b.go"],"warnings":["src/c.go 被多 session 修改无 task 跟踪"],
   "actionItems":["commit_orphan: xxx"],"goals":["修复 proxy token 认证"],"guidelines":true}
  ```
- 写入时机：仅「实际注入」的请求写一行（`shouldInject` 通过后、`injectIntoPrompt` 返回前），
  与 `:148` 日志同位置；**same_content / no_summary 跳过的请求不写**（对照组从日志侧重建）。
- **suppressed 字段如实记录 0/1（8/18 修订，原 T7「suppressed 不写表」废弃）**：
  char_limit 裁剪的请求**已实际注入**（只是内容不完整），必须写表且 `suppressed=1`（对应 `:153`），
  否则 M1 分子漏计裁剪注入、M2 注入组只剩「完整注入」session 造成样本偏差。
  提取器按 `suppressed` 分层：0=完整注入样本，1=不完整注入样本。
  `segments_json` v1 记录**注入前完整 segments**（`buildContextBlock` 不返回裁剪后实际内容），
  提取器对 `suppressed=1` 行按「不完整样本」处理；升级为返回实际段内容留待 S4 核验。
- 该表挂在 `db.go` 的 `schemaStatements` 中，`SCHEMA_VERSION` 升到 3（含 `migrate()` 增量路径）。

> **口径变更记录（8/18）**：原写策略「suppressed 不写表」在 heavy 环境失效——实测
> 1669 次注入中 1651 次（98.9%）带 char_limit 裁剪（file_total p50=9/p90=45，200B 预算
> 平均裁剪率 82%），导致 inject_log 恒空。修订为「实际注入即写 + suppressed 如实记录」。
> M1/M2 基线需在修订后重录，报告标注口径变化（HARNESS §1.3 修订，8/18）。

### 1.4 改动 3：skip 行补 session/req（M2 对照组重建前提，claude-code S2 认领）

`:166` 当前格式 `skip agent=%s reason=same_content hash=%s` **无 session/req**（`shouldInject`
签名不接收 sessionID/reqID），导致 same_content / no_summary_data 两层的 skip 行无法归属
session，M2 对照组只剩 char_limit 一层有数据。S2 需让 skip 行携带 `session=%s req=%s`
（将 reqID 传入 `shouldInject`，或在调用处补记）。§3.2 归并规则中「`:166` 行均带
agent/session/req」在改动落地前不成立，S4 按此核验。

### 1.5 热路径约束

- 改动只在 `InjectSessionContext` 内，追加一次 JSON marshal + 一次 insert；不允许改变注入内容/顺序。
- 注：本规格只覆盖**注入点捕获（v1）**；「模型实际收到什么」（出站 body）与响应推理捕获属 L-T v2，见 `docs/MEASUREMENT_LOOP.md`。
- 与 trace 加列（P1-1）解耦：`inject_log` 独立先落，`parent_id/span_type` 后续再加，互不阻塞。

---

## 2. 指标口径定稿（M1-M5）

原则：**分母先于分子定义**；每组指标必须写清「分母、分组、unknown 规则、时间窗」，否则不实现
（吸取 P14「mark_consumed 全量消费 → D2 失真」教训）。

### M1 注入观测（8/18 修订 v2：口径与目标语义对齐）

**背景**：8/18 注入稳定性修复后 `same_content` 跳过时**仍注入同一 block**
（`injectIntoPrompt` 已调用，仅观测层不写日志/表）——原「覆盖率」口径
`injected/(injected+same_content)` 实际是「新注入率」，健康形态（SP 稳定）下
天然 ≈1%，目标 ≥80% 结构不可达（实测 8/821=0.97%）。故拆为两个正交指标：

**M1a 注入观测完整性（对账，测量卫生核心）**
- 分子：`inject_log` 行数（窗口内，含 `suppressed=1`）
- 分母：日志侧 `:148` 注入明细行数（**仅 `agent=... hash=` 真实注入行**；
  `inject source=guidelines_only` 标记行在 dedup 之前打印、对 `same_content` 跳过
  也出现，计入分母会使 guidelines_only 流量对账系统性虚低——8/18 实测 codex 分母
  翻倍，已从提取器排除）
- 窗口起点：**`inject_log` 最早行**（观测层启用时间）。启用前的历史 `:148` 行无
  对应表行，计入分母会造成系统性误报（8/18 实测 claude reconcile=0.005 根因）
- 测试进程隔离：proxy 包测试直写生产日志/生产库（8/18 实测 write_err=10 全来自
  测试临时目录、日志侧 19 条无表行全为空 session 测试行）——测试已整体隔离
  （`AIPMC_LOG=off` + `PMAI_HOME` 临时目录），观测数据仅计生产流量
- 期望：**1.0**（每一条 `:148` 日志都有对应 `inject_log` 行）
- 语义：<1.0 即观测层断裂（写库失败/提取器 bug）——**先验证观测可信，再谈画像**
- 目标：`= 1.0`；`< 1.0` 触发告警（附 `write_err` 计数与差量）
- unknown 规则：无

**M1b 注入新鲜度（参考，稳定性镜像）**
- 分子：`inject_log` 行数
- 分母：`inject_log` 行数 + 日志侧 `reason=same_content` 行数（排除 `no_summary_data`）
- 语义：高 `same_content` 占比 = 高注入稳定性（8/18 修复的设计目标），非缺陷
- 目标：**参考**（首跑记基线，趋势监控；不作通过/失败判定）

**注入失效告警（直接证据，非间接推断）**
- `inject_log write_err=` 日志行出现 → 写库故障告警（与 SQLITE_BUSY 排查 task 联动）
- M1a 对账 <1.0 且差量 >0 → 观测断裂告警
- 注：不采用「injected=0 且 same_content>0」作为失效信号——内容确实从未变化时
  同样满足该条件，是健康形态，会产生误报
- 历史口径（8/18 v1 的 `inject_coverage`）在 EVALUATION.md 基线中标注口径变更后重录

### M2 文件命中率（准实验，核心指标）

- 定义：注入含 ≥1 个文件的 session 中，**注入时刻之后**该 session 的工具调用是否引用了注入文件
- 分子：命中 session 数（至少引用 1 个注入文件）
- 分母：注入组 = 该 agent 有 ≥1 次注入且注入含文件的 session 数；对照组 = suppressed 组
- **按 session 计，不按文件计**（避免大文件刷分母）
- **对照组按 reason 分层**：`same_content`（语义=「已见过」，基线是已注入过）、`cooldown`、
  `no_summary_data`（语义=无记忆）——三者语义不同，**必须分开报告**，禁止混在一起
- 混杂因子注记：`same_content` 抑制意味着刚注入过同内容，对照组基线不是「无注入」而是
  「已见过」；本指标是**准实验，不是随机对照**，报告必须带此注记
- 文件引用判定：**多格式兼容解析器**（8/25 修订）——复用 EVAL S1 `ParseToolRecord` 归一化，
  兼容 ① post_tool 平铺（codex/cursor 主流，顶层 `rel_path`/`file_path` + `tool_name`）
  ② `file_op` 嵌套旧格式（claude）③ 顶层 `type` 格式；引用 = 工具调用携带的任一文件路径
  （读/写均计，M2 语义是「模型是否引用注入文件」）。**8/25 口径变更注记**：旧实现只认
  `file_op` meta（生产仅 107 行 vs post_tool 10,366 行，漏 ~95% 带路径记录），解析器兼容后
  历史 M2 基线作废，需重录；变更来源 task-20260824-164234 审核链（Claude 审核 + codex 实测）。
- 输出：注入组/对照组各一个比率 + 差，标注「关联，非因果」
- unknown 规则：无法解析出文件路径的工具调用不计数

### M3 警告回避率（对齐 EVALUATION D4/F 层）

- 定义：注入的风险提示（**warnings + actionItems**，8/25 D2 修订——生产注入端把路径风险提示
  放在 actionItems，warnings 恒空，仅读 warnings 分母恒空）指向文件 X 后，agent 是否在**窗口内**
  未对 X 发生写操作；
  窗口 = 该次注入 ts 至该 session 下一次注入 ts（8/25 修订：原「N 轮（LIMIT 5 近邻）」会丢
  第 6+ 条写操作，且 `file_op` 嵌套假设对 post_tool 解析恒失败 → 回避率虚高；全窗口会让更晚的
  无关写计入「未回避」→ 方向失真；近邻注入窗口保留近邻语义且不丢数据）
- 分子：回避的 warning 数（warning→文件映射成功且后续无写）
- 分母：注入的 warning 总数中**能映射到具体文件**的数量
- 映射失败的处理：记为 `unknown` 单独一列报告，**不进分母**（当前 warning 多为自然语言，
  语义映射需要 LLM，headless 阶段只统计「可映射」子集；可映射子集覆盖率的提升本身是一条
  sub-metric）
- 写操作判定：**多格式兼容**（8/25 修订）——`ParseToolRecord` 归一化工具 ∈
  {edit, create, delete, rename, append, write, new_file, multi_edit, patch} 且携带文件路径，
  或 `file_op` meta 中 `type ∈ {edit, create, delete, rename, append}`；`read/bash/mcp` 不计写
- **8/25 口径变更注记**：写操作识别由 file_op-only 改为解析器兼容，且窗口由 LIMIT 5 近邻改为
  近邻注入窗口——旧实现算出的 M3 历史数值（8/7 基线、metrics 快照）作废，需重录并在报告中
  标注口径断层；变更前「回避率虚高」的量化见 8/25 审核（Claude C4）；8/25 D2 再修订：数据源
  并入 actionItems（真实库实证 warnings 0/274 非空，mapped 从 0 恢复到 214）；8/25 E1 再修订：
  写判定与警告路径 basename 归一化匹配（原精确匹配让回避率恒 100% 假象）；**语义局限注记**：
  当前 actionItems 语义是「建 task 跟踪」而非「禁止写」，回避率宜解读为「对告警文件继续操作的比例」；
  8/25 E1b 再修订：codex 写操作经 Bash 执行 apply_patch/sed -i/重定向，tool_name=Bash 被写过滤器
  误挡（111 条 apply_patch 全带 file_path）——写判定消费 hook 已打标的 source=bash_heuristic +
  type∈写集合（read/stage/unverified 不计），并解析 rel_paths 复数；真实库 M3 回避率
  100% → 99.55%（首个非 100% 数值：注入 HARNESS_ROADMAP.md 后窗口内多文件 apply_patch 命中）
- 目标：先定基线（首次运行记录值），暂不设阈值

### M4 立即行动区信噪比（收件箱卫生指标）

- 定义：简报「立即行动」区中 `commit_orphan + *_stale_file + *untracked + mcp_error` 事件占比
- 分母：该区全部事件（`events` 表 `consumed_by_agent=0` 且属于立即行动类型的集合）
- **时间窗：近 7 天**（否则历史堆积永久锁死指标）
- 目标：`< 50%`（现状目测 >90%，37 条 commit_orphan 占满）
- 与 M5 的关系：若抑制集中在 actionItems 而事件仍刷屏，说明收口在事件源头，不在注入

### M5 截断分布（对齐 W1/F2）

- 定义：被 cap 裁剪的条目按 segment 分布（`segments=file_cut/warn/act/goals/guide`）
- 数据源：`:153` suppressed 日志行（`metrics.go:418` 已解析）
- 目标：`file_cut + warn 占比 = 0`（fileAssoc/warnings 是最高优先级，不应被裁）；
  若伤及，属 INJECT 预算分配 bug（EVALUATION C3）
- unknown 规则：无

### 指标与 EVALUATION.md 映射

| 指标 | 对齐目标 | 数据源 | CI（fixture） | 生产（定时） |
|---|---|---|---|---|
| M1 | C1/E1 | inject_log + metrics 日志 | ✅ | ✅ |
| M2 | D1/D4/F | inject_log + discussion_log(file_op) | ✅ 提取器逻辑 | ✅ |
| M3 | D4/F | inject_log + discussion_log | ✅（可映射子集） | ✅ |
| M4 | B6/D2 | events 表 | ✅ | ✅ |
| M5 | C3/F2 | :153 日志 | ✅ | ✅ |

---

## 3. fixture 数据样例（提取器测试用，claude-code 不得自行发明）

以下合成数据写入测试临时库，覆盖 M2/M3/M4 的三条路径：

### 3.1 inject_log（2 行：命中 / 未命中；对照组不写表，见 3.2b）

```
id=inj-0001 agent=codex-cli session_id=sess-A req_id=r100-1 ts=2026-08-14T10:00:00 hash=abc12345
  segments_json={"fileAssoc":["src/proxy.go","src/hook.go"],"warnings":["src/proxy.go 被多 session 修改"],
                 "actionItems":[],"goals":["修 proxy 认证"],"guidelines":true} chars=412 suppressed=0

id=inj-0002 agent=codex-cli session_id=sess-B req_id=r100-2 ts=2026-08-14T11:00:00 hash=def67890
  segments_json={"fileAssoc":["src/api/server.go"],"warnings":[],"actionItems":[],"goals":[],
                 "guidelines":false} chars=180 suppressed=0

```

### 3.2b 对照组日志行（M2 按 reason 分层，覆盖 T3/T4；来源为 INJECT 日志的 `:153`/`:166` 行）

```
[INJECT] skip agent=codex-cli reason=same_content hash=abc12345          -- 对照组 A：已见过（刚注入过同内容）
[INJECT] suppressed=2 reason=char_limit cap=800 agent=codex-cli session_id=sess-C req_id=r100-3 segments=file_cut:1 warn:1 act:0 goals:0 guide:0  -- 对照组 B：char_limit（cooldown 类同构）
[INJECT] skip agent=codex-cli reason=no_summary_data                      -- 对照组 C：无记忆，M1 分母排除
```

### 3.2 discussion_log（工具调用，测文件引用判定）

```
sess-A: role=assistant source=codex-cli content='📝 src/proxy.go — modify'  metadata={"file_op":{"type":"edit","rel_path":"src/proxy.go"}}  ts=10:01:00（注入后）
sess-A: role=assistant source=codex-cli content='📝 src/other.go — modify' metadata={"file_op":{"type":"edit","rel_path":"src/other.go"}} ts=10:02:00（注入后）
sess-B: role=assistant source=codex-cli content='👁 src/api/server.go — read'  metadata={"file_op":{"type":"read","rel_path":"src/api/server.go"}} ts=11:05:00（注入后）
```

预期：M2 注入组 = {sess-A: 命中（proxy.go）、sess-B: 命中（server.go）} → 2/2 = 100%；
对照组按 reason 分层报告：same_content 组（无工具调用）0%、char_limit 组（sess-C 无工具调用）0%、
no_summary_data 组不参与（M1 分母排除）。M3：warning「src/proxy.go 被修改」→ sess-A 对
proxy.go 发生 edit → 未回避（0/1），可映射 1 条。

**M2 归并规则（已定，S4 核验）**：注入组按 `inject_log.session_id` 为主键去重；对照组从日志侧重建时
按 `(agent, session_id)` 去重（`:153` 行已带；`:166` 需先落地 §1.4 改动 3 补 session/req，S4 核验）。

### 3.3 events（测 M4）

```
type=commit_orphan created_at=近3天内 × 30 条
type=tentative_link   created_at=近3天内 × 5 条
type=mcp_error        created_at=近3天内 × 1 条
```

预期：近 7 天窗口下噪音占比 = 31/36 ≈ 86%（>50% 目标，触发告警）；若窗口外还有 200 条
commit_orphan，必须被时间窗排除。

---

## 4. INJECT 2.0 delta 设计（v1 设计稿，实现排在评测闭环之后）

### 4.1 原则

**注入增量，不注入存量**：agent 上下文里已有项目状态，800 字符预算只花在「模型不知道的
变化」——上次注入/摘要之后发生了什么。

### 4.2 机制：水位（watermark）聚合

- 每 (agent, session) 维护注入水位 `last_inject_ts`（取自 `inject_log.ts`）
- delta = `events.created_at > last_inject_ts` 且 `consumed_by_agent=0` 的事件，
  按类型聚合：`commit_orphan`/`tentative_link` **逐条**（对应具体实体），
  `hotspot_untracked`/`mcp_error` **聚合为一条**（"N 个文件…"）
- 上限沿用现有 `perTypeCap=5` / `actionItemCeil=10`（`context_inject.go:31-32`）
- 去重从「内容 hash」升级为「事件 id」：同一事件只注入一次（不再依赖整块 hash 相似度）

### 4.3 预算分配（保持 800 上限，不涨 token）

```
fileAssoc（新出现文件）> warnings（新警告）> actionItems（新事件）> goals > guidelines
```

与现状（`context_inject.go:67`）一致，仅把「新」从「hash 变」细化为「事件级水位差」。

### 4.4 落地条件

- **必须先有 M2/M3 基线**：改格式=一次 A/B，注入格式的每次调整记录 before/after M2 值
- canary：先对单 agent（如 codex-cli）灰度，指标不劣化再全量

---

## 5. 评测任务集草稿

### 5.1 headless 层（进 CI，fixture 驱动）

| 任务 | 断言 |
|---|---|
| T1 提取器正确性 | fixture 3.1-3.3 → M1-M5 输出与预期一致 |
| T2 分母规则 | suppressed(no_summary_data) 请求不进 M1 分母 |
| T3 时间窗 | M4 仅统计近 7 天 |
| T4 对照组分层 | M2 按 reason 输出三组，不合并 |
| T5 unknown 规则 | M3 不可映射 warning 进 unknown 列，不进分母 |
| T6 对照组分层 | fixture 3.2b 三组（same_content/char_limit/no_summary）分别输出，不合并 |
| T7 写策略 | fixture 中 same_content/no_summary 请求必须来自日志行（inject_log 无对应行）；char_limit 裁剪请求写表且 `suppressed=1`，与 `:153` 日志一致 |

### 5.2 行为回归层（`aipmc eval`，手动/定时，跑开发机生产库）

- 每次改 INJECT 格式/阈值后运行，输出与仓库内 `eval/baseline.json` 对比
- baseline.json 首次运行生成并提交，后续 diff 报告（M2 下降 >5pp 即阻断合入）

### 5.3 受控实验（可选，低频）

- 同 prompt × 注入开关 × 最便宜模型，样本 ≥20，报告 M2 因果差
- 即使长期不跑，5.1/5.2 的核心评测不受影响

---

## 6. 落地顺序、分工与验收

| 步骤 | 负责 | 交付 | 验收 |
|---|---|---|---|
| S1 | codex | 本文档（规格） | 本节完成 |
| S2 | claude-code | `:148` 加 hash + `inject_log` DDL/写入（热路径） | 注入请求写入 inject_log，内容与 segments_json 一致 |
| S3 | claude-code | `aipmc eval` 提取器 + fixture 测试 | T1-T5 通过，fixture 用本文档 3.x 数据 |
| S4 | codex | 用 fixture 核验实现忠实于口径 | M1-M5 输出与 3.x 预期逐项一致 |

**S4 核验项清单（对应 claude-code S2/S3 审稿修正）**：
1. fixture 与 §1.3 写策略一致（无 inject_log 的 suppressed 行，对照组走日志侧）
2. M1 分母排除 no_summary_data；C1 注释/代码不一致已修复或单独挂起并记录（`metrics.go:503-507`）
3. fixture 覆盖 T3（7 天窗）与 T4/T6（对照组三 reason 分层）
4. `aipmc eval` 输出：JSON + 人类可读双输出（对齐 `metrics.go` printRow 风格）
5. M2 归并规则：注入组按 session_id 主键、对照组按 (agent, session) 去重
| S5 | 双方 | INJECT 2.0 delta 实现（第 4 节） | M2/M3 基线不劣化 |

前置基线（不阻塞 S2-S4，但阻塞 S5 上线）：P0-1 默认构建自举、P0-2 测试隔离 + relpath 修复、
P0-3 Windows CI。最终验收标准：**默认环境下裸 `go test ./...` 全绿**。
