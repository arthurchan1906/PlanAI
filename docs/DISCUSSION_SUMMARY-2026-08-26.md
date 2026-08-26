# 讨论总结 — 2026-08-26(明天继续的锚点)

> 参与:用户 + Claude(1ec35131)+ codex(01a0375b 实现者 / 01a03cf0 独立审核者)
> 关联:thread-20260826-170920-94b5ef(整合路线图)、docs/INTEGRATION_DISCUSSION.md(收敛稿)、docs/P1C_REPORT.md

---

## 一、今日三条线

### 线 1:HARNESS 审核链(P1a → P1b → P1c)→ 已闭环 ✅
- P1a 形态 5-10 L1 扫描器:误报 100%(形态9 git commit 误判、形态10 同刻多匹配)→ 两轮返工 → 合理候选
- P1b L2 五任务确认器:prompt → 编排层(死代码 challenge)→ 超时机制 → **远程 LLM 通道打通**(proxy `/v1/chat/completions` 虚拟模型路由修复,4/4 候选 16s 完成)
- P1c 标注集 19 例:**捕获缺口被发现**(discussion_log 对 apply_patch 丢失 14%→78% 逐日恶化)→ D5/P4 假阳性 → spool 兜底修复链(时点保真/锁内原子性/UNIQUE 幂等/DDL 守卫)→ 误报率报告 82.4%(严格口径)
- **我的教训**:人工核查基于同一套缺失日志被 JSONL 证伪(D6 被 📝 图标误导)—「测量者与被测量者不能是同一证据链」从原则变成实证

### 线 2:产品整合讨论(模块关系)→ 已收敛 ✅
- 01a03cf0 提出四条线地图(A 测量/B PM/C 协作/D 应用悬空)+ 五个「各自为战」实锤(EVAL 不读理解层/两套解析器/EVAL findings 不产生事件/meeting_* 孤儿/自报实测碎片化)
- 我提出两个张力 → 被接受并修正:
  - **张力 1(Goodhart 边界)**:杠杆 1 注入必须「去模式化」— 注入「发生了什么」(记录级事实),不注入「被判了什么」(判定标签);schema 结构强制约束 A(payload 只存 L1 确定性提取)
  - **张力 2(证据独立)**:「判定与展示分离」— EVAL 判定基于原始记录,L2 摘要只用于展示/注入,不作判定输入
- 边界判据最终版:注入「记录级事实」(完整记录序列,可被 agent 逐条核对);否定性归纳(「无修改」)算判定不注入;**前置:捕获完整性校验**(记录序列也可能缺 edit)

### 线 3:真实数据校准 → 修正整合排序 ✅
- 跑 metrics/review/日志,修正三个判断:
  - 理解层 **B1 l2_coverage = 40.1%**(目标 85%),且与 01a03cf0 的 52.8% 打架 = 口径分裂实证
  - **D2 event_processed_rate = 17.7%**(可行动 269 只处理 86)— 事件管道堵
  - spool 兜底已真实触发 3 次(修复生效)
- **修正后的四阶段整合方案(按管道通畅度排序)**:
  - 阶段 0:口径统一 + 测量卫生(seed 清理 ✓ / MCP 写路径泛化 / M0 对账)
  - 阶段 1:事件管道修复(D2,按 actionItem 有无处理动作分层,先可发现性审计)
  - 阶段 2:理解层补齐(历史按需回填,非修管道 — 97 个无摘要 session 是历史未回填)
  - 阶段 3:杠杆 1(EVAL→注入,记录级事实;验收 = 行为改变前后对比 + 注入率监控,不用 D2 处理率)
  - 阶段 4:E6/E7 改进闭环(workflow_score 49.1 不达标)

## 二、已落盘(明天可直接引用)

| 落点 | 内容 |
|---|---|
| `docs/INTEGRATION_DISCUSSION.md` | 整合收敛稿(四条线/实锤/两约束/排序/删减/北极星) |
| `decision-20260826-172138-fb48b1` 等 3 条 | 约束 A(Goodhart 边界)/ 约束 B(判定展示分离)/ 测试隔离教训 — **proposed,待用户冻结** |
| `docs/P1C_REPORT.md` | 误报率报告 82.4%(严格)+ 捕获缺口根因 |
| `docs/P1C_ANNOTATION_SET.md` | 19 例标注(人工判定已回填) |
| thread-20260826-170920-94b5ef | 整合路线图线索(14 实体) |
| idea-20260826-103300 | Review Agent(§6)— 暂不实现 |

## 三、明天待办(按序)

1. **用户拍板**:约束 A/B 冻结(proposed → accepted);P1c 82.4% 确认 → task-20260826-101313-970794 标 done
2. **动作 1**:真实数据校准附录落盘到 INTEGRATION_DISCUSSION.md(Claude 起草,今天的数据在手)
3. **动作 2(核心)**:l2_coverage 口径统一(最小闭环:metrics.go 与 store 对齐,分母=有讨论活动的 distinct session;其余口径差异登记清单,分批修)— 01a0375b 实现,Claude 复核
4. **动作 3**:D2 事件管道诊断 — 按类型拆解(必须处理 vs 参考性)+ 可发现性审计(commit_orphan 54% 未处理是没看到还是没处理)
5. **注意**:MCP 写路径锁竞争(record_decision/add_to_thread 各 locked 一次)—「快速失败+spool」泛化评估,进阶段 0

## 四、一句话总结

**测量体系从「只读仪表盘」走向「闭环方向盘」的路径已讨论清楚:先通管道(事件 D2 + 理解层 B1 + 口径统一),再注入矫正(杠杆 1,记录级事实),一切以真实数据为锚。**

---

## 五、codex 01a03cf0（独立审核者）补充（2026-08-26 收盘追加）

> 来源归属：本节为独立审核者视角的补充，与 Claude 的总结互为补全；其中真实数据发现与审核链结论均为当日实证。

### 5.1 对数据修正方案的三点补充（已被 Claude 接受并深化）

1. **D2 处理率先按事件类型拆解**：hotspot_untracked（16%）与 tentative_link 属「参考性」事件，设计上不要求处理动作，低处理率≠管道堵；须区分「必须处理」（mcp_error / commit_orphan）再定 D2 目标。→ Claude 深化为「按 actionItem 有无处理动作」判据，采纳。
2. **阶段 0 补两项测量卫生**：① MCP 写路径锁竞争泛化（`aipm_record_decision`/`aipm_add_to_thread` 仍用全局 15s busy_timeout，今日各 locked 一次，我亲历）——「快速失败+spool」应从 discussion 路径泛化到 MCP 实体写路径；② M0 对账（task-20260817-095226，in_progress）= Claude C1 的捕获完整性校验，同一交付，显式挂阶段 0。
3. **杠杆 1 验收指标不用 D2 处理率**：eval_finding 事件走自动消费（getCachedContext/buildActionItems），不占人工处理率；验收 = 行为改变前后对比 + 注入率监控（发出/实际注入/被预算裁剪），而非「事件处理率 ≥40%」。

### 5.2 真实数据校准的独有发现（metrics + eval M1-M5 + aipmc.log 逐项 + 生产库交叉）

- **理解层「空转」8/17 已解决**：`no_summary_data` 148 次全部集中在 8/17，此后归零（8/15 文档基线 92% 已过时）；8/26 当日覆盖 3/3（唯一未覆盖是 RunAuto 30min 周期滞后）——97 个无摘要 session 根因 = **历史未回填**（RunAuto 只处理近期窗口 + limit 50），非管道故障。
- **hook 写路径 16:44 后 zero locked 确认成立**：当日 16 时后 5 次 locked = 3 次测试输出 + 2 次 MCP 工具路径，无生产 hook 路径。
- **MCP 实体写路径是下一个锁竞争暴露点**：17:10 add_to_thread、17:43 record_decision 均 SQLITE_BUSY（后者我亲历）——discussion 路径修复未覆盖 MCP 写路径。
- **fileAssoc 截断量化**（M5）：file_cut=51764 vs goals=10326 vs act=23309——每注入约 11 段 fileAssoc 被 800 字符 cap 裁掉，F2「fileAssoc 被裁」判据仍成立，杠杆 5（代码↔任务索引）优先级被数据强化。
- **事件堆积已好转**：当前未消费仅 tentative_link 6 / mcp_error 1 / hotspot 1（对比 8/15 的 41 条 commit_orphan 全堆积）。
- **Agent 画像（杠杆 4）现成输入**：E6 workflow_score 按 agent = claude 60.3 / codex 45.6 / gemini 38.6 / opencode 22.9 / cursor 25.0；M1a 对账按 agent = claude 42.9% / codex 36.7% / cursor 32.6% / opencode 100%。
- **M1a 对账偏低的归因**：部分为 8/26 09:41 proxy 重启前的跨项目分母污染（历史行无 project= 标签），新基线窗口样本小待积累。

### 5.3 审核链结论（今日我审核的提交）

- `ae8786c` → **S1【严重】** TestSpoolDropsWhenFull 无 PMAI_HOME 隔离，删除/污染真实 spool，测试种子 `id='seed'` 被活跃 flush 补写进生产 discussion_log + fts5_index；**S2【中】** 超限丢弃伪装 `spooled`。
- `4f65c89` → **P1-1** 清理不完整：DELETE 后 spool 垃圾仍在，seed 被再补写（清理顺序须先清文件再清表）；**P1-2** `aipmc log` 在 spooled/dropped 路径 `r["id"].(string)` panic（兜底返回值与正常路径不同构）。
- `67beac7` → ✅ 两处修复正确且验证充分：生产库 seed 清零（discussion_log/fts5/spool 全干净）、panic 修复覆盖全部 10 处消费方、PITFALLS 三条教训落盘（PMAI_HOME 隔离 / 先清文件再清表 / 兜底返回值同构）。两轮审核闭环。

### 5.4 对下一阶段（阶段 0）的建议

- **推荐阶段 0（基线固化 + 口径统一），明确排除杠杆 1**：三个前提（事件管道 D2 / 检测点可信 P1c / 口径统一）未就绪前动杠杆 1 = 往堵着的管道灌水。
- 三动作：① 决策冻结（约束 A/B accepted）+ 数据附录落盘；② l2_coverage 口径统一（核心，最小闭环，其余登记「已知口径差异清单」分批修）；③ D2 事件管道诊断（类型分层 + 可发现性审计）。
- 对 Claude 17:49 三点补充（最小闭环分批 / 结构防分叉口径一致性测试 / 可发现性审计）——**全部接受**；「注入率监控」与「结构防分叉」正是约束 A/B 思路的延续。

---

## 六、codex 01a0375b（实现者视角）追加意见（2026-08-26 22:xx，读完全部讨论后）

> 来源归属：本 session 未参与当日讨论，收盘后独立阅读 Claude 总结 + 01a03cf0 补充 + INTEGRATION_DISCUSSION + P1C_REPORT，并核对 metrics.go 口径与 mcp_error 事件后的意见，与上述两方互为补全。

### 6.1 同意的核心
- 约束 A（注入事实不注入判定）/ 约束 B（判定基于原始记录）：设计正确，metrics.go 的 blind_edit_loop warnings 已是 a 风格，有先例，建议 accepted。
- 「阶段 0 优先、排除杠杆 1」：排序正确，前提未就绪前注入 = 往堵着的管道灌水。

### 6.2 需要收紧的结论
1. **82.4% 是「修复前样本上的薄线通过」**：3 例不一致全在 deadloop_confirm、根因全是被测系统的捕获缺口，而 82.4% 正是用这套有缺口的日志测出来的；C1 重跑只修了解析失败，D5/P4 的证据缺口「旧数据不可自愈」。建议 done-gate 挂复核条件：8/26 后新样本复验 ≥80%，而非一次性放行。
2. **观察者隔离存在锚定漏洞**：01a03cf0 作为「独立审核者」是先读 Claude 结论再表态的，82.4% 确认本质是双 agent 互证。对关键数字（82.4%、40.1%）应改为 blind 复核——先看原始数据出结论，再对照实现者报告。
3. **MCP 写路径 spool 泛化是过度设计**：spool 为 hook 路径设计（不能阻塞 commit、时点保真）；MCP 工具调用无此约束，15s busy_timeout + 指数退避重试 3 次即可。且 mcp_error 事件（record_decision SQLITE_BUSY）影响所有 agent 日常写路径，优先级应高于 l2_coverage。

### 6.3 对阶段 0 动作的补充
- **l2_coverage 命名冲突**：「L2 确认器」（P1b LLM 判定器）与 l2_coverage（理解层摘要覆盖率）共用「L2」，40.1% vs 52.8% 打架一部分源于此；建议注册表改名 summary_coverage 消歧。
- **分母需按新口径重校准 85% 目标**：分母含大量短/闲聊 session；8/26 当日 3/3=100%，管道修复后目标值应重估。
- **约束 A 需要可执行 payload schema**：payload 限定纯可观测项（文件/命令/时间戳/消息与编辑计数），任何超出计数的聚合算判定。
- **fileAssoc 截断（51764 被裁）先做注入格式优化**（压缩/只注入最近 N 段），再决定是否上杠杆 5 索引子系统。
- **可发现性审计分三态**：commit_orphan 54% 未处理需区分「没看到」「看到了没处理」「处理了但只 mark_consumed 未 mark_event_processed」，避免高估「没看到」。