# Agent 评审收敛协议 v1（设计实施方案）

> 状态：**设计稿 v1.3（8/11 修订）**——新增效果指标与基线章节（防盲改），吸收 Claude 3 补充
> 修订：§3 收敛协议改「约定驱动格式解析」（原 emerge 语义识别不可解析）；§2.4 R2.2 对账锚点改 2pp 容差（原「差 0」不可复现）；§3.3 计时起点明确；§1.4 R1.2 改结构化统计
> **8/11 Claude 复核 v1.1 → 3 条微调全认，出 v1.2**：① `protocol_idle` 降级为观察指标（原设计高噪音，Claude 承认打偏）；② challenge 前缀简化 + evidence 宽松（机器可读性 ≠ 使用意愿，格式摩擦是采用率天花板）；③ R1.2 分阶段验收（先硬指标再全量）。连带调整 `challenge_invalid` 判定（evidence 缺省取正文后不再是 invalid 条件）。
> **8/11 用户反问「是否有可观测指标，否则都是盲改」→ v1.3**：新增 §0.5 效果指标与 8/11 基线（机制验收 ≠ 效果验收）；Claude 3 补充：① R1 效果用阶段 A 合规率做代理（「无出处占比」语义判定会陷入正则噪音陷阱）；② 「用户介入次数」机器判定不可靠，降为人工记录，改用 R3 收敛前轮数（机器可数）；③ 采样触发改「累计 10 次 [challenge] 或 30 天，先到先评」。
> 背景：8/11 三轮 challenge→回应 暴露「断言缺出处、口径漂移、基线无标注、转述替代查证」四类根因；
> 结论：规范必须是**双方共同执行**，不是单方向约束；三条 task 落地 + 一条工作习惯
> 关联 plan：`plan-20260807-092253-b2e614`（质量评估与完善闭环）、`plan-20260804-175245-c10790`（协作感知 L1）
> 原则：EVALUATION §0.1 每个目标必须有可计算量化指标；§0.3 先记录基线 → 定目标 → 差距即待办

---

## 0. 根因与收敛结论（8/11 三轮对质摘要）

| # | 根因 | 实例 | 治理 |
|---|------|------|------|
| R1 | 断言无出处/引用当实测 | 「codex 恒 0」引 E5 备注未查数据，25 分钟后实测推翻 | §1 断言分级 + 强制命令片段 |
| R2 | 口径漂移 | cache_hit_rate 次数 vs token 加权，同日 4 种口径 | §2 指标注册表唯一口径 |
| R3 | 基线无口径标注 | A1 基线 34,929 实为 ED 跨库口径，文档未标注 | §3 基线口径标注 + 对账 |
| R4 | 转述替代查证 | T1 误报阻塞（未查 list_tasks）；基线引用（未查口径） | §5 转述移除（工作习惯） |

**双方承诺（8/11 收敛）**：
- codex：断言分级标签 + 固化指标脚本 + 争议先查结构化接口
- Claude：提交前 `git status` 核对非裸 `git add -A`；Claude 侧数字同样固化进 scripts/

---

## 0.5 效果指标与 8/11 基线（防盲改）

**原则（8/11 用户反问）**：机制验收（跑得通）≠ 效果验收（有没有用）。每条 task 除机制验收外必须带效果指标；无基线指标必须先记基线（EVALUATION §0.3 先记录基线 → 定目标 → 差距即待办）。

**8/11 基线（今天实测，先记录）**：

| 指标 | 8/11 基线值 | 判定方式 |
|------|------------|---------|
| 用户介入协调/裁决次数 | ≥3（转发/协调/裁决类，非全部 user 消息） | **人工记录**（机器语义判定不可靠，Claude 8/11 补充） |
| 对质轮数 | 3 轮（challenge→回应→复核） | 机器可数（R3 落地后） |
| 翻案/打偏次数 | codex 3 + Claude 2 | 人工记录 |
| 断言无出处 | 多起（恒0/malformed/地基空） | 用 R1 阶段 A 合规率做代理（见下） |

**效果指标（v1.3，含 Claude 8/11 三补充）**：

- **R1 效果**：阶段 A「`[实测]` 必附命令片段」合规率（机器可判定，前缀解析即算）——作为「无出处断言占比」的代理，**不追求后者语义判定**（正则判「评审类对话」/「无出处」必然误报，避免陷入自身批评过的噪音陷阱）
- **R2 效果**：`aipmc metrics` 脚本调用次数（可观测）；对话中直接引用指标数字（非脚本输出）的次数下降
- **R3 效果**：收敛前轮数（机器可数，R3 落地后）替代「用户介入次数」——后者机器判定不可靠，仅人工记录兜底
- **采样触发（Claude 8/11 补充 3）**：累计 10 次 `[challenge]` 事件 **或** 30 天，先到先评——30 天是拍的数（呼应 H-A4 样本稀疏教训），对质非每日发生，事件数为主触发、日历时间兜底

## 1. Review 断言规范（Task R1）

### 1.1 断言分级标签

评审/讨论发言中，**每一条数据断言**必须标注来源级别：

| 级别 | 定义 | 必附 | 示例 |
|------|------|------|------|
| `[实测]` | 本次会话亲自执行了命令/查询 | **当次命令 + 输出片段**（≥关键 3 行） | `[实测] sqlite3 ... GROUP BY source → claude 9867` |
| `[引用]` | 转述文档/他人/历史发言 | **出处位置 + 查证记录**（是否已复核原文） | `[引用] EVALUATION.md:33（未复核口径）` |
| `[推断]` | 无数据支撑的分析/推测 | 标注「推断」二字 + 依赖前提 | `[推断] 若 POC-2 显示断点包含 user 则…` |

### 1.2 强制规则

1. **`[实测]` 未附命令 = 无效实测**：按 `[引用]` 处理，对方可要求补命令。防止「自报实测无审计」滑向「全是实测」。
2. **`[引用]` 必须附位置**：`file:line` 或 `disc-xxxx`，不附位置视为无出处。
3. **禁止用结果反推级别**：先查证再标注，不允许事后把 `[引用]` 升级为 `[实测]`。
4. **口径必须固定**：引用指标时附带口径（权重/窗口/空值处理），见 §2 注册表。

### 1.3 落点

- 写入 `docs/GO_GUIDE.md`（评审/讨论章节）+ `AGENTS.md` 引用
- `aipm_get_briefing` 的「评审提醒」区提示：`断言请标 [实测]/[引用]/[推断] + 命令/位置`

### 1.4 验收

- R1.1：GO_GUIDE 断言章节可读，含 3 级定义 + 4 条强制规则
- R1.2：分两阶段验收（8/11 v1.2，避免 R1 刚落地即要求 80% 过严）：
  - **阶段 A（硬指标，先达标）**：`[实测]` 断言必附命令片段——合规率 ≥ 80%（前缀解析可机器判定，无语义成分，不依赖分母定义）
  - **阶段 B（全量分级标注率，后达标）**：带 `[实测]/[引用]/[推断]` 前缀的数据断言占比 ≥ 80%
  - **分母定义（实现约束）**：「数据断言」= assistant 消息中含数据断言特征的行（正则近似：数字/百分比/计数词 如 `\d+%|\d+(?:,\d+)+|次数|行|命中率|占比`）；分子 = 带前缀行。分母正则固化进脚本并写注释，防口径漂移

---

## 2. 统计口径规范 + 固化指标脚本（Task R2）

### 2.1 指标注册表（唯一口径来源）

新建 `docs/METRICS_REGISTRY.md`，每个核心指标一条记录：

```yaml
metric: cache_hit_rate
definition: Σ(cache_hit) / Σ(in_tok)        # token 加权，非次数
source: ~/.aipmc/logs/aipmc.log [LLM] 行
fields: cache_hit= (anthropic) / n_hit= (responses)   # 双字段兼容
weight: in_tok token 加权
window: 可配置（默认全量；--since/--window 支持）
empty_value: 缺 cache_hit 字段的行按 0（不计入分子，计入分母）
split: 全 agent 汇总 + 按 agent 拆分（各 agent cacheHit/inTok）
code_ref: metrics.go:433-435（汇总）, metrics.go:456-457（按 agent）
inject_judge: injected=Y/N 字段
```

首批注册指标（8/11 争议相关）：

| 指标 | 定义 | 代码出处 |
|------|------|---------|
| cache_hit_rate | Σcache_hit/Σin_tok，token 加权 | metrics.go:433-435 |
| injected_rate | injected=Y 行数 / [LLM] 总行数 | metrics.go:456-460 |
| discussion_log 规模 | 按 source 计数（**必须标单库/跨库**） | 无（新增脚本） |
| 工具调用计数 | role∈(tool,assistant) 且含 🔧 标记 | 无（新增脚本） |
| metadata 合法率 | valid/(total−empty)，分母排除空串 | metrics.go H2 块 |
| 碰撞/重叠文件数 | 2+ session 修改同一文件（hotspot） | session/emerge.go:151 |

### 2.2 固化脚本（`scripts/` + `aipmc metrics` 扩展）

新增可复用命令，替代「每次手写 SQL/日志查询」：

```bash
# 日志类（全局 ~/.aipmc/logs/aipmc.log）
aipmc metrics cache-hit --agent=codex --window=7d      # 按 agent + 窗口
aipmc metrics cache-hit --agent=codex --since=2026-08-10T00:00:00

# DB 类（当前项目 .pmai）
aipmc metrics discussion-log --by-source              # 单库规模
aipmc metrics discussion-log --by-source --project=/Users/dazsec/projects/EncryptDrive  # 跨库对比
aipmc metrics metadata-health --by-source
```

实现要点：
- 复用 `metrics.go` 的日志解析（`[LLM]` 行 key=value 解析），抽公共函数
- 所有查询带 `--since/--until/--window/--agent/--project` 参数（回放历史窗口必需）
- 输出：JSON（machine-readable）+ 表格（human-readable），JSON 带 `generated_at/window/schema_version`
- 挂在 `aipmc metrics` 子命令下，不新增独立二进制

### 2.3 基线口径标注（前置）

EVALUATION.md 所有基线数字统一补标注（**先于脚本对账**，否则「差 0」卡在脏基线上）：

```markdown
- 基线：claude-code 18634 / codex-cli 12977 / cursor 1583 / opencode 1361 / aipmc-vision 374
  **（口径：跨库汇总 [aipmc + ED]，8/6 快照；cursor/opencode 与 ED 库逐位一致）
  （查询：sqlite3 pmai.db "SELECT source,COUNT(*) FROM discussion_log GROUP BY source" — 8/6 快照）**
```

标注字段模板：`（口径：单库/跨库 + 快照日期 + 查询命令摘要）`

### 2.4 验收（含 Claude C 建议）

- R2.1：注册表覆盖 ≥ 6 个核心指标（上表）
- R2.2：脚本自洽（同命令重跑输出一致）+ 与 8/10 实测 36.9% 偏差 **< 2pp**（8/11 修订：8/10 的 36.9% 是当日窗口口径、查询未固化，`--since=8/10` 全天重算不可精确回放，允许窗口误差；「差 0」只保留给可回放对象——基线标注后的对账 R2.3）
- R2.3：脚本输出可对账 EVALUATION 基线（同口径重算，差 0）——**前置：基线口径标注完成**
- R2.4：1c-B 观测门触发基线**改从脚本输出取数**，不再从对话数字取（决策 23 观测门依赖）
- R2.5：EVALUATION 基线全部补口径标注（8/11 后新增基线必须自带标注，写进 §0.3）

---

## 3. 对质收敛协议（Task R3）

### 3.1 challenge/response 结构化（约定驱动格式解析）

沿用现有 events 机制（`store.CreateEvent` + `session/emerge.go` dupEvent 去重），新增事件类型。

**关键约束：challenge/response 必须带机器可读前缀，emerge 按格式解析成事件，不做语义推断**（Claude 8/11 审核 Challenge 1：自然语言关键字匹配填不出 target/证据/类别，与 commit_orphan 的机器信号不可类比）。

| 事件 | 字段 | 说明 |
|------|------|------|
| `challenge_factual` | 目标 disc id、断言原文、证据、类别 | 类别：factual / consistency / provenance |
| `response_marked` | 源 disc id、status（成立/部分成立/不成立）、理由、证据 | 逐条回应 |
| `unresponded` | 源 challenge id | 超过 N 时间未回应自动生成 |

### 3.2 流程

```
[challenge] 提出（必须带前缀 + 证据：原文引用 or 数据命令）
   → [respond] 逐条标注（成立/部分成立/不成立 + 理由 + 证据）
   → 未回应条目：超过计时窗口自动 events.unresponded → 进 briefing
   → 全部响应且无新 challenge → 自动收敛报告
```

**消息格式（机器可读前缀，无前缀不计入协议；8/11 v1.2：前缀简化 + evidence 宽松）**：

```
[challenge:factual] disc-<id> <断言原文>（证据放正文；可选 evidence= 显式标注）
[challenge:consistency] disc-<id> <断言原文>
[challenge:provenance] disc-<id> <断言原文>
[respond] disc-<id> status=<成立|部分成立|不成立> <理由>（可选 evidence=）
```

- 类别直接进前缀（`[challenge:factual]`），不再 `category=` 键值对——少一层摩擦
- `evidence=` 可省略，缺省取正文为证据（8/11 连带调整：**「缺 evidence」不再是 `challenge_invalid` 条件**，invalid 仅剩「缺 target/无 disc-id/status 非法」）

解析规则（`session/emerge.go` 新增 `challengeParse()`，与 commit_orphan 同模式但**输入是格式不是语义**）：
- 扫描最近窗口内 discussion_log 的 user/assistant 行，`content` 以 `[challenge]`/`[respond]` 开头才解析
- 正则提取前缀类别（`[challenge:<category>]`）、`disc-<id>`、`status=`；缺必填字段（disc-id、status 非法）视为**格式无效**，记录 `challenge_invalid` 事件（不计数）；evidence 宽松（缺省取正文，不判无效）
- 无前缀的普通对话（如「你的 challenge 是什么」）**不误报**——格式门保证
- 匹配/去重走 `dupEvent`，防止重复事件

### 3.3 终止条件（Claude D 建议）

**可计算收敛判定**：`未回应条目数 = 0 且 24h 内无新 challenge → 收敛`。

**计时起点（8/11 修订，原稿未定义）**：从最后一条 `[challenge]` 或 `[respond]` 消息的 created_at 起算；每次 emerge 扫描时判断「最后一条协议消息距现在 > 24h 且未回应数 = 0」→ 触发收敛报告。既不是从 challenge 起、也不是从 response 起，而是「协议消息静默窗口」。

收敛报告（自动生成，进 discussion + briefing）：
- 双方各认了几条、驳了几条（按 status 聚合）
- 未解决项 → 自动转 task（title 带 `[收敛遗留]` 前缀，挂质量评估 plan）
- 用户只拍板 task，**不读全文**（打破「用户当裁判」循环）

### 3.4 落点

- `session/` 新增 `convergence.go`（格式解析 + challenge 事件生成 + 收敛判定 + 报告）
- `session/emerge.go` 挂 `challengeParse()`：扫描 `[challenge]`/`[respond]` 前缀行 → 按格式提取字段 → 生成/关闭事件（**与 commit_orphan 同模式，但输入是机器可读格式，不是语义**）
- **不新增 MCP 工具**（低代码路线），收敛报告进 `aipmc metrics` 的 review 区块
- 无效格式事件（`challenge_invalid`）进 briefing，提示发言者补全前缀——保证协议自纠错
- **协议无人用信号（8/11 v1.2 降级为观察指标）**：N 天（默认 3 天）内无 `[challenge]` 前缀但存在数据断言类对话 → 计入 `aipmc metrics` 的 review 区块（`protocol_idle` 计数），**不进 briefing 事件**——正则判定「评审类对话」必然误报（「改了 5 个文件」也含数字），放进 briefing 等于多一个不可信来源（Claude 8/11 承认原设计打偏）；R1 阶段 A 达标后该信号自然缓解

### 3.5 验收

- R3.1：构造 1 条 challenge（含证据）→ 24h 无响应 → 事件出现在 briefing
- R3.2：响应全部标注 status → 收敛报告生成，未解决项转 task
- R3.3：报告含「各自认/驳计数 + 遗留 task 列表」

---

## 4. 转述移除（工作习惯，不建 task）

从 8/11 起双方执行：

1. 涉及**任务状态** → 先 `aipm_list_tasks` / `aipm_get_task`，不转述简报
2. 涉及 **commit 记录** → 先 `aipm_get_commit` / `aipm_list_commits`
3. 涉及**基线/指标** → 先 `aipm metrics ...`（R2 落地后）或 EVALUATION 原文
4. 涉及**口径** → 先查 METRICS_REGISTRY（R2.1）

原则：**争议事实直接查结构化接口，没有「你说 vs 我说」这一层**。
判断标准：转述前问自己「AIPM 能不能直接回答这个？」→ 能，则查；不能，才转述 + 标注 `[引用]`。

---

## 5. Task 拆分与排期

| Task | 内容 | 依赖 | 验收锚点 |
|------|------|------|---------|
| R1 | Review 断言规范（分级标签 + 强制命令片段） | — | R1.1/R1.2 + §0.5 R1 效果 |
| R2 | 统计口径规范 + 固化指标脚本 + 基线标注 | — | R2.1–R2.5 + §0.5 R2 效果 |
| R3 | 对质收敛协议（challenge/response + 终止 + 报告） | — | R3.1–R3.3 + §0.5 R3 效果 |

依赖关系：R1/R2/R3 相互独立可并行；R2 内部顺序：**基线标注 → 脚本 → 对账 → 观测门切换**。
建议归属：R1/R3 → `plan-20260807-092253-b2e614`；R2 → 同 plan（指标脚本同时服务协作感知 1c-B 观测门，与 `plan-20260804-175245-c10790` 关联引用）。

**排期优先级（8/11 v1.3）**：R2 立即排期（不盲——它的指标就是数据本身，等数据是逻辑错误）；R1/R3 以 **POC 形式**落地（采用率是行为假设，低承诺先行），按 §0.5 采样触发（10 次事件或 30 天）用数据裁决是否升级强制。口径是前两轮争议的最深根因，R2 先修。

---

## 6. 风险与回退

| 风险 | 概率 | 影响 | 回退 |
|------|------|------|------|
| 断言分级流于形式（全标实测） | 高 | 规范失效 | B 强化：实测必须附命令输出片段，无片段按引用 |
| 脚本对账差非 0（基线口径历史不明） | 中 | R2 验收卡住 | 先标注已知基线（A1 等），历史不明者标注「口径未知待重采」 |
| 收敛协议无人用 / 格式无效（agent 不按前缀发言） | 中 | 事件空洞 | 格式门：无前缀不计入；`challenge_invalid`（仅缺 target/status 非法）提示补全；`protocol_idle` 降级为 metrics 观察指标覆盖「完全不用」（8/11 v1.2：简化前缀降摩擦 + 观察指标防静默，噪声不进 briefing） |
| MCP 新工具成本 > 收益 | 低 | 过度工程 | 推荐 emerge 规则方案，不新增 MCP 工具 |

---

## 7. 遗留（不在本设计范围）

- EncryptDrive 下午事故的 F 层指标（构建红时长/吞并次数/重叠文件数）——建议入协作感知 plan 后续，作为 L1 感知的观测层
- Git 排他锁 / 提交前自检 gate（A 桶）——独立 task，本次设计只含「评审层」三件套
