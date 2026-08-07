# AIPM 项目质量评估框架 — 审核目标清单 v1

> 目的：为整个项目建立**可量化、可预见**的评估体系。任何代码修改都应能通过埋点/数据验证效果——"再好的系统构建也需要反馈"。
> 依据：项目记录（commit message、decision、discussion）+ 代码埋点 + EncryptDrive 实际运行数据（2026-08-07 采集基线）。

---

## 0. 评估原则

1. **反馈闭环**：每个目标必须有可计算的量化指标，禁止"感觉有效"式评估。
2. **依据溯源**：每个目标标注设计意图来源（commit / decision / discussion），评估时对照"当初为什么做"。
3. **基线驱动**：先记录基线 → 定目标值 → 差距即待办。基线随评估滚动更新。
4. **分层评估**：按架构闭环（捕获 → 理解 → 注入 → 行为 → 系统）分层，任一层断裂都导致整体失效。

## 1. 评估维度总览

| 层 | 维度 | 目标数 | 一句话 |
|----|------|:--:|--------|
| A 捕获层 (L0) | Hook / 数据完整性 | 4 | Agent 的一切行为是否被完整记录 |
| B 理解层 (L1-L3) | Pipeline 质量 | 9 | 记录是否变成可用的"理解" |
| C 注入层 | INJECT / 反馈 | 4 | 理解是否及时回到 Agent 视野 |
| D 行为层 | Agent 响应闭环 | 4 | 注入是否改变 Agent 行为 |
| E 系统/工程层 | 观测 / 稳定 / 成本 / 协议 | 4 | 系统本身是否健康可持续 |

## 2. 审核目标清单

### A. 捕获层（L0）— Hook 与数据完整性

**A1. Hook 平台覆盖完整性**
- 设计意图：Phase0「Hook 全项目验证」；`9e4823c` opencode role 修复；`ca66e47` MCP 全文修复
- 量化指标：各 source 的 `discussion_log` 消息数 / session 数；hook 失败率（日志 `[HOOK]` 228 条中的 error 占比）
- 数据源：`discussion_log.source`；`~/.aipmc/logs/aipmc.log`
- 基线：claude-code 18634 / codex-cli 12977 / cursor 1583 / opencode 1361 / aipmc-vision 374（消息数）；114 个 session
- 目标值：所有启用平台的 source 均有持续增长的数据；无整段 session 缺失
- 备注：cursor/opencode 消息量远低于 claude/codex——需确认是使用量差异还是漏录

**A2. 数据时效性**
- 设计意图：理解层自动运转（`3211f1e`）的前提是数据及时落库
- 量化指标：`discussion_log` / `graph_edges` / `session_summaries` 的 MAX(created_at) 与当前时间差
- 数据源：各表 created_at
- 基线：8/6 17:36 仍在更新（<30min 延迟）
- 目标值：延迟 < 30 分钟（pipeline 周期 30m）

**A3. 会话完整性**
- 量化指标：session 数 vs 实际会话数；消息数/session 分布
- 数据源：`discussion_log`
- 基线：34929 消息 / 114 session（平均 306 条/session）
- 目标值：无空 session、无单消息 session 异常堆积

### B. 理解层（L1-L3）— Pipeline 质量

**B1. L2 摘要覆盖率** 🔴 差距最大
- 设计意图：`task-20260616-154916-5a28d5`「Observer shadow run 验收 (workflow_completed≥85%)」
- 量化指标：`session_summaries` / `DISTINCT(session_id) in discussion_log`
- 数据源：EncryptDrive pmai.db
- 基线：**63/114 = 55%**（51 个 session 无摘要）
- 目标值：≥85%（设计验收线）
- 备注：无摘要的 51 个 session 需排查（AI 不可用 / session 太短 / 归类失败）
- **8/7 根因排查（B1 归因）**：281 次 L2 summarize error = **225 次 DeepSeek 401**（`api.deepseek.com` 返回 `Authentication Fails (governor)`，config.json 无 `ai_api_key` 字段、env 无 `AI_API_KEY`）+ **55 次本地网关拒连**（aipmc 项目配 `localhost:8080` llama-server 未启动）+ 1 次 EOF。8/7 已修：`LoadConfig`/`ReloadAI` 支持 `ai_api_key`（config.json 字段，env 优先）；**剩余依赖环境**：EncryptDrive config.json 填入有效 DeepSeek key，或启动 llama-server:8080 后重跑 pipeline 复测覆盖率

**B2. L2 goal 质量**
- 设计意图：`7ec9826` L2 goal unnest（剥离嵌套 JSON）
- 量化指标：goal 为嵌套 JSON（`"goal":"{"...`）的比例；goal 可读性（人工抽样）
- 基线：**63 条中 8 条残留嵌套（12.7%）** ⚠️（6/29×3、7/21×1、7/24×2、7/31×1、8/6×1；来源含 opencode/claude/codex）
- 检测口径（8/7 复核）：**双列**——`review_json.goal`（解析后结构化字段）与 `summary`（L2 模型原始输出 JSON）。1.4 只清了 `review_json.goal`（0 残留），但 `summary` 列写入路径未 unnest，8/7 复核（Claude）发现另有残留
- 根因：`unnestGoal`（`7ec9826`）只作用于注入缓存读取路径；**写入路径 `session/run.go` 将 `GenerateL2Summary` 原始输出直接落库**，且历史脏摘要被 L2 缓存（`1359bd0`）复用——8/6 及 8/7 新数据残留即 `cached`/新写入复存
- **8/7 修复（Claude 复核确认）**：① 新增 `session.NormalizeSummaryGoal`（写入前对 summary JSON 的 goal 字段 unnest）接入 `run.go` 生成/缓存两条路径；② 存量清理：EncryptDrive 4 条（Claude 检测的 3 条 + 1 条 ```` ```json ```` 变体）、aipmc 0 条；③ 单测 `session/summary_unnest_test.go` 5/5 PASS
- 目标值：嵌套率 = 0（**已达成**：8/7 双口径复测两库均为 0；后续新数据由写入归一化兜底）

**B3. L2 缓存效率**
- 设计意图：`1359bd0` + `652d18a`（监控目标 2：缓存节省多少 LLM 调用）
- 量化指标：日志 `L2 summary cached=N new=M` 的 M/N 比
- 数据源：`~/.aipmc/logs/aipmc.log`
- 基线：cached=N new=0（100% 命中，重复扫描零额外 LLM 调用）✅
- 目标值：new=0 常态（新 session 除外）
- 备注：缓存命中会复用历史脏摘要（见 B2 根因），缓存读取需带 unnest 兜底

**B4. L3 Reconcile 产出与质量**
- 设计意图：`task-20260727-133003-10d121`「Baseline 数据收集 — reconcile 产出质量验证」（8/7 完成）
- 量化指标：commit↔session 自动链接数（relates_to 边）；tentative_link 假阳性率
- 数据源：`graph_edges`、`events(type=tentative_link)`
- 基线（8/7 采集）：relates_to **867** 条（commit→session 665 / commit→task 128 / discussion 相关 74 / task→bug 1）；file_touch 621 条；same_session 11141 条；fixes 3 条
- 质量验证：随机抽样 5 条 commit→session 边，evidence 均为 2+ 真实文件交集，**无假阳性** ✅；`evidence_json` 含 `note`+`via`，证据可溯源
- 目标值：tentative 低置信度链接占 auto_linked 比例 < 30%
- 结论：tentative_link **116/867 = 13.4% < 30% ✅**

**B5. graph_edges 覆盖率**
- 设计意图：`3150030` IDF-weighted file_touch；Activity 图数据源
- 量化指标：有边的 commit / 总 commit
- 数据源：`graph_edges`
- 基线：**233/871 = 26.7%**
- 目标值：活跃 session 涉及的 commit 全覆盖（≥80%）
- **8/7 确认（Claude Challenge 5 + 代码验证 `session/graph.go`）：只对有 session 关联的 commit 建边是设计如此**——覆盖率口径应为"有 session 关联的 commit 中已建边比例"，非"全部 commit 比例"；从 bug 清单移除

**B6. emerge 事件质量（去重）**
- 设计意图：`80f02c7` commitOrphans 双向检查；`2d1587d` tentative dedup
- 量化指标：事件数 vs 唯一 entity_id 数（重复率）
- 数据源：`events`
- 基线：commit_orphan **660 事件 / 137 唯一实体（重复率 480%）** ❌；tentative_link 114 条 0 重复 ✅
- 目标值：重复率 < 10%
- 根因：`dupEvent` 只查 unconsumed 事件（已确认 bug）

**B7. Cross-session knowledge**
- 设计意图：`3211f1e` P1-2 recent_lessons JSON 化
- 量化指标：patterns / clusters 数量；common_issues 是否命中真实问题
- 数据源：`.pmai/cache/recent_lessons.json`
- 基线：11 file patterns + 5 entity clusters（8/6 17:35 生成）✅
- 目标值：每周 review 时人工确认 ≥1 条 pattern 可转化为行动

**B9. L2 摘要准确率**（Claude Challenge 2 新增）
- 设计意图：错误摘要比没摘要更危险（幻觉会让 Agent 基于错误信息决策）；现有质量门只过滤空输出（`len(goal)>=5 && root_causes>0`），不过滤幻觉
- 量化指标：随机抽样 N 条 L2 goal 人工对照原始 session 的准确率
- 数据源：`session_summaries` vs `discussion_log`
- 基线：**8/7 抽样 3 条（claude×2 + vision×1）初步与 session 实际工作相符，但未正式评估**（5 条人工对照未执行）
- 目标值：准确率 ≥ 80%；< 80% 则 L2 优先级从"提覆盖率"改为"修质量"
- 备注：goal 多来自 session 中段指令而非首条消息，抽样须看完整 session 主题

**B8. L0 Hook 失败率**（Claude 审核新增）
- 设计意图：README 声称 "Hook 自动拦截所有 Agent 的工具调用"——捕获层是后续所有层的前提
- 量化指标：`[HOOK]` 日志中 error/fail 占比
- 数据源：`~/.aipmc/logs/aipmc.log`
- 基线（8/7 复核修正）：日志 229 条 [HOOK] 中 **228 条为 `hook=post-commit`**（git hook），与 Agent 工具调用 hook 无关；Agent hook 仅 1 条 `json_parse_err src=claude`（1.7 埋点后首条）。原"8 条 error（3.5%）"为 title 文本含 "error" 的误匹配（实际 `status=OK`）——**Agent hook 失败率此前无埋点不可测**
- 8/7 补充：复核发现 codex/gemini/opencode 的 hook 错误路径（panic/JSON 解析失败）仅 `AIPM_DEBUG_HOOK` 下输出 stderr，**默认静默**；已改为无条件 stderr + LogShared（退出码保留 0 = fail-open 有意决策，见 decision-20260807-114527-2ceb9a）
- 目标值：< 1%
- 备注：hook 丢数据则 pipeline 再准也无用（Claude 审核建议）

### C. 注入层 — INJECT / 反馈

**C1. INJECT 注入率**
- 设计意图：`3211f1e` P0-2 上下文注入；`652d18a` 埋点
- 量化指标：`injected=Y / (Y+N)`（按请求）；更合理口径：**按唯一 content hash**（`unique_hash_injected / unique_hash_total`），因 same_content 去重是正确行为，不应算注入失败（Claude 审核建议）
- 数据源：`~/.aipmc/logs/aipmc.log`
- 基线：按请求 **5730 / 25267 = 22.7%**；按唯一 hash **当前不可测**——日志仅 389 个唯一 hash 且均出现在 skip 日志，成功注入未记录 hash（观测缺口，需在 0.2 补埋点）
- 目标值：按唯一 hash 注入率 ≥ 80%（"新信息是否被注入"口径）
- 备注：same_content 去重 19194 次 + char_limit 抑制 18513 次为主因

**C2. file-awareness 协议适配** 🔴
- 设计意图：`652d18a` 监控目标 1&3（file awareness 是否找到图数据；各协议 body 解析成功率）
- 量化指标：`file_assoc files=N matches=M` 成功数；`body_parse=err` 失败率；按 agent 拆分的 file= 注入量
- 数据源：日志 `file_assoc` / `agent=... file=`
- 基线：成功 4710 次 / 失败 2104 次（31%）。**8/7 精确归因（修正此前"codex/cursor 完全失效"结论）**：
  - file_assoc 成功：claude 3768 / codex 779 / cursor 235——**codex/cursor 部分请求有效**（归因方法：按 `agent=...` 前缀行推断；成功行 `file_assoc files=N matches=M` 本身不带 agent 字段——8/7 复核确认需注明）
  - body_parse=err：claude 817（17.8%）/ codex 856（52.4%）/ cursor 463（66.3%）——**所有 agent 均有失败**，codex/cursor 失败率显著更高
- 目标值：各协议解析成功率 ≥ 90%
- 根因：`extractFilePaths` 仅单一 JSON 解析路径，失败即放弃（无降级）；且 `extractPaths` 依赖 `os.Getwd()` 前缀——proxy cwd=EncryptDrive 时其他项目绝对路径匹配失败（隐藏缺陷）

**C3. char 预算利用率**
- 设计意图：INJECT 800 chars 上限（`maxInjectChars`）+ `maxActions=3`
- 量化指标：`suppressed=N reason=char_limit` 次数；实际注入 chars 分布；actionItems 注入数 / 事件数
- 数据源：日志 `suppressed=` / `agent=... chars=`
- 基线：suppressed 18513 次；**50 个 emerge 事件只注入 3 个（cap=3）** ❌；Claude 平均 chars=1299（超预算，guidelines/file 有独立预算）
- **2.1 已实现（8/7）**：maxActions=3 硬顶移除，改为预算驱动（maxInjectChars 内按序写入）；事件按可操作性优先级排序（commit_orphan=4 > stale_file/mcp_error=3 > hotspot=2 > 其他=0）；hotspot_untracked/mcp_error 聚合为单行（Claude 审核细化：这两类聚合、orphan/link 不聚合）；单类型上限 5、总条数上限 10；新增单测 TestFormatActionItems*
- **8/7 复测（proxy 重启后实测）**：`emerge_events total=20 types=commit_orphan=12 hotspot=3 mcp_error=1 tentative_link=1 task_created=2 plan_created=1 items=7 perTypeCap=5 ceil=10`——total 44→20（去重+已处理过滤）、items 42→7（优先级+聚合+cap）；注入 `agent=cursor/codex actions=7 chars=679` < 800 预算 ✅
- 目标值：suppressed 占比 < 30%（char_limit 抑制主要转向同类型 cap）；注入动作项覆盖 3+ 事件类型；复测：`rg "emerge_events" ~/.aipmc/logs/aipmc.log | tail` 观察 items 数与原 50→3 对比

**C4. guidelines 注入**
- 设计意图：`93fa9e8` guidelines.md INJECT 机制
- 量化指标：`[GUIDELINES] loaded N chars` 次数；guidelines 是否出现在注入块
- 数据源：日志；`.pmai/guidelines.md`
- 基线：215 次加载，EncryptDrive 1622 chars ✅
- 目标值：项目配置了 guidelines 则 100% 注入

### D. 行为层 — Agent 响应闭环

**D1. MCP 工具自发使用率**
- 设计意图：`decision-20260728-101303`「Agent 自发使用率 5% → 30%+」；`task-20260728-101318-69ed87`
- 量化指标：工具调用次数 / LLM 请求数；区分"自发" vs "人类显式要求"（难点：用上下文判断）
- 数据源：`[MCP]` 日志 vs `[LLM]` 日志；discussion_log
- 基线：get_briefing 87 次（7/28 时 36 次）；read_discussions 630 次；最近 400 条 MCP 中搜索类工具（search_context 69 / read_discussions 55 / search_discussions 49）占主导，**自发使用趋势上升**
- 目标值：自发使用率 ≥ 30%（决策线）；零调用工具（submit_feedback、suggest_threads）至少被使用
- 备注：`aipm_trace_context` 仅 27 次——设计假设"Agent 会主动查图"已失效（7/28 讨论确认），评估时按"图数据须由 pipeline 主动注入"口径

**D2. 事件消费率**
- 设计意图：`7b26cfd` INJECT 注入 emerge events 作为 actionable nudge
- 量化指标：`events.consumed_by_agent=1 / total`
- 数据源：`events`
- 基线：1117/1178 = **94.8%**
- **8/7 细节确认：指标语义失真**——`handleMarkConsumed` 执行全量 `UPDATE ... consumed_by_agent=1`，94.8% 是"已读"而非"已处理"
- **2.3 已修复（8/7）**：新增 `events.processed_by_agent` 列区分「已读/已处理」；自动标记点：commit 绑定 task → 对应 `commit_orphan` 置 processed、task → done → 对应 `task_stale_file` 置 processed；另提供 MCP 工具 `aipm_mark_event_processed(entity_id, event_type?)` 供 Agent 显式标记（hotspot_untracked/mcp_error 等无自动映射的事件）
- 量化指标：`processed_by_agent=1 / total`（已处理率，D2 主指标）；`consumed_by_agent=1 / total` 保留为「已读率」参考
- 目标值：已处理率 ≥ 40%（待 2 周基线采集后校准）

**D3. Agent 行为改变证据**
- 设计意图：项目核心使命"影响 Agent 决策"
- 量化指标：Agent 主动调用修复类工具（record_commit 修复 orphan、update_task_status、link_entities）次数；对话中出现注入内容响应（"已通过 AIPM 梳理完当前项目状态"等）
- 数据源：discussion_log
- 基线：aipm_* 调用 3988 次；record_commit 586 次；对话中 74 次提及简报/待处理/编码规范 ✅
- 目标值：每次评估窗口内 ≥ 1 条可归因于 INJECT 的行为改变证据

**D4. 用户负面反馈趋势**（Claude Challenge 3 新增）
- 设计意图：20 个目标全是系统内部指标，缺用户定性反馈回路；`detectUserFrustration`（`proxy/context_inject.go:544`）已存在但未纳入评估
- 量化指标：`detectUserFrustration` 输出频率趋势（"还是不行""完全没用"等负面信号）
- 数据源：`proxy/context_inject.go` 注入的 frustration 内容 + 日志
- 基线：**未统计**（机制已存在，需在 0.2 补观测）
- 目标值：负面反馈占比不随使用增长而上升

### E. 系统 / 工程层

**E1. 日志观测体系完备性**
- 设计意图：`3211f1e` P2 全链路日志（[MCP][PIPELINE][INJECT][LLM] 四标签）；`652d18a`/`8a533b8` 观测埋点
- 量化指标：tag 分布（INJECT 57071 / PIPELINE 39463 / LLM 25281 / MCP 2776 / EMERGE 671 / RECONCILE 396 / GITSYNC 395 / GUIDELINES 216）；每个埋点是否可回答一个设计问题
- 数据源：`~/.aipmc/logs/aipmc.log`（注意：LogShared 固定写 `~/.aipmc/logs/`，不写项目 `.pmai/logs/`——排查日志时须用全局路径）
- 基线：观测体系可回答：注入率、file 匹配、缓存节省、事件量、pipeline 运行 ✅
- 目标值：新增功能必须自带可量化埋点（评审门槛）
- 备注：8/6 Claude 审查误报"管道日志丢失"——根因是 macOS BSD grep 不支持 `\|` 交替 + 查错路径。观测工具（日志查询方式）需沉淀为文档

**E4. Proxy 协议正确性**（Claude 审核新增）
- 设计意图：README 声称 "Anthropic↔OpenAI↔Gemini 协议翻译"——协议错误会直接污染 Agent 行为
- 量化指标：`[LLM]` 日志中错误响应占比（`status=ERR` 或等价字段）
- 数据源：`~/.aipmc/logs/aipmc.log`
- 基线：**当前不可测**——[LLM] 日志无错误字段（25326 条中 status=ERR 为 0 是"未记录"而非"无错误"），需补埋点
- 目标值：错误率 < 1%（先补日志再定基线）

**E2. 稳定性（并发 / 版本）**
- 设计意图：`f61e845` 多 agent 并发写锁；SQLITE_BUSY 重试
- 量化指标：日志中 error/BUSY 次数；运行进程版本一致性
- 数据源：日志；进程列表
- 基线：**8/7 修正——当前 6 个 aipmc 进程全部为 `dist/aipmc`（8/6 17:34 新版），"新旧进程并存"已不存在**（cooldown 3903 次为 17:34 前旧进程的历史残留）。**但 6 进程多实例冗余是新异常**：1263(aipmc web:8720)、1712(EncryptDrive web:8011)、4732(proxy:19530)、2727/3339/5169（无监听，疑似失败的 serve/agent 实例）；且 projects.json 中两项目 proxy_port 均为 19530
- 目标值：每项目至多 2 进程（serve+agent）；无幽灵进程；BUSY 错误 0
- 备注：启动自检（0.3）应同时防端口冲突与重复实例

**E3. 成本效率**
- 设计意图：L2 缓存、INJECT 去重的核心收益
- 量化指标：LLM 调用节省数（L2 cached）；token 使用（cache_hit 比例）；INJECT 重复注入避免数
- 数据源：日志 `[LLM] cache_hit=`、`L2 summary cached=`
- 基线：L2 缓存 100% 命中；INJECT same_content 去重 19194 次（避免重复注入）；Claude LLM cache_hit 大量（99712/101728）
- 目标值：缓存命中率持续高位；无显著 token 浪费

## 3. 已知问题 → 目标映射

| 问题 | 目标 | 严重度 | 状态 |
|------|------|:--:|------|
| `CreateBug` 缺 `SyncFTS5Entity`（plan/bug 搜索为 0） | B 组搜索覆盖 | 🔴 | 待修（不急） |
| `dupEvent` 只查 unconsumed → commit_orphan 重复率 480% | B6 | 🔴 | 待修 |
| `extractFilePaths` 对 codex/cursor 解析失败 | C2 | 🔴 | 待修 |
| char_limit + cap=3 丢弃 47/50 事件 | C3 | 🟡 | 待修 |
| 新旧进程并存 → 评估失真 | E2 | 🟡 | 重启到最新版 |
| L2 缓存复用历史脏摘要（8 条嵌套 goal 残留） | B2/B3 | 🟡 | 已修（8/7 写入归一化 + 存量清理归零） |
| summary 写入路径未 unnest（8/7 复核新发现 4 条） | B2 | 🟡 | 已修（8/7 `NormalizeSummaryGoal` + 存量清理） |
| `ConvertIdeaToTask`/`ConvertIdeaToDecision` 裸断言 6 处（panic 风险） | — | 🔴 | 已修（8/7 加 ok 断言 + 空值错误返回） |
| codex/gemini/opencode hook 错误路径默认静默 | B8 | 🟡 | 已修（8/7 无条件 stderr + LogShared） |
| StoreGitCommit 预检 LIKE 匹配空 hash 行 → hook 记录静默丢失（13:38 后 6 个 commit 被吞） | B8/数据完整性 | 🔴 | 已修（8/7 排除空 hash + 回归测试，bug-20260807-144853-f2c39e） |
| gitsync Chdir 无错误检查 | — | 🟡 | P3 待办（可能操作错误目录） |
| extractSessionText 预算溢出（S 级消息无预算控制） | B1 | 🟡 | P3 待办 |
| 多 pipeline 并发 CWD 竞争（进程级全局状态） | — | 🟡 | P3 待办 |
| L2 覆盖率 55% < 85% 验收线 | B1 | 🟡 | 需排查根因 |
| graph_edges 覆盖率 26.7% | B5 | 🟡 | 需确认设计意图 |
| trace_context 低频（27 次） | D1 | 🟢 | 设计假设已变，按 pipeline 注入口径 |

## 4. 评估执行建议

1. **先固化基线**：本清单即基线 v1（2026-08-07）。后续每次评估更新基线。
2. **优先补齐缺口**：B4 已完成（reconcile 基线已验证）；E1（日志查询指南）是剩余前置项。
3. **修复不急但需排期**：B6、C2、FTS5 三个 🔴 问题影响数据质量，建议在下一迭代修复并复测对应目标。
4. **新增功能门槛**：任何功能合入前必须定义其可量化目标（本清单编号），否则视为未完成。
