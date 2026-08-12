# v1.13 Agent 协作感知 L1：假设与验证设计（data-first）

> 状态：**定位变更（8/10 data-first 收敛）**——本文件不再是「实施设计」，而是「**假设清单 + 验证设计 + 决策矩阵**」。实施 task 拆分仅在数据裁决后启动。
> 基线：`docs/agent-collab-design.md` v1.12（8/5 定稿，commit cd78c5e）
> 日期：2026-08-10
> 原则：EVALUATION §0.1 每个目标必须有可计算量化指标；§0.3 先记录基线 → 定目标 → 差距即待办
> **8/10 变更**：用户指出「建方案是否也早了」——方案核心参数（碰触频率/peer 动态/缓存断点）全部依赖尚未存在的 rel_path 数据。故将本文件降级为验证矩阵；T1/T2（已验证系统问题修复）保留排期，T3b/T4/1c 全部「待数据裁决」。

---

## 0.1 待验证假设清单（数据裁决矩阵）

**裁决原则**：每项假设标注数据来源 + 当前状态；状态=A 已测实 / P 待测 / X 被数据推翻。数据裁决前，对应设计不进入实施。

| # | 假设 | 当前数据 | 状态 | 裁决后分支 |
|---|------|---------|:--:|-----------|
| H-A1 | codex 注入伤缓存（injected=Y 命中率低） | 36.9% vs 97.9%（8/10 测，token 加权） | **A** | 修复性切 user prompt（POC-1 ≥85% 时） |
| H-A2 | claude 注入伤缓存轻微 | -8pp（88.1% vs 92.8%） | **A** | 保持 system 注入 + 观测门 |
| H-A3 | 注入内容稳定度：claude 35% / codex 6% 注入率 | [INJECT] 6.9 万行 | **A** | peer 段可复用 same_content 去重 |
| H-A4 | 折叠计数低动态（91min 变一次） | 7 天滑窗，12 活跃段 | **A⚠️** | 样本稀疏（12 段），仅作方向参考 |
| H-A5 | 碰触段「平时零占用」 | 12 次/7 天（**仅 claude 覆盖**） | **X** | 有偏，方向未知；codex file_path 盲区需补采 |
| H-A6 | 缓存断点位置（user 前/后） | 无直接数据 | **P** | POC-1 天然观测回答 |
| H-A7 | 跨 session 缓存共享价值 | 无直接数据 | **P** | POC-1/POC-2 回答 |
| H-A8 | codex metadata 完整率 100% | 两库实测 | **A** | H4-A 归因修正（非写入失败） |
| H-A9 | claude tool 空串 35% 是写入失败 | Read(543)+MCP(164) 本应空 | **A（推翻）** | 真问题仅 Bash/Edit 需查；Read 补 metadata 属 T4 |
| H-A10 | json_extract 对空串报错 | 实测 `json_extract('')` → malformed JSON | **A** | T2 需防护（保留） |

**已裁决结论**（数据支撑，非推断）：
- 决策 44（codex 分支）基线已测实：36.9% vs 97.9%
- H4-A 归因已修正：codex 非「数据地基空」，claude tool 空串是 Read/MCP 本应空
- 决策 46（碰触频率）**方向未知**，无捷径——codex 数据需动代码才可采（codex hook 无 file_path）

---

## 0.2 排期状态（8/10）

| Task | 内容 | 状态 | 依据 |
|------|------|------|------|
| T1 | H1 DB 写入协调 | ✅ **排期** | 8/7 已验证系统问题（SQLITE_BUSY 83%），与方案 C 无关 |
| T2 | H2 hook 写入可观测性 | ✅ **排期** | 已验证系统问题（tool_response 丢 37 条、json_extract 空串报错）；验收去掉「35%→<10%」假想病目标 |
| T3 | H3 协议矩阵测试 + src 归一化 | ✅ **排期** | 493bbdf 已修代码，补测试固化 |
| T3b | H4 codex 感知缺口 | ⏸ **待数据裁决** | 依赖 POC-1 分支（codex 是否参与感知） |
| T4 | Phase 0 rel_path | ⏸ **待数据裁决** | 方案 C 本身是要验证的假设，不可当验证手段 |
| POC-1 | codex 注入位置 + metrics 视图 | ✅ **定义完成** | 定界明确（locateLastUserMessage + 开关 + 视图），半天观测 |
| T8-T12 | 1c-A / 1c-B / Phase 2-4 | ⏸ **待数据裁决** | 全部参数依赖 rel_path/POC 数据 |

---

## 0. 变更摘要（v1.12 → v1.13）

| # | 变更 | 来源 | 影响 |
|---|------|------|------|
| C1 | **新增 §H「DB 写入协调」**：SQLITE_BUSY 是实测最大错误源（45/54=83%，a3a1453），git 锁（v1.12 §4.4）不解决同库并发 | 8/7 审计 + 8/10 讨论 | 硬前提，Phase 0 前置 |
| C2 | **hook 写入可观测性前置（字段级）**：tool_response 数组形态曾静默丢 37 条事件（23addfd）；方案 C 全靠 hook 写 rel_path，写入失败不可见则感知不可信。实测 claude tool role 35% metadata 空串 | 8/7 审计 + 8/10 二次核实 | 硬前提，Phase 0 前置 |
| C3 | **协议矩阵补 OpenAI Responses**：codex 8/6 起走 /v1/responses，input 数组未解析导致静默 0 文件（493bbdf 已修代码，文档未跟） | 8/10 讨论 | 硬前提 |
| C4 | **src 归一化表**：codex-mcp-client → codex-cli（1af89c8 已修代码），幽灵过滤与按 agent 统计基于归一化 src | 8/10 讨论 | 硬前提 |
| C5 | **决策 23 重估：按 agent 灰度 + POC**。实测 codex injected=Y 命中率 36.9%（vs 未注入 97.9%，-61pp），claude 仅掉 ~8pp——不再是「user prompt 一刀切必做」 | 8/10 数据 | Phase 1c 拆 1c-A/1c-B |
| C6 | **注入健康度门槛**：同行感知按 agent 的 hook 写入健康度（非 quality_score，后者是工作流规范性分）做注入开关 | 8/10 讨论 | Phase 1c-A |
| C7 | **碰触段频率实测 12 次/7 天**（ED 库，方法待修正——codex file_path 覆盖仅 5%，统计漏报跨 agent 碰撞） | 8/10 讨论 | Phase 1c-A 节流校准 |
| C8 | **跨 session 缓存共享观测（POC-2）**：注入 block 稳定性的跨 session 命中价值，方向取决于缓存断点位置（未知） | 8/10 讨论 | POC |
| C9 | **guessSelf 简化**：proxy 已知 agent 类型（[LLM] agent= + mcpClientName），退化为「同 agent 类型 + 最近活跃 + 时间窗口」 | 8/10 讨论 | Phase 2 |
| C10 | **执行纪律**：Phase 0/1a 排期不等 1c（POC 只影响 1c 写法） | 8/10 讨论 | 排期 |
| C11 | **E5 备注修正**：「codex 恒 0」过时——实测 8,290/8,328 行非 0，E3 已含 codex 数据 | 8/10 数据 | 文档修正 |

---

## 1. 实施顺序总览（依赖图）

```
硬前提 H1/H2/H3/H4（T1-T3、T3b，H4 依赖 H2）
   │
   ▼
Phase 0  rel_path 数据地基（T4）
   │
   ▼
Phase 1a 预算重构（T5）── Phase 1b cwd 修复（T6）   [可并行]
   │
   ▼
POC-1 codex 注入位置 + POC-2 缓存断点（T7）
   │
   ▼
Phase 1c-A peer 段 + 碰触段 + 冲突机制（T8，codex 分支由 POC-1 决定）
   │
   ▼
Phase 1c-B user prompt 注入（T9，观测门/修复性切换）
   │
   ▼
Phase 2 guessSelf 简化（T10）→ Phase 3 MCP task_id（T11）→ Phase 4 aipm_get_session（T12）
```

关键依赖：
- T4（rel_path）依赖 T1（DB 协调）+ T2（可观测性）——写库可靠且可见后才有可信数据
- T8（1c-A）依赖 T7（POC-1 决定 codex 分支：修复性切换 / 不注入 / 低频注入）
- T9（1c-B）依赖 T8（claude 观测门基线在 1c-A 上线前采集）
- T1-T3、T5、T6 不依赖 POC，可立即排期（C10）

---

## 2. 硬前提（Phase 0 前置，T1-T3）

### H1. DB 写入协调（T1）

**背景**：8/7 实测 review/reconcile 错误 49 次中 45 次 SQLITE_BUSY（83%）。已修：
- store 层 `retryOnBusy`（store/store.go:19）
- pipeline 层 `retryPipelineBusy`（session/auto.go:112，指数退避 500ms/1s/2s）

**缺口**：hook 是独立进程连库（Claude Code / Codex CLI 的 hook 每事件一次进程），
四入口（hook_claude / hook_codex / hook_gemini / hook_opencode）的单点写入是否
都被 `retryOnBusy` 覆盖，未审计。

**改动**：
1. 审计四个 hook 的写入调用链，缺失的补 `retryOnBusy`（复用 store/store.go:19）
2. 确认 db.go DSN 已配 `busy_timeout` 与 WAL（`journal_mode=WAL`，注意 modernc
   驱动 pragma 语法——8/7 FTS5 已踩坑，用 `_pragma` 形式）
3. 并发写压测：2 进程 × 1000 次并发写，0 丢失

**验收**：
- hook 四入口写入均带 BUSY 重试（代码审查 + 压测）
- 压测 0 丢失、0 报错
- 重启后 `E8 review_error` 周环比下降（目标 <5/天）

### H2. hook 写入可观测性（T2，字段级）

**背景**：
- `23addfd`：Claude tool_response 数组形态导致 37 条整事件静默丢弃
- 本库实测 discussion_log 19,728 行中 **5,036 行（25.5%）metadata 为空串**
  （json_valid=0，json_extract 报 malformed JSON——8/10 复核可复现）
- **归因（8/10 二次核实修正）**：空串中 claude user role 986/986（100%）与部分
  assistant 为对话消息本应空；**真正写入缺失是 claude `tool` role 1,675/4,798
  （35%）**——工具调用应带 metadata 却为空（ED 库同构 36%）
- 方案 C 依赖 hook 写入 rel_path，写入静默失败 = 感知漏报且不可见

**改动**：
1. hook 写入成功/失败计数：`LogShared("HOOK", "write ok=.. fail=.. src=..")`
2. metadata 写入前 `json.Valid` 检查 + 非法率统计
3. 存量空 metadata 行处理（脚本 + 记录；空串与合法 JSON 区分统计）
4. 查询端容错：`json_extract` 遇非法行跳过 + LogShared 计数（不 panic）
5. rel_path 缺失告警：Phase 0 上线后按 agent 统计覆盖率，低于阈值告警

**验收**：
- metadata 合法率 ≥99.5%（可复测指标，纳入 metrics）
- hook 失败 100% LogShared 可见（fail-open，8/7 已定）
- 查询端在脏数据下不 panic、不漏查（回归测试）

### H4. 感知数据地基缺口：两个独立问题（T3b，8/10 二次核实修正归因）

**背景（8/10 二次核实）**：最初假设「codex 数据地基空」被数据否定——
aipmc 库 codex-cli 6,393 assistant + 534 user **metadata 空串 = 0（完整率 100%）**，
ED 库同构。真正的地基缺口是**两个不同的问题**：

| 问题 | 数据（aipmc 库） | 性质 |
|------|-----------------|------|
| **A. claude `tool` role metadata 缺失** | 1,675/4,798 空串（35%）；ED 同构 36% | hook 写入缺失（工具调用应带 metadata 却空） |
| **B. codex 无 `file_path` 字段** | codex 工具调用 3,396 条仅 171 条含 file_path（5%）；ED 1.1% | 非 metadata 缺失，是 hook 解析问题（Bash/apply_patch 无结构化路径） |

**改动**：
1. A：claude hook 写入补 `tool` role 的 metadata（定位 tool_response 空 metadata
   根因，复用 H2 可观测性先量化）
2. B：codex hook 的 Bash 命令可稳定解析路径补 rel_path（决策 19 覆盖边界，
   复用 extractFilePaths；8/7 C2 已修 responses input 解析）
3. 两指标分开跟踪：claude tool 空串率、codex file_path 覆盖率，均进 H2 报表

**验收**：claude tool role 空串率 35% → <10%；codex 结构化操作 file_path
覆盖率 5% → 按决策 19 覆盖边界设定（接受漏检）；两指标可见可告警。

**定位**：H4 是 H2 的下游（可观测性先落地才能量化），T4（rel_path）前置。



### H3. 协议矩阵 + src 归一化（T3）

**改动**：
1. `extractFilePaths` 协议矩阵固化为回归测试 4 例：anthropic messages /
   openai chat / **openai responses input** / gemini systemInstruction
   （responses 解析已由 493bbdf 实现，补测试即可）
2. src 归一化表：`codex-mcp-client → codex-cli`（mcp.go:81 已有 1af89c8），
   补全所有按 src 统计/过滤的路径（metrics、幽灵过滤、同行感知）
3. EVALUATION.md E5 备注修正（C11）：codex cache_hit 已可解析，非恒 0

**验收**：4 例协议测试 PASS；按 agent 聚合无 src 分裂（codex-mcp-client
并入 codex-cli 后 E5/E6 数字不变或有说明）

---

## 3. Phase 0：rel_path 数据地基（T4）

### 3.1 写入端（hook）

| agent | 现状（决策 18） | Phase 0 改动 |
|-------|----------------|--------------|
| claude | Edit 有 file_path / Read 空 | Edit → rel_path；**Read 补 metadata**（决策 15） |
| codex | 无结构化 file_path | 结构化工具 + 可稳定解析的简单 Bash 操作补 rel_path（决策 19 覆盖边界，复用 extractFilePaths 能力）；复合命令不写（宁缺勿脏） |
| cursor | 全有 | 低优先（决策 20），补 rel_path 即可 |
| gemini/opencode | 部分 | 同 codex 策略 |

归一化规则（决策 1/2）：绝对路径 → 相对项目根（RuntimeDir()）；项目外文件不写。

### 3.2 查询端（store）

- 封装 `GetRecentFileSessions(relPath, since, limit)`：
  `json_extract(metadata,'$.rel_path') = ?` 精确匹配（决策 16）
- 脏数据容错（H2 的 4）
- driver 兼容测试（modernc `_pragma` 教训——json_extract 行为 + FTS5 回填）

### 3.3 验收（量化）

| 指标 | 基线 | 目标 |
|------|------|------|
| claude Edit/Read rel_path 覆盖 | 15%（file_path 行占比，本库实测） | ≥90% |
| codex 结构化操作 rel_path 覆盖 | 5%（本库实测 171/3396） | 按决策 19 覆盖边界设定，验收接受漏检 |
| 「谁最近碰了 X」查询 | 无 | 返回 session 列表（含时间/agent/op） |
| metadata 合法率（非空且 valid） | 74.5%（14,692/19,728，8/10 复核） | ≥99.5% |
| 冷启动 | — | Phase 0 落地后积累 1-2h 数据再验同行段（v1.12 已定） |

---

## 4. Phase 1a：注入预算重构（T5）

**原则（C10）**：直接扩展 8/7 的 budget-driven formatter（`formatActionItems`：
优先级排序 + 同类聚合 + perTypeCap=5 + actionItemCeil=10），**不写第二套**。

**改动**（proxy/context_inject.go `buildContextBlock`）：
1. 段预算表定稿（方案 §5.3，用 8/7 实测 chars=679 校准）：

   | 段 | 预算 | 优先级 |
   |----|------|--------|
   | [文件碰触]（1c-A 后） | 100 | 1（第一优先） |
   | [项目编码规范] guidelines | 600 | 2 |
   | ⚠️ 待处理 actionItems | ceil 内 | 3 |
   | [同行 agent]（1c-A 后） | 50 | 4 |
   | [文件关联] fileAssoc | 200 保留 | 5 |
   | 最近的 session goals | 剩余 | 6 |

2. 同一套「优先级 + 聚合 + cap」机制扩展到 peer 段与碰触段
3. 回归测试：guidelines 满 600 时高优段不被挤掉

**验收**：inject_coverage ≥80%（基线 96.7%）；注入 chars ≤800；budget 回归测试 PASS

---

## 5. Phase 1b：cwd 修复 + GIT_GC_AUTO=0（T6）

- extractFilePaths 相对路径解析用 RuntimeDir() 定位项目根（决策 10a）
- 环境注入 `GIT_GC_AUTO=0`（防 worktree 并发损坏）
- 验收：serve 与独立 proxy 两模式路径转换一致；与 Phase 0 归一化 rel_path 对得上

---

## 6. POC（T7，1c-A 前必做）

### POC-1：codex 注入位置验证

**背景**：codex injected=Y 命中率 36.9%（vs 97.9%，token 加权，全天持续非短期波动）。
机制未明：instructions 位于 responses 缓存前缀最前部（假说 A）vs DeepSeek responses
缓存粒度本身（假说 B）。

**改动**：
1. 新函数 `locateLastUserMessage(input []any)`：定位最后一个 `type=message` 且
   `role=user` 的元素（extractFilePaths 只聚合文本不查 role，需新写——方向相反）
2. 开关 `INJECT_USER_PROMPT=codex-only`：injectCodex 从「instructions 追加」切到
   「最后一个 user message content 末尾追加」；无 user message 时跳过（§5.5）
3. metrics 加「按 agent × injected 拆分 cache_hit_rate」视图（POC 观测工具化，
   现有 `[LLM]` 行字段齐全，仅展示层）

**基线**：codex injected=Y 36.9% / injected=N 97.9%（已测）

**判定**（半天观测）：
- injected=Y 命中率 ≥85% → 假说 A 成立，codex 修复性切换 user prompt（T9 中 codex 直接切）
- <85% → 假说 B，走降级路径（见 §8 风险）

**防误判**：同时统计「定位失败跳过数」——跳过导致注入率下降会虚高命中率，
判定用 injected=Y 子集而非全量。

### POC-2：跨 session 缓存断点验证（8/10 审核修正：改为天然流量观测）

**背景（C8）**：注入 block 稳定性的跨 session 命中价值，方向取决于 DeepSeek
缓存断点是否包含最新 user message——完全未知。

**修正**：原「构造相同 block + 不同 user 合成请求」不可行——block 动态
（fileAssoc 每请求现算），真实流量无「相同完整 block」对比对；合成请求测出的
断点位置在真实流量未必复现（Claude 8/10 审核）。

**改为天然观测**（无需开关、无合成流量）：
1. 现有数据直接对比：codex `injected=N`（block 未变）97.9% vs `injected=Y`
   （block 变）36.9%——「block 稳定性 ↔ 命中率」的相关性基线已存在
2. 观测「同 session 相邻 injected=Y 请求命中率」是否随 block 重复而上升
   （block 重复 → 命中升，说明跨请求前缀共享；不升 → 断点含 user/按 session）
3. 输出与 POC-1 合并分析：POC-1 切 user prompt 后，若命中恢复且 block 重复
   上升，则 user prompt 注入在断点之后、不破坏共享

**输出**：决策 23 天平的实测砝码，写入 v1.13 决策记录。

---

## 7. Phase 1c-A：peer 段 + 碰触段 + 冲突机制（T8）

### 7.1 注入段（全部 agent）

- `[同行 agent]`：折叠计数（50 预算，集合驱动，30min active 窗口——决策 9a）
- `[文件碰触]`：条件段（100 预算，第一优先，5min COLLISION_WINDOW——决策 9a），
  同文件节流 5min 一次（决策 26 节流复用）
- 幽灵过滤：`📡%` + `HAVING users>0`（store/discussion.go 已落地）+ src 归一化（H3）
- **注入健康度门槛（C6）**：同行感知注入前查该 agent「最近 1h hook 写入成功率」，
  低于阈值（如 90%）则不注入同行段（数据残缺时宁缺勿误导）

### 7.2 冲突机制（v1.12 §4.3-4.6，不重设计）

| 机制 | 适用 agent | 阶段 |
|------|-----------|------|
| PreToolUse 双条件 deny | Claude 专属 | 1c-A |
| git wrapper 排他锁（TTL 120s + 续期，checkout 移除） | 全部（被动） | 1c-A |
| scope.json 域校验（warn 双方 / block Claude） | 全部/Claude | 1c-A |
| worktree 编译隔离（门槛=编译互踩≥3 次/2 sprint） | Claude 专属 | 1c-A（后置） |

### 7.3 codex 分支（由 POC-1 决定）

- POC-1 通过：codex 注入保持 system（instructions），T9 修复性切换 user prompt
- POC-1 未通过：codex 降级——关闭 codex 同行注入（§2.4 加 codex 例外）或低频注入
  （仅碰撞窗口内），并在 E3 加 codex 专项监控

### 7.4 验收

- 两 agent 同跑，各自看到对方折叠计数；改同一文件双方看到碰触告警
- 幽灵 session 不出现（unknown 过滤）
- 低健康度 agent 不出现在同行段（门槛生效）
- claude 编辑冲突文件被 deny；git 写操作取锁；commit 越界被 warn

---

## 8. Phase 1c-B：user prompt 注入（T9）

### 8.1 实现就绪（1c-A 同期交付，开关默认关）

- `injectAnthropic` / `injectOpenAI` / `injectGemini` 改为「最后一个 user message
  content 末尾追加」（§5.5 表）；无 user message 时跳过
- 与 block 构建器解耦：注入点策略（system append / user prompt append）由配置选择，
  共享同一个 block 构建与 same_content 逻辑
- codex 分支：locateLastUserMessage（POC-1 产物）复用

### 8.2 切换决策（两类，逻辑不同）

| agent | 类型 | 触发 |
|-------|------|------|
| **codex** | 修复性 | POC-1 通过 → 直接切换（不等观测门，已知灾难） |
| **claude** | 预防性 | 观测门：injected=Y 命中率相对 1c-A 前基线下降 ≥ 阈值且持续 1 天 |

**观测门基线口径**（必须先定，88-92% 随口径波动 3-4pp）：
- agent 精确过滤（agent=claude）+ 固定时间窗口 + token 加权
- 阈值用历史日粒度分布的 σ 定（如 均值 −2σ），不手拍
- 切换成本低（开关），故规则从简可解释

### 8.3 验收

- 开关切换后注入块位置正确（4 协议回归测试）
- codex 切换后 injected=Y 命中率 ≥85%（POC-1 判定延续）
- claude 切换触发与否有指标依据（观测门记录可审计）

---

## 9. Phase 2：guessSelf 简化（T10）

**C9**：退化为「同行列表里同 agent 类型 + 最近活跃 + COLLISION_WINDOW 时间窗重合」
→ 排除自己。依赖 [LLM] `agent=` 识别 + mcpClientName（均已落地）。

**验收**：同行段不包含自己（单 agent 场景同行段为空）；多 agent 场景排除正确。

---

## 10. Phase 3/4：MCP task_id + aipm_get_session（T11/T12）

沿用 v1.12 设计（§4.2 / Phase 3 / Phase 4），无变更：
- Phase 3：mcpLogDiscussion 内提取 task_id → discussion_log（📡 前缀，不进同行感知）
- Phase 4：`aipm_get_session` MCP 工具 + INJECT 引导钩子

---

## 11. 决策日志增量（v1.13，决策 40-50）

| # | 决策 | 状态 |
|---|------|------|
| 40 | **DB 写入协调章节**：SQLITE_BUSY 是主错误源（83%），hook 四入口补 retryOnBusy + WAL/busy_timeout 确认；git 锁（§4.4）优先级降级为 DB 协调之后 | ✅ 共识 |
| 41 | **hook 写入可观测性前置**：字段级成功/失败计数 + metadata 合法率 + rel_path 缺失告警；tool_response 数组形态等解析失败必须可见（fail-open） | ✅ 共识 |
| 42 | **协议矩阵补 OpenAI Responses input**：extractFilePaths 4 例回归测试固化 | ✅ 共识 |
| 43 | **src 归一化表**：codex-mcp-client → codex-cli，统计/过滤基于归一化 src | ✅ 共识 |
| 44 | **决策 23 重估**：不再一刀切。codex 36.9%（-61pp）→ POC 验证后修复性切换；claude ~90% 健康 → 观测门。user prompt 注入代码就绪但默认关 | ✅ 共识 |
| 45 | **注入健康度门槛**：同行感知按 agent hook 写入成功率（非 quality_score）开关 | ✅ 共识 |
| 46 | **碰触段频率实测 12 次/7 天 = 有偏结论（Claude 8/10 认错）**：统计仅覆盖 claude（codex file_path 1.1%，编辑未入 tool）；「零占用」暂缓，真实基线待 rel_path + H4 后重采 | ✅ 共识 |
| 47 | **跨 session 缓存共享观测**：POC-2 构造「相同 block + 不同 user」请求对，判定缓存断点位置 | ✅ 共识 |
| 48 | **guessSelf 简化**：agent 类型 + 最近活跃 + 时间窗，替代完整启发式 | ✅ 共识 |
| 49 | **执行纪律**：Phase 0/1a 排期不等 1c，POC 只影响 1c 写法 | ✅ 共识 |
| 50 | **E5 备注修正**：codex cache_hit 非恒 0（8,290/8,328），E3 已含 codex 数据 | ✅ 共识 |
| 51 | **H4 感知数据地基缺口（二次核实修正归因）**：A=claude tool role 35% metadata 缺失；B=codex 无 file_path 字段（metadata 完整率 100%）。两问题独立跟踪，Phase 0 前置 | ✅ 共识（8/10 二次核实） |

---

## 12. Task 拆分与排期

| Task | 内容 | 依赖 | 验收锚点 |
|------|------|------|---------|
| T1 | H1 DB 写入协调 | — | 压测 0 丢失；E8 <5/天 |
| T2 | H2 hook 写入可观测性 | — | 合法率 ≥99.5%；失败可见 |
| T3 | H3 协议矩阵测试 + src 归一化补全 | — | 4 例测试 PASS；E5 备注修正 |
| T3b | H4 数据地基（A: claude tool 空串率修复；B: codex Bash 路径提取） | T2 | claude tool 空串 <10%；codex file_path 覆盖提升；指标可见 |
| T4 | Phase 0 rel_path（写入 + 查询 + 容错） | T1+T2+T3b | 覆盖率目标；查询返回列表 |
| T5 | Phase 1a 预算重构 | T4 | coverage ≥80%；chars ≤800 |
| T6 | Phase 1b cwd 修复 + GIT_GC_AUTO=0 | T4 | 两模式路径一致 |
| T7 | POC-1 + POC-2 + metrics 工具化 | T5+T6 | 半天观测；判定记录 |
| T8 | Phase 1c-A（peer + 碰触 + deny/git锁/scope） | T7 | §7.4 验收 |
| T9 | Phase 1c-B（user prompt + 观测门） | T8 | §8.3 验收 |
| T10 | Phase 2 guessSelf 简化 | T8 | 同行段无自己 |
| T11 | Phase 3 MCP task_id | T8 | task 关注度可查 |
| T12 | Phase 4 aipm_get_session | T8 | 深挖可用 |

**立即排期（不等 POC）**：T1-T6（C10）。T7 起依赖 POC，但 T7 本身可在 T5/T6
进行中并行准备（开关 + locateLastUserMessage + metrics 视图）。

---

## 13. 风险与回退

| 风险 | 概率 | 影响 | 回退 |
|------|------|------|------|
| POC-1 未恢复（DeepSeek responses 缓存问题） | 中 | codex 感知不可用 | codex 关闭注入 / 低频注入（§7.3） |
| POC-2 显示断点包含 user | 中 | user prompt 注入破坏跨 session 共享 | system 注入 + 稳定 block 优先（决策 23 保持现状） |
| 碰撞统计低估（codex 无 file_path） | 高 | 碰触段节流参数不准 | H4-B 修复后重采基线（决策 46/51） |
| 观测门口径漂移 | 中 | 误触发/漏触发 | 固定口径 + σ 阈值 + 可审计记录 |
| 两套注入逻辑维护成本 | 低 | 死代码 | 注入点策略开关共享测试（§8.1） |

---

## 14. 遗留问题（交给实施中验证）

1. codex 与 claude 注入后命中率差异的机制根因（claude -8pp vs codex -61pp）——
   POC-1/POC-2 数据回答，不提前归因
2. DeepSeek 缓存是否跨 session——POC-2 直接回答
3. 碰触段真实频率——rel_path 落地后重采（决策 46）
4. 上游是否可能切回 Anthropic 官方 API——90.4% 是 DeepSeek 行为，切换时决策 23
   需重新评估（C5 的上游依赖备注）

