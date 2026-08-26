# P1c 标注集（草案，待人工确认）

> 生成：2026-08-26 · 工具：`aipmc eval process --session <id> --l2 --l2-sample 2 --l2-timeout 120` · 通道：proxy → DeepSeek
> 约束落盘：PROCESS_QUALITY_SPEC §2.3 P1c 四件套（样本分层 / 观察者隔离 / 对抗验证 / 阈值观察期）
> **观察者隔离（硬约束）**：codex 只产候选与回填证据；人工判定由**用户 + Claude** 执行，实现者不标自己的结果。

## 样本构成（来源分层）

| 来源 | session | 说明 | 例数 |
|---|---|---|---|
| 开发 | `01a00d3c-5262-7200-915b-899ececfad8a` | AIPM 开发（proxy 路由调查），8/17-8/19 | 9 L2 + 1 形态6 |
| 产品 | `01a013f3-e6ca-7f20-8bde-de0414cabe4c` | ED 分享 saga（规格对照物），8/18-8/20 | 8 L2 + 1 形态6 |
| 对抗 | 已知健康时段（见文末） | 约束③ 对抗样本 | 待跑 |

产品样本占比 9/19 ≈ 47%（规格要求 ≥5-8 例，达标）。

## 开发样本（01a00d3c，9 例 L2）

| # | 任务 | 候选 | L2 判定 | 人工判定 |
|---|---|---|---|---|
| D1 | claim_classify | 「关键澄清：你说走 proxy 的 deepseek 通信没问题——Claude 核实后确认实际链路是 `Clau…`」 | 事实 (0.9) | 事实 同意（Claude 8/26 核查：含转述成分但陈述可验证） |
| D2 | evidence_match | 同 D1 断言 | match=无；依据「证据仅显示 aipm_read_discussions last_n=30，未涉及 proxy 链路、虚拟名或本地模型」 | match=无 同意（Claude 8/26 核查） |
| D3 | claim_classify | 「如果你能先让 `01a00eb9` 那个 session 停一下（或确认它已结束），我就开始」 | 意见 (0.7) | 意见 同意（Claude 8/26 核查；注：置信度两次跑 0.7→0.3，LLM 漂移实证） |
| D4 | claim_classify | 「若要根治，可对 inject=on 相位做逐请求 body 字节 diff（capture API 留存有完整 body…」 | 意见 (0.95) | 意见 同意（Claude 8/26 核查） |
| D5 | deadloop_confirm | 2026-08-18 13:40:15 → 13:40:42（go test 重试） | **is_deadloop=true** (0.95)「同一 go test 命令 27 秒内重复执行两次，中间无 edit/检索/根因定位」 | **false**（假阳性）：JSONL 13:40:25 有 apply_patch 修复 metrics_test.go 语法错（日志缺失），真实序列=写测试→失败→修复→通过（bug-20260826-154305-941881） |
| D6 | deadloop_confirm | 2026-08-18 14:25:45 → 14:27:03（go build 重试） | **is_deadloop=false** (0.85) —— **L1 候选被 L2 纠正** | **false 同意**（注记：窗口内 14:26:17 apply_patch 修复 attribution.go 缺失，结论碰巧正确、证据不完整） |
| D7 | feedback_response | 8/17 09:08:17 纠偏（查看上周五与 Claude 的讨论） | responded/deepened/sustained/aligned 全 true；matched_object=「上周五与Claude的讨论」 | 五子全 true 同意（Claude 8/26 核查；L2 解析失败已人工复核） |
| D8 | feedback_response | 8/18 17:57:08 纠偏 | 五子全 true；matched_object=disc-20260818-175637-d0059c | 同意（Claude 8/26 核查） |
| D9 | feedback_response | 8/19 08:55:10 纠偏（查看 Claude 最近分析） | 五子全 true；matched_object=disc-20260819-085212-ed5934 | 同意（Claude 8/26 核查） |

形态 6（D10，L2 未触发）：转向 105 次 / 访问 391 / 新对象 26% —— **人工确认项**：105 转向是「跨 3 天多任务响应指令」还是病态换方案？（L2 因段内自发检索 ≥2 未触发方向评估，§2.2）→ **非病态**（Claude 8/26 核查：105 转向 / 185 次用户介入，平均每次用户消息 <1 次转向，是响应指令）

## 产品样本（01a013f3，8 例 L2）

| # | 任务 | 候选 | L2 判定 | 人工判定 |
|---|---|---|---|---|
| P1 | claim_classify | 「调查完成」 | 进度 (0.9) | 进度 同意（Claude 8/26 核查） |
| P2 | claim_classify | 「**哪些是我的锅**——开局我没调 get_briefing，根因确认后也没顺手 record_bug——不是…」 | 意见 (0.95) | 意见 同意（Claude 8/26 核查） |
| P3 | claim_classify | 「所以要在 1.0.1 里重新加密文件再验证升级路径」 | 意见 (0.7) | 意见 同意（Claude 8/26 核查） |
| P4 | deadloop_confirm | 8/18 17:35:54 → 17:36:41（xcodebuild 重试） | is_deadloop=true (0.85)「同一 xcodebuild 反复执行，中间无修改/检索/根因定位」 | **false**（假阳性）：JSONL 17:36:11 有 apply_patch 修复 FilesDecryptTab.swift（ViewBuilder 裸语句→.onAppear，日志缺失）（bug-20260826-154305-941881） |
| P5 | deadloop_confirm | 8/18 17:50:26 → 17:50:56（xcodebuild 重试） | is_deadloop=true (0.9)「同一 xcodebuild 反复执行（tail -5 / rg -n err），中间无修改/分析/根因定位」 | **部分成立**（描述修正）：run1→run2 真盲试；run2→run3 之间 17:50:42 有 apply_patch 修复（日志缺失），非「中间零修改」（bug-20260826-154305-941881） |
| P6 | feedback_response | 8/18 16:19:51 纠偏 | 五子全 true；matched_object=「讨论」 | 五子全 true 同意（Claude 8/26 核查；L2 解析失败已人工复核） |
| P7 | feedback_response | 8/19 08:47:58 纠偏（验证 1.0.1 基线） | 五子全 true；matched_object=「1.0.1基线（commit 18d5ad4）」；note 提及存在反复但核心动作一致 | 同意（Claude 8/26 核查） |
| P8 | feedback_response | 8/20 17:46:22 纠偏（密友分享 sheet） | 五子全 true；matched_object=「密友分享」 | 同意（Claude 8/26 核查：立即 record_bug + link_entities，最强样本） |

形态 6（P9，L2 未触发）：转向 36 次 / 访问 149 / 新对象 26% —— **人工确认项**：分享 saga 场景的 36 转向（8/18-8/20 跨任务）是否病态。→ **非病态**（Claude 8/26 核查：36 转向跨多子任务——相册多选/打开方式/资云集，需求推进非换方案）

## 对抗样本（约束③，已执行 2026-08-26）

已知健康时段混入，验证 L2 不误判（工具：`aipmc eval l2-probe`，每时段 runs=2 同时测稳定性）：

| 时段 | 说明 | L2 判定 | 一致率 |
|---|---|---|---|
| c0ad2534 06-23 14h（14:00-15:00） | 正常（讨论+检索，build=0 自发=1） | is_deadloop=false（conf 0.97/0.95） | 100% ✓ |
| c0ad2534 06-24 11h（11:00-11:48） | 修复验证期（纠偏→根因→修复，非盲试） | is_deadloop=false（conf 0.95/0.95） | 100% ✓ |

结论：健康时段未被误判为死循环，L2 对抗验证通过；漂移样本有限（2 runs），稳定性全量结论待标注集人工确认后汇总。

## 捕获缺口注记（2026-08-26 人工核查修正）

**发现**：交叉核对 codex 本地 JSONL（UTC）与 discussion_log（本地时区）发现 apply_patch 事件捕获缺口逐日恶化（dev session 01a00d3c：8/17 丢 14% → 8/18 丢 43% → 8/19 丢 54% → 8/20 丢 78%）。根因方向：写路径锁竞争（`~/.aipmc/logs/aipmc.log` 8/17-8/20 共 56 处 database is locked 且逐日上升），hook 在 BUSY 重试期被超时杀死的写会静默丢失（日志无 write_err）。

**四个形态 9 窗口已逐一用 JSONL 复核**（本表人工判定列按复核结果回填）：
- D5 13:40:25 / P4 17:36:11 / P5 17:50:42 / D6 14:26:17 各有缺失的 apply_patch 修复（`patch_apply_end success`）
- D5/P4 为假阳性（L2「中间无 edit」理由不成立）；P5 描述错误；D6 结论正确但证据不完整

**方法论教训**：人工核查与 L2 同源（都基于 discussion_log）会继承捕获缺口——「测量者与被测量者同一」不仅存在于实现者自评，也存在于评审数据源。形态 9 死循环确认前必须做 JSONL 交叉校验。

**修复（已落）**：`store.LogDiscussion` 写失败后落盘 spool（`<runtimeDir>/cache/discussion-spool.jsonl`），下次写前/守护进程启动时补写，杜绝静默丢失；`eval process --l2` 对「窗口内无 write 信号」的 deadloop=true 输出证据完整性警告。

## 复现命令

```bash
./dist/aipmc eval process --session 01a00d3c-5262-7200-915b-899ececfad8a --db .pmai/data/pmai.db --l2 --l2-sample 2 --l2-timeout 120
./dist/aipmc eval process --session 01a013f3-e6ca-7f20-8bde-de0414cabe4c --db /Users/dazsec/projects/EncryptDrive/.pmai/data/pmai.db --l2 --l2-sample 2 --l2-timeout 120
```
