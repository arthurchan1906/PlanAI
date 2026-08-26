# 系统整合讨论稿：测量-理解-注入闭环（1+1>>2 路线图 v1）

> 状态：讨论稿（2026-08-26，codex 01a03cf0 × claude 1ec35131 交叉验证后收敛，待用户冻结）
> 关联：thread-20260826-170920-94b5ef（测量-理解-注入闭环整合）
> 本文是「模块不应各自为战」的全局定位分析 + 实施前必须遵守的设计约束。

## 1. 结论先行

未完成/idea 状态的 plan、decision、task 不是孤立想法，全部是「观察→理解→注入→再观察」
闭环四段的缺口。真正瓶颈是**再观察结果不回流**：EVAL 判定的过程质量标签只进报告，
不进下一个 agent 的上下文。但回流必须遵守 Goodhart 边界与证据独立原则（§4），
否则「整合」会变成「污染」。

## 2. 模块地图（四条线 + 支撑层）

| 线 | 模块 | 真实定位 | 状态 |
|---|---|---|---|
| A. Harness 测量线 | hook/proxy/store.discussion/session/eval/metrics | 观察→理解→注入→再观察 | 捕获/理解/归因已通，回流断 |
| B. PM 实体线 | main.go 实体命令 + db 实体表 | 项目管理数据库 + 状态机（agent 自报注解） | 成熟，与测量线脱节 |
| C. 协作感知线 | thread/agent_status/list_sessions/read_discussions/同行感知 plan | 多 agent 协同 | 半成型（感知 L1 停滞） |
| D. 应用线 | agent/app/chatcli/frontend(chat)/vision/search | AIPM 自建 agent | 悬空：历史包袱 + meeting_* 孤儿表 |
| E. 支撑层 | ai/db/u/cli/paths/project | 基础设施 | 清晰 |

**定位判断**：D 线是 6-7 月「AIPM 也是 agent」残留，与 8 月 harness 重心冲突；
建议降级为「内置 agent 试验台」（理解层 P4 的 briefing 注入 task 仍需要），不再作为产品线投入。

## 3. 「各自为战」实锤（代码级，双 session 已验证）

1. **理解层 ↔ 测量层两座孤岛**：eval/ 不读 session_summaries（仅注释提及）；
   理解层产出只流向 briefing + 注入 goals，不流向 EVAL。
2. **测量结果不回流、不产生事件**：events 写入源唯一（store/store.go reconcile）；
   EVAL findings（死循环/伪进展/反馈未响应）不产生事件；注入 warnings 只接 B1 的
   blind_edit_loop 一个 tag。
3. **两套 INJECT 解析器**：metrics.go:405-418（C1/C2）与 eval/attribution.go:193/214/235
   （M1-M5）解析同一批 [INJECT] 日志——M1a project= 口径修复只改了一侧，正是分裂代价。
4. **三套「自报 vs 实测」验证散落**：L-O 未实现 / 轻量一致性校验 todo / 自证检测仅文本推断。
5. **孤儿与层叠**：meeting_* 三表 + agent_assignments 无活跃代码；
   EVAL_PIPELINE 达成判定部分已被 PROCESS_QUALITY_SPEC 取代但文档并列活跃。

## 4. 实施前必须遵守的设计约束（claude 提出，codex 接受）

### 约束 A：Goodhart 边界——注入行为事实，不注入检测结论

规格 §8「检测点不注入 agent 可见上下文」。EVAL findings 回流入注入时必须区分：

- **a. 中性行为事实（安全）**：「上个 session 在这个文件死磕了 22 小时」——
  事实性、去模式化，兑现 decision-20260624 的「给阅读一个验证闭环」。
- **b. 检测结论（危险）**：「形态 10 伪进展」「T5 判定死循环」——模式化，
  agent 学会规避 → Goodhart 启动。

**实现要求**：注入「发生了什么」（文件/命令/时间的事实序列），不替 agent 下结论。
现有 getCachedContext 的 blind_edit_loop warnings 已是 a 风格（注入 evidence 非 tag），
回流设计延续并显式化该风格。events 进注入前必须过「去模式化」过滤。

### 约束 B：判定与展示分离——EVAL 判定基于原始记录

北极星「四层证据独立，任一层被刷，另外三层能发现」。因此：

- EVAL 判定只基于原始记录（discussion_log 原始行 + metadata），现状不变；
- B1 规则 findings（确定性、可审计）可作辅助输入；
- **L2 LLM 摘要不作判定输入**（不可审计、易漂移——D3 置信度 0.7→0.3 实证）；
  仅用于报告展示与注入展示（经约束 A 过滤）。

## 5. 修订后的整合顺序

1. **统一测量发现协议**：EVAL findings 写 events（type=eval_finding，payload=证据事实），
   走既有 emerge→注入/推送路径；进注入前过约束 A 去模式化。
2. **L-T proxy_trace**（规格已冻结）：一个数据源喂 M2 归因 + grounding 盲猜检测。
3. **L-O 验证通道**：hook post-commit 触发验证命令 → 结果写 events/commits 系统字段 →
   统一「自报 vs 实测」（自报膨胀率是核心指标）。
4. **Agent 画像聚合层**：M1-M5 时间序列 + 形态标签分布 + 五子信号 → A/B 实验输入。
5. **代码↔任务索引**（反馈 #29）：注入强触发 + 「自建记录利用」判据。
6. **事件涌现 → thread_suggestion**：跨 agent 协同信号。

## 6. 删减/降级清单

- meeting_* 三表 + agent_assignments：标 deprecated（不动 schema），除非共享记忆方向激活。
- agent/app/chatcli：降级为试验台，不作为产品线投入。
- 两套 INJECT 解析器：合并为单一口径（M1a project= 过滤统一）。
- EVAL_PIPELINE 被取代部分：标 deprecated，避免规格多头。

## 7. 北极星

「测量者与被测量者不能是同一证据链」：捕获（hook/proxy）、理解（session）、测量（eval）、
反馈（注入/报告）四层证据独立；自报只是声称，实测才是事实，必须并列呈现；
闭环活性（M-1：no_summary_data 占比）是分母前提。
