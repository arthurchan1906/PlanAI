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
| F 效用验证层 | 工具值不值得存在 | 6 | 机制在运转 ≠ 工具有效（8/13 三方收敛新增） |

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
- **8/7 实现双口径**：`inject_rate`（实际注入请求占比，24.5%——含 same_content 去重，参考）＋ `inject_coverage`（有数据可注时的覆盖 = 注入 + 去重 / 排除 no_summary，96.7% ✅）。coverage 实现「新信息是否被注入」意图的近似（same_content 去重算覆盖不算失败）
- 目标值：inject_coverage ≥ 80%（"新信息是否被注入"口径）；inject_rate 降为参考
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

**E5. MCP 工具可靠性**（8/7 审计新增）
- 设计意图：aipm 34 个 MCP 工具是 PM 系统唯一入口；工具失败 → Agent 学会绕过（cursor getInt bug → 69 次 SQL 绕过，见 DATA-AUDIT 3.3）
- 量化指标：调用总量、成功率（`[MCP] status=ERR` 占比）、工具分布、读写比；`src=` 按 agent 拆分（serve 重启后生效，旧行归 unknown）
- 数据源：`~/.aipmc/logs/aipmc.log` 的 `[MCP]`（结构化 `tool=/status=/src=`，双源方案见 DATA-AUDIT 3.5）
- 基线（8/7）：~2,927 行，成功率待首跑确认（含历史契约错误 record_commits/record_bug）
- **8/7 复测修正：成功率 94.6%（160/2973 ERR）——旧值 95.9% 是虚高**。根因：`parseKVFields` 后覆盖前，`aipm_update_task_status` 参数回显 `status=done` 覆盖真实 `status=ERR`（89 行双 status，39 条 ERR 漏计）。已改首见优先（P2 批次）。ERR 分布：aipmc_vision 62 / update_task_status 39 / record_commit 23——vision 错误率需单独排查
- **8/7 vision 修复**：ERR 主因是本地量化 VL 模型（Qwen3.5-4B）间歇性 `empty_response`（实测同批图片 ERR→重试 OK）；`vision.go` 已加空响应自动重试 2 次（82c3e61，需重启 serve 生效）。`image_read`（文件缺失）不重试
- 目标值：成功率 ≥ 95%（基线确认后可调）；契约错误（工具描述/输入校验）应趋零
- 备注（8/10 修正）：**「codex 恒 0」已过时**——实测 codex 路径 `cache_hit=` 8,328 行中 8,290 行非 0（8/10 复核），E3 数据已含 codex 贡献（codex 占 in_tok ~21%）。8/7 观测的恒 0 应为当时上游/字段兼容问题，非协议性缺失；metrics 双字段（cache_hit/n_hit）兼容保留

**E2. 稳定性（并发 / 版本）**
- 设计意图：`f61e845` 多 agent 并发写锁；SQLITE_BUSY 重试
- 量化指标：日志中 error/BUSY 次数；运行进程版本一致性
- 数据源：日志；进程列表
- 基线：**8/7 修正——当前 6 个 aipmc 进程全部为 `dist/aipmc`（8/6 17:34 新版），"新旧进程并存"已不存在**（cooldown 3903 次为 17:34 前旧进程的历史残留）。**但 6 进程多实例冗余是新异常**：1263(aipmc web:8720)、1712(EncryptDrive web:8011)、4732(proxy:19530)、2727/3339/5169（无监听，疑似失败的 serve/agent 实例）；且 projects.json 中两项目 proxy_port 均为 19530
- 目标值：每项目至多 2 进程（serve+agent）；无幽灵进程；BUSY 错误 0
- 备注：启动自检（0.3）应同时防端口冲突与重复实例

**E3. 成本效率**
- 设计意图：L2 缓存、INJECT 去重的核心收益
- 量化指标：`cache_hit/in_tok`（命中 token 占输入比例，与 proxy inspect 前端 `tk()` 口径一致）；cache_hit 绝对量；LLM 调用节省数（L2 cached）；INJECT 重复注入避免数
- 数据源：日志 `[LLM] cache_hit=`（anthropic 路径）/ `n_hit=`（responses 路径，metrics 双字段兼容 8/7 修复）；`L2 summary cached=`
- 基线：L2 缓存 100% 命中；INJECT same_content 去重 19194 次（避免重复注入）；Claude LLM cache_hit 大量（99712/101728）
- **8/7 复测：cache_hit_rate 92.1%（43.1 亿 / 46.8 亿 tok）**——92% 输入命中上游 prefix cache，成本节省显著（DeepSeek 缓存读单价约为 miss 的 1/10）
- 目标值：cache_hit_rate ≥ 90%；cache_create 上游（DeepSeek responses）不返回恒 0——`hit/(hit+create)` 恒 100% 无信号，故用 `hit/in_tok` 口径；**codex 的 cache_hit 数据 8/10 已确认可解析**（见 E5 备注修正），按 agent 拆分观测 codex injected=Y 命中率 36.9%（POC-1 基线）

## 2.5 F 效用验证层（8/13 三方收敛新增——Claude/codex 8/12-8/13 三轮对质产物）

> **F 层定位**：A-E 层验证「机制是否在运转」（工具健康），F 层验证「工具是否值得存在」（工具效用）。机制健康 ≠ 工具效用——注入率 96.7% 只证明「送达机制好」，不证明「送达内容对 Agent 决策有用」。任何工具改进若只有机制指标而无效用证据，即属盲改（用户 8/13 判断）。故 F 层独立于 A-E（机制指标），不混编。
>
> **方法论四支柱**（与 v1.14「先建仪器」同构）：
> 1. **锚点清单**——三类锚点，每个锚点带三要素「方向 + 窗口 + 基线」：
>    - 预防型（边界违规、重复 task、done-gate 拒绝）→ 工具生效时**下降**
>    - 检测型（接力断链、孤儿 commit、事件可见性）→ 工具生效时**先上升**（开始能看见了），行为改变后才下降；无上线前基线则「先升」区分不了「检测生效」和「行为回归」（codex 8/13 修正）
>    - 人时型（协调动作/天、转达循环时长、接力打标）→ 工具生效时**下降**（注意：指标方向可能依赖系统阶段，见 F6 阶段条件——L1 落地前「负载高」才是正向证据）
> 2. **自然对照臂**——不故意降级（ED 是真实工作流）：`skip reason=same_content` 两臂（去重跳过 vs 首次注入，测「重复 vs 新信息」效用差）、跨项目差异部署（ED 开/他项目关）。char_limit 抑制是「全量 vs 去尾」不是「有 vs 无注入」，不构成对照臂（codex 8/13 自我挑战）
> 3. **行为证据链**——注入内容 hash → Agent 引用 → 后续动作，统计转化率。注意：证据密度天然低，季度级指标，非周级
> 4. **用户侧接力打标**——用户转达当下标记「这条转达依赖 AIPM 上下文吗」，即时反事实测量（回忆偏置最小，成本为零——用户本来就在转达）；形态：极简 CLI/MCP 打标工具，**≤1 按键量级**（schema 最小化：`relay_pair, depends_on_aipm, ts`，不做任何额外输入，否则用户放弃——codex 8/13 约束）

**F1. 事件→动作漏斗（D2 三口径拆分）**（EPIC「可见性漏斗」W2）
- 设计意图：D2 单口径 10.8% 把「事件噪音」和「效用低」混为一谈；三口径拆分后才能诊断
- 量化指标：`设计免处理`（tentative_link/task_created——生成即完成使命）/ `可行动`（commit_orphan/mcp_error/hotspot_untracked）/ `已处理` 三列；**关键诊断问题：各事件类型的处理分布是否均衡**
- 数据源：`events.processed_by_agent`；**事件管线是概率性的**（agent 可能不调 get_briefing/inbox，调了也可能不看）——需测 **events → surfaced → processed 三级**：get_briefing/inbox 返回时记录 included 的 event ids（W2 代码改动点），surfaced-but-ignored 才是诊断信号
- 基线（8/13 实测）：免处理 78（tentative_link 40 + task_created 38）/ 可行动 95（commit_orphan 66 + mcp_error 16 + hotspot 13）/ 已处理 19（20%），**且 19 个全部是 commit_orphan——mcp_error、hotspot 零响应**
- 目标：可行动事件已处理率提升；处理分布不再集中于单一类型
- 方向：**行为型 ↑**（处理率上升且分布均衡，无「后降」阶段——先升后降只适用于「检测到的问题数」如 F5，不适用于处理率；codex 8/13 修正）| 窗口：每周 | 基线：8/13 上述数字

**F2. 注入交付漏斗（C3 精细化）**（EPIC「可见性漏斗」W1）
- 设计意图：「被抑制的是不是高价值内容」现在**无法回答**——suppressed 行无 ids/priority/source_segment
- 量化指标：按 `source_segment`（fileAssoc/warnings/actionItems/goals）拆分的被裁率；suppressed 行的 ids + priority 分布
- 数据源：`~/.aipmc/logs/aipmc.log` 的 `suppressed=`。**历史不可恢复**：97% 旧行（8/12 15:58 前）是 time-only 前缀**无日期字段**，`--since` 只对带日期的新行生效——基线待 instrumentation 后**从零采**，不可回算（codex 8/13 修正）
- 基线：待 instrumentation 后从零采集；语义注意——suppressed 是**尾部低优先级裁剪的部分注入，不是失败**；分母混入 cooldown/same_content 设计性 skip，口径须收紧
- 目标：先跑一周采集，再定目标与是否修（不拍脑袋设 <30%）
- 方向：检测型 | 窗口：一周 | 基线：待采（从零）

**F3. done-gate 拒绝分布（E9 升级为锚点）**
- 设计意图：done-gate 拒绝是「任务为何无法闭环」的无歧义计数，现成、可自动统计
- 量化指标：`[DONE-GATE] pass/reject` 与拒绝原因分布（`no_verified_commit` 等）
- 数据源：`[DONE-GATE]` 日志
- 基线（8/13）：pass=52 reject=12——**reject 埋点 8/7 才上线，历史 pass=20/reject=0 是「仅 pass 埋点」的测量假象（E9 备注）**；启动期 reject 0→12 正是「开始能看见了」；拒绝原因分布需补采
- 目标：稳态 reject 低位参考；原因分布用于诊断（工具没促成提交 vs 任务本来没做完——须拆分后再下结论）
- 方向：**检测型——启动期先升后降，稳态低位**（0→12 是检测生效而非行为回归，与 F5 同构；codex 8/13 修正）| 窗口：每周 | 基线：补采上线前 + 启动期数据

**F4. rel_path 覆盖率验收（T3b+T4 闭环验收锚点）**（EPIC W3 配套）
- 设计意图：`7d50ebd`/`ed3fcf9`/`e334131` 落地后，rel_path 数据地基的覆盖率验收
- 量化指标：`H2 rel_path_coverage(filetools)` claude≥90% 锚点 / `(bash)` 参考（决策 19）
- **验收命令（8/13 审核补充）**：必须显式 `aipmc metrics --since 2026-08-13T09:20:00`（rel_path 部署时刻）——默认 8/7 窗口含部署前存量（无 rel_path），会虚假稀释（全表口径 22.2% vs 部署后 100%）
- 数据源：metrics H2（**当前阻塞：全表统计被修复前 2851 条存量稀释成 0.5%/58.8% 假象；DB 类指标无 `--since` 窗口——EPIC W3 待修**）
- 基线（8/13 部署后实测）：claude filetools **10/10 = 100%** ✅（缺 rel_path 7 条全是部署前）；codex filetools 分母**结构性为 0**（codex CLI 文件操作全是 exec_command/Bash，无独立 apply_patch/Write/Read tool_name——真实信号在 bash 桶，157 条 Bash/12 条带 rel_path）
- 目标：`--since` 窗口落地后按部署后数据正式验收
- 锚点关系：检测型数据地基——接力断链锚点（F5）的前置

**F5. 接力断链频率**（检测型锚点占位，待 T4 数据）
- 设计意图：v1.14 共识主风险是「隔小时接力断链」而非并发碰撞；断链事件是 AIPM 声称要防的事故
- 量化指标：B 碰了 A 最后改过的文件、且变更无因果链的事件数/周
- **判定规则待操作化（codex 8/13 强制前置）**：「无因果链」目前无法判定（讨论链接？提交重叠？时间窗口？）——基线采集前必须定死判定规则（对齐 v1.14「git 状态客观判定 + 无链接作信息性提示」），否则「先升」区分不了规则漂移和真检测
- 数据源：rel_path + commits（T4 数据积累中）
- 基线：**当前数字 0 是测量假象**（无实时追踪器）——上线前基线待 T4 数据落地 + 判定规则操作化后采
- 目标：先采基线，不设目标值（数据裁决）
- 方向：检测型（上线后先升后降——不标注方向会在第一个月误判工具无效）| 窗口：周级

**F6. 用户侧接力打标**（人时锚点，形态占位）
- 设计意图：锚点和行为链只能测 Agent 侧，但 AIPM 的核心消费方是转达中枢（人）——人的判断对 metrics 不可见
- 量化指标：转达消息中「依赖 AIPM 上下文」打标比例
- 数据源：打标工具（待建，占位——极简 CLI/MCP，转达当下标 y/n，≤1 按键）
- 基线：待工具上线
- 目标：**阶段条件（codex 8/13 修正——方向依赖系统阶段，不写阶段会得出「工具越有用越没用」）**：
  - L1/编排台落地**前**：AIPM 是唯一转达管道，打标「依赖 AIPM」比例**高 = 工具在承重（正向证据）**
  - L1/编排台落地**后**：agents 能直接互感知，依赖转达的比例**下降 = 正向证据**（转达被自动化替代）
  - 当前阶段按「高负载为正」观测，切换阶段时锚点方向翻转
- 方向：人时型（阶段依赖）| 窗口：每周汇总

## 3. 已知问题 → 目标映射

| 问题 | 目标 | 严重度 | 状态 |
|------|------|:--:|------|
| `CreateBug` 缺 `SyncFTS5Entity`（plan/bug 搜索为 0） | B 组搜索覆盖 | 🔴 | 待修（不急） |
| `dupEvent` 只查 unconsumed → commit_orphan 重复率 480% | B6 | 🔴 | 待修 |
| `extractFilePaths` 缺 OpenAI Responses `input` 解析（codex 静默 0 文件）；`body_parse=err` 被未知路径请求污染（detectAgent default 误标 cursor） | C2 | 🔴 | 已修（8/7 `input` 数组解析 + err 口径收紧，493bbdf；回归测试 3 例 PASS） |
| char_limit + cap=3 丢弃 47/50 事件 | C3 | 🟡 | 待修 |
| 新旧进程并存 → 评估失真 | E2 | 🟡 | 重启到最新版 |
| L2 缓存复用历史脏摘要（8 条嵌套 goal 残留） | B2/B3 | 🟡 | 已修（8/7 写入归一化 + 存量清理归零） |
| summary 写入路径未 unnest（8/7 复核新发现 4 条） | B2 | 🟡 | 已修（8/7 `NormalizeSummaryGoal` + 存量清理） |
| `ConvertIdeaToTask`/`ConvertIdeaToDecision` 裸断言 6 处（panic 风险） | — | 🔴 | 已修（8/7 加 ok 断言 + 空值错误返回） |
| codex/gemini/opencode hook 错误路径默认静默 | B8 | 🟡 | 已修（8/7 无条件 stderr + LogShared） |
| StoreGitCommit 预检 LIKE 匹配空 hash 行 → hook 记录静默丢失（13:38 后 6 个 commit 被吞） | B8/数据完整性 | 🔴 | 已修（8/7 排除空 hash + 回归测试，bug-20260807-144853-f2c39e） |
| done-gate 空 hash commit 放行 task done（不可溯源记录通过验收） | 数据完整性 | 🔴 | 已修（8/7 `countVerifiedCommits` 要求 hash 非空，e75b726） |
| `[MCP]` 日志无 agent 字段 → 无法按 agent 拆 MCP 指标 | E 观测 | 🟡 | 已修（8/7 补 `src=`/`name=`，取自 initialize clientInfo，e75b726） |
| hook 抢跑后 `record_commit` 去重直接返回 → 孤儿 commit 永远无法绑定 task（三件套首跑窗口内 50% orphan） | P0 commit 三件套 | 🔴 | 已修（8/7 `BackfillCommitTask` 幂等回填 + 去重回填语义，e007ee4；窗口内 orphan 12→0） |
| `record_commit` 去重精确匹配短 hash → hook 完整 hash 行不命中，产生重复行 | P0 commit 三件套 | 🔴 | 已修（8/7 双向前缀匹配，5356486；实测触发后合并数据） |
| `aipm_record_commits` 批量路径无按 hash 去重 → hook 抢跑后批量记录产生重复行（8/7 实测 3 组实时重复） | P0 commit 三件套 | 🔴 | 已修（8/7 `CreateCommit` 去重兜底 + `BatchCreateCommits` 逐条双向前缀去重合并；存量 3 组已并） |
| hash_uniqueness 原 distinct 口径把多 task 同 hash（合法 relates_to）标红 → 误报 | P0 commit 三件套 | 🟡 | 已修（8/7 细化口径：只统计同 task/含空 task 重复组，多 task 单列不告警） |
| gitsync Chdir 无错误检查 | — | 🟡 | P3 待办（可能操作错误目录） |
| extractSessionText 预算溢出（S 级消息无预算控制） | B1 | 🟡 | P3 待办 |
| 多 pipeline 并发 CWD 竞争（进程级全局状态） | — | 🟡 | P3 待办 |
| L2 覆盖率 55% < 85% 验收线 | B1 | 🟡 | 需排查根因 |
| graph_edges 覆盖率 26.7% | B5 | 🟡 | 需确认设计意图 |
| trace_context 低频（27 次） | D1 | 🟢 | 设计假设已变，按 pipeline 注入口径 |
| metrics DB 类无 `--since` 窗口 → H2 全表被存量稀释（0.5%/58.8% 假象），90% 锚点物理不可达 | F4 | 🔴 | 待修（EPIC「可见性漏斗」W3） |
| suppressed 行无 ids/priority/source_segment →「被裁的是不是高价值内容」不可答（97% 行来自旧时期） | F2 | 🟡 | 待修（EPIC W1） |
| D2 单口径 10.8% 混淆「事件噪音」与「效用低」；mcp_error/hotspot 零响应原因未知 | F1 | 🟡 | 待修（EPIC W2 三口径拆分） |
| codex filetools 分母结构性为 0（无独立 apply_patch/Write/Read tool_name） | F4 | 🟡 | 口径调整：codex 参考 bash 桶 |
| 接力断链无上线前基线（当前 0 是测量假象） | F5 | 🟡 | 待 T4 rel_path 数据落地后采 |
| 接力打标工具未建（用户侧反事实测量缺口） | F6 | 🟢 | 占位，待设计 |

## 4. 评估执行建议

1. **先固化基线**：本清单即基线 v1（2026-08-07）。后续每次评估更新基线。
2. **优先补齐缺口**：B4 已完成（reconcile 基线已验证）；E1（日志查询指南）是剩余前置项。
3. **修复不急但需排期**：B6、C2、FTS5 三个 🔴 问题影响数据质量，建议在下一迭代修复并复测对应目标。
4. **新增功能门槛**：任何功能合入前必须定义其可量化目标（本清单编号），否则视为未完成。
5. **F 层纪律一（8/13）**：任何工具改进 = 一次前后对比——上线时记录前后锚点值（零成本自我对照，E3 复测/items 42→7 即先例），不测不改进。
6. **F 层纪律二（8/13）**：每个效用指标必须带三要素「方向 + 窗口 + 基线」才能合入（检测型锚点不标注方向会在第一个月误判工具无效——H-A5「碰撞低频」陷阱的镜像）。
7. **F 层执行路径（8/13 收敛）**：先修仪器（EPIC「可见性漏斗」W1 suppressed ids/priority/source_segment + W2 D2 三口径 + W3 metrics `--since`）→ 固化锚点（F1-F6）→ 行为证据链 → 自然对照（same_content 臂、跨项目差异部署）。对照实验最后，不故意降级。

**E6. workflow_score 工作流规范性**（8/7 审计新增）
- 设计意图：`session_summaries.quality_score` 是启发式规则分（100 起扣：无 workflow baseline -30、未 completed -25、MCP 工具缺失 -10/个、hook 覆盖不完整 -15、SQL 直查 -20），**不是 AI 质量评估**——反映 Agent 是否走标准工作流（MCP 工具 + hook + 任务闭环）
- 量化指标：有分 session（`quality_score>0`）均值，覆盖率标注分母；按 agent 拆分
- 数据源：`session_summaries.quality_score`
- 基线（8/7）：48.9（覆盖 89/91）；按 agent：claude-code 65.3 / codex-cli 43.7 / gemini-cli 38.6 / cursor 25.0 / opencode 22.9
- 目标值：≥ 60（依据 claude-code 基线 65；低于 60 说明工作流规范性不足——MCP 绕过/hook 缺失/SQL 直查）
- 备注：8/7 由 quality_score 改名，消除「AI 质量」误导；cursor/opencode 低分主因是 getInt bug 期间 SQL 绕过（已修复），重评估后应回升

**E7. task_completion_rate 任务闭环率**（8/7 审计新增）
- 设计意图：plan→task→commit→done 闭环完成度；done-gate 保证 done 有 approved/auto + passed/auto + 真实 hash 的 commit 支撑
- 量化指标：`tasks.status='done' / 活跃任务（done+todo+in_progress+blocked+paused，不含 deleted）`
- 数据源：`tasks`
- 基线（8/7）：47/59 = 79.7%
- 目标值：> 80%（当前差 0.3pp；P0 采集修复（e007ee4/5356486）落地后应自然达标）
- 备注：deleted 不计入分母（归档语义）

**E8. PIPELINE 健康度**（8/7 P2 批次新增）
- 设计意图：pipeline 是 PM 系统后台心跳——它挂了事件不再产生、reconcile 不再运行、L2 摘要停更
- 量化指标：L3 session 处理量（运行频率参考）、reconcile 成功率 `done/(done+error)`、review error 计数
- 数据源：`[PIPELINE]` 日志（`L3 session=` / `reconcile done|error` / `review error`）
- 基线（8/7）：L3=2,483；reconcile 575 done / 5 error = 99.1%；review error 48 次（SQL UNIQUE 约束冲突）
- **8/7 复核：review error 49 次中 45 次是 SQLITE_BUSY（多 agent 并发写），仅 4 次 UNIQUE（昨天历史，CreateLink 已 INSERT OR IGNORE）**。已修：runOnce 层 retryPipelineBusy 指数退避重试 Run/Reconcile（a3a1453，需重启生效）
- 目标值：reconcile 成功率 ≥ 98%；review error 计数趋零（BUSY 重试后应大幅下降；约束冲突已在代码层去重）

**E9. done-gate 通过/拒绝分布**（8/7 P2 批次新增）
- 设计意图：done-gate 是 task 完成的最后防线；此前只记录 pass，拒绝原因全黑
- 量化指标：`[DONE-GATE] pass/reject` 计数与拒绝原因分布
- 数据源：`[DONE-GATE]` 日志（reject 埋点 8/7 补：`reject task=... reason=no_verified_commit`）
- 基线（8/7）：pass=20 / reject=0（历史仅 pass 埋点；reject 埋点上线后才有拒绝数据）
- 目标值：reject=0（参考）；若 reject>0，原因分布用于定位「任务为何无法闭环」（无 commit/未 approve/空 hash）

**P0. commit 三件套：采集管道完整性**（8/7 审计新增）
- 设计意图：任一标红 = 采集管道异常（任务关联 / 来源可追踪 / 去重正确性）。三件套互为补充：orphan 看关联、hash 非空率看来源、去重率看「同一 commit 是否被重复记录」
- 量化指标与语义：
  - `orphan_rate`：`task_id` 空占比，目标 <10%
  - `hash_traceability`：`commit_hash` 非空率，目标 >90%（空 hash 不可溯源）
  - `hash_uniqueness`：**只统计「采集 bug 重复」行**——同 task 重复行 + 含空 task 的重复组行，目标 =0。多 task 同 hash（同一物理 commit 被多个 task 引用，`relates_to` 多对多）是合法语义，单列 `hash 多task引用: N 组` 不告警（8/7 细化：原 distinct 口径会把合法多对多也标红，狼来了）
- 数据源：`commits` 表（窗口参数 `--since`，默认 8/7 14:00 起只看修复后数据；`--since all` 看全表）
- 基线（8/7 ED 全表清理后）：orphan 5.6% ✅ / hash_traceability 96.8% ✅ / hash_uniqueness 0.0% ✅ / 多 task 引用 132 组（合法）
- **8/7 ED 存量清理记录**：547 空 hash → 精确/高置信回填 518（94.7%），29 行为 Session/重写记录无 git 对应保留；短 hash 归一化 102 行；同 task 真重复合并 12 组删 16 行 + 空 task 重复组删 29 行 + 实时重复 3 组（引用全部迁移，审计写入 `audit_log`）；hash_traceability 62.4% → 96.8%
- **8/7 aipmc 自身库清理记录**：36 空 hash（97993d9 前 MCP 未传 hash 的旧行）→ 标题精确回填 24（66.7%），12 行为代理拟稿标题与 git subject 不符，归档不强行回填；回填暴露同款「hook+MCP 双记」重复 → 合并 20 组删 21 行（引用迁移 31）；hash_traceability 82.3% → 93.4%，uniqueness 0.0%。历史孤儿 79 个（GITSYNC/hook 未绑定）属「孤儿绑定」P0 并行工作，需人工判断任务归属
