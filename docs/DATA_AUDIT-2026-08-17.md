# 数据审计：三份设计文档声明 vs 实测（2026-08-17）

> 目的：对 `docs/HARNESS_ROADMAP.md`、`docs/MEASUREMENT_LOOP.md`、`docs/EVAL_PIPELINE.md`
> 中的生产数据声明做严格核对，固化可复现基线，并标记文档中已过时/需修正的绝对值。
> 数据源：`.pmai/data/pmai.db`（本机 2026-08-17 09:25 快照）+ `~/.aipmc/logs/aipmc.log`
> （当前）+ `~/.aipmc/logs/aipmc.log.20260814_155529`（8/14 全天归档）。

---

## 1. 结论摘要

1. **文档中的绝对值全部来自 8/15 快照（Windows 机器 `D:\projects\AIPMC`），在本机库不可复现**。
   总量、agent 分布、近 30 天占比、摘要覆盖率、自报 commit 数、事件未消费数全部对不上。
2. **两个"严重度"结论已反转**：
   - "注入回路空转、no_summary_data 92%" → 当前 no_summary 仅 5.3%，注入率 45.4%（8/14 曾达 48.3%）。
   - "94% 是历史数据" → 近 30 天占 56.6%。
3. **最大操作风险（文档外发现）**：运行中的二进制是 `7a5db22`（8/14 17:11），
   L2 401 修复 `a66d6e5`（8/14 17:50）**未部署**；日志 BOOT 行（8/17 08:59）version=7a5db22 实锤。
4. **方案结构性判断依然成立**：M2/M3 依赖的 `file_op` 信号仅 33 条 / 13 sessions，
   `rel_path` 精确口径 677 条（2.8%）——"先修 hook 产出（数据可得性），再谈归因指标"不变。

## 2. discussion_log 声明 vs 实测

| 声明（8/15，MEASUREMENT_LOOP §7） | 实测（8/17） | 判定 |
|---|---|---|
| 总量 11,529 条 | 24,279 条 | ✗ 翻倍 |
| claude-code 5,859 / opencode 2,757 / codex-cli 1,430 / cursor 1,326 / gemini 152 | 11,737 / 1,547 / 9,433 / 1,363 / 155（另 mcp 4 / other 33） | ✗ codex +6.6x，opencode 反减 |
| 近 30 天 692 条（6.0%，"94% 历史"） | 13,735 条（56.6%） | ✗ 反转 |
| rel_path 新格式 32 条（0.2%） | 宽松 LIKE 1,001（4.1%）；**精确 JSON 键 `"rel_path"` 677（2.8%）** | ✗ 涨 ~21-31x |
| file_path 旧格式 1,439（"仍持续产出"） | 2,358（近 30 天 1,141 仍在产出） | ✓ |
| file_op 21 条 | 33 条，仅 13 个 session | ✓ 极稀少 |

口径注：宽松 LIKE 会把工具输出文本里的字面量计入（如 1,001 vs 677），归因/覆盖率指标一律用
精确 JSON 键解析。

## 3. 理解层 / 产出层 / 事件声明 vs 实测

| 声明（8/15） | 实测（8/17） | 判定 |
|---|---|---|
| session_summaries 42 行 / 5 有摘要（12%） | 115 行 / 56 有摘要（48.7%） | ✗ |
| commits 自报 passed/approved 12 行 | 80 行（另 auto/auto 121、auto/approved 19、not_run/pending 19） | ✗ |
| 系统验证 0 行 | commits 表无 `verify_status` 列（L-O 需先 schema 迁移） | ✗ 无列可查 |
| commit_orphan 41 条全部未消费 | 79 已消费 + 24 未消费（消费机制已生效） | ✗ M4 基线需重定 |

## 4. 注入回路日志基线（§7.3）声明 vs 实测

| 声明（8/15 窗口） | 8/14 归档全天 | 当前日志 | 判定 |
|---|---|---|---|
| no_summary_data 1,883（92.0%） | 996（≈1.5%） | 141（5.3%） | ✗ 已非主因 |
| cooldown 59 | 3,903 | 0（机制已移除） | ✗ M2 对照组该层无数据 |
| injected=Y 6（0.4%） | 10,308（30.4%） | 592（45.4%） | ✗ 注入率大幅回升 |
| same_content 105 | 28,910 | 772 | ✗ |
| —（文档未列） | char_limit 33,881（≈50%） | 613 | 实际最大抑制源 |

当前日志 skip reason 主流仅三种：char_limit / same_content / no_summary_data
（system_fault 等零星存在，属归档旧机制）。

## 5. EVAL_PIPELINE 数据前提推演

| 前提 | 实测 | 影响 |
|---|---|---|
| 单 session 1,194 行 / 74 user 消息 / 跨 2 天 | 确有 session `24d8f040`：1,194 行 / 74 user / 3 天；现有更大：`c5afe53d` 1,329 行 / 63 user；`fc191b4c` 1,243 行 / 185 user | 成立且更极端 |
| user 消息 ~15-20/大 session | 全库 user 消息 2,183（9.0%）；>100 行 session 56 个，其中 ≥50 user 的 15 个、≥20 的 34 个 | "15-20 意图/大 session"只对超大 session 成立；**全库意图段预计数百个** |
| 标注集 15-20 段 | 按意图类型分层后每类仅 3-5 例 | **建议上调 ≥40 段**，或先无监督回合化聚类再抽样 |
| 近 30 天全量成本 | 13,735 行（文档假设 692 行的 ~20 倍） | 成本模型需重估，建议加 `--limit` |
| 四格式并存 | post_tool 18,323 / type= 9,436 / hook_event_name 9,054 / conversation_id 1,388 / file_op 33 | 成立 |

## 6. 文档外新增事实（影响实现）

1. **日志有 NEL（U+0085）行终止符 + 无日期旧格式**，macOS `grep` 会当二进制失明——
   EVAL_PIPELINE 的"乱码容忍"只提 GBK，`parse.go` 必须补 NEL/无日期/二进制检测
   （与 8/12 已修的行日期、LC_ALL=C/grep -a 同源，见 `docs/LOG_GUIDE.md`）。
2. **`[LLM]` 日志行 session 覆盖（8/18 复核结论）**：`567b332` 已给 7 个日志点补 `session=`，
   但 **codex 路径生效（当前实例 100% 带 session，8/18 10:41 后空 session=0）、claude 路径为
   结构性缺口**——`extractSessionID` 只认 `client_metadata.session_id`/顶层 `session_id`，
   Claude Code 的 anthropic 直通请求体（`/v1/messages`）协议本身不带这两个字段，24h 内 197/197
   行 `session=` 全空（8/18 实测）。影响：codex 漏录率可按 session join；claude 只能做
   agent×小时粗粒度对齐（8/18 实测 9/9 小时双向一致、0 脱链信号，捕获层健康），精确对账
   需 serve 侧方案或接受协议限制。
3. **cooldown 机制已消失**：M2 对照组"按 reason 分层"需按现况重定义
   （char_limit / same_content / no_summary_data / text_too_short / system_fault）。
4. **8/14 单日 INJECT 行 98,617**（归档日志），注入频率不低——M1/M2 只缺 L2 摘要恢复后的样本，
   不缺频率。
5. **`inject_log` 表不存在、SCHEMA_VERSION=2**（文档要求升 3）——HARNESS S2 尚未开工。

## 7. 建议的基线动作（按序）

1. 部署当前源码（含 `a66d6e5` + 本审计），确认 L2 恢复产出，再固化 M0 基线（否则基线是"L2 已死"快照）。
   **8/18 状态：L2 已恢复**——当前日志 no_summary_data 占注入回路 6.9%（8/17 审计 5.3%），主抑制源
   为 char_limit/same_content（健康形态）。
2. 新增 M0 基线采集命令（无 LLM、纯 SQL/日志对账），输出 `eval/baseline.json`。
   **8/18 状态：已落地**——`aipmc metrics --baseline`（提交 57b2fb2 + 粗对账增强），
   24h 实测：codex 漏录 0/2、脱链 0/2（session join，双向一致）；claude 197 行 session 全空
   不可按 session 对账，降级为小时粗对账 9/9 双向一致（详见 §6.2）。
