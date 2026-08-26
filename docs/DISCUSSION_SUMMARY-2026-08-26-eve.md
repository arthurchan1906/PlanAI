# 讨论总结 — 2026-08-26 晚间（执行器设计 + 方向反思）

> 参与：用户 + codex（01a03e36，本 session）+ claude（6989ad17，审核）
> 关联：thread-20260826-170920-94b5ef（整合线索）、docs/DISCUSSION_SUMMARY-2026-08-26.md（白天总结）、docs/INTEGRATION_DISCUSSION.md
> 状态：讨论锚点，明天据此研究实施方向（不直接开工）

---

## 一、今晚三条线 + 结论

### 线 1：Web 展示（观测侧）→ 暂缓，但修正为先做「不依赖口径」的部分
- Claude 提议把 metrics/eval/status/review 搬上 Web（方案 A：落盘+只读端点）。
- codex 意见：Web 纯人看；agent 消费走 MCP+注入，不走 UI；落盘 schema 现在按两段式 {evidence, verdict} 设计，为将来注入留接口。
- Claude 修正（codex 接受）：**健康概览（ContextPack）+ baseline 不依赖口径统一，可先行**；EVAL/metrics 展示等 l2_coverage 口径统一后。
- 映射：理解层 **P5「前端 PM 面板：仪表盘+Session 流」paused**——重启即接，不是开新 plan。

### 线 2：任务→代码定向索引 → 已存档 idea，不启动
- 用户澄清：不需要 code graph（不会点开看），只给 agent 定位能力省 token（改代码时不用 rg/glob/读文件猜）。
- 方案：任务→代码定向索引（L0 静态符号 / L1 task→文件 / L2 符号→文件定位），按 task 裁剪 3-8 文件，符号清单块注入；先例 Aider repo-map。
- **已存：idea-20260826-220519-5238a6**（挂整合线索）。触发条件：① 阶段 0 注入预算修复 ② grounding 数据量化问题规模。
- Claude 修正（已核实）：**grounding.go 在 eval/archive/，是归档代码，不能当现成能力**；82% 被裁是 8/18 修复前数字（当前 fileAssoc 动态缩放，9 文件 56% 注入率 / 44% 裁剪）。

### 线 3：测量→纠正执行器（今晚最有价值）→ 框架已成型，试点待排期
- 用户点破：测量目的自然是修正 agent 行为，但「如何做」没深入讨论过。
- 核心洞察：**测量是传感器，纠正才是执行器；现在只有传感器设计，没有执行器设计**。
- 为什么测量不自动纠正行为：感知（agent 看不到）/ 归因（事实被动靠自觉）/ 动机（无行动计划）/ 时机（事后对当前 session 无效）。

#### 纠正矩阵（T0-T3，内容 = 规则+事实，永远不注入判定）
| 层级 | 时机 | 内容 | 验证指标 |
|---|---|---|---|
| T0 预防 | session 开始 | 项目规则 + 近期相关事实 | 规则遵守率 |
| T1 情景 | 每次请求 | 命中高风险文件/任务注入事实 | 盲猜率、首文件命中率 |
| T2 实时 | 检测到循环时下一次请求 | 事实序列（不判定） | 死磕中断时长 |
| T3 事后 | session 结束 | 教训 → 下个 session T0 | 跨 session 复发率 |

- 约束 A 的执行器解读：注入「规则（预防性全局）+ 事实（情景性局部）」，**永远不是判定**。「你正在死循环」=判定（禁）；「命令 X 已执行 3 次（13:01/13:05/13:09）exit=1」=事实（安全）；「重试 3 次失败后换方案」=规则（安全）。

#### 死磕 T2 原型（具体可执行）
- **关键发现：注入通道已存在**——getCachedContext()（context_inject.go:238）warnings 段就是按请求注入的机制，blind_edit_loop 已接入。T2 是扩展不是新管道。
- 检测信号（L1 纯规则，不调 LLM）：同命令重试≥3 / 同错误构建重试≥3 / 无 write 窗口>30min / 单文件连改≥5 次无验证成功。
- 注入内容 schema（只允许可观测字段）：命令文本/计数/时间戳/exit code/文件路径；禁判定词、禁建议、禁情绪。
- 误报防护核心洞察：**事实注入没有「误判」，只有「无关」**——事实是真的，agent 可忽略；L2 确认对判定场景需要，对事实注入场景反而有害（延迟+成本+判定词触发 Goodhart）。防护=内容 schema + 频率上限（每信号一次/session≤3/间隔≥5 请求）+ 静默观察。
- 失败降级：查询超 50ms 跳过（fail-open）、spool 异步靠窗口容忍、预算不足丢弃。
- 验证方法学：**影子模式（零风险，先做）**——检测照算、block 生成但不写入 body，只记 shadow 日志，跑 1-2 周看「命中时刻 agent 下一动作是否已换策略」；通过后再 A/B（随机 50% session）。

#### 注入机制澄清（物理位置）
- **通过 proxy，不是 hook**。hook=捕获写库；proxy 转发前 InjectSessionContext(body, agent) 改写请求体（proxy.go:358），[AIPM Context] 块拼进 system prompt/instructions。
- 输入：goals / warnings / actionItems / fileAssoc / guidelines，≤800 字符，顺序 fileAssoc > warnings > actionItems > goals > guidelines。
- 插入位置按 agent 格式：Claude→system message 尾部；Codex→instructions 尾部。
- T2 改动点集中在一个文件：proxy/context_inject.go（加实时本 session 检测源，先例=resolveFileContext 每请求算；影子开关同位置）。

## 二、Claude 全局盘点（6989ad17）→ 已核实两处修正
- **三个「新方向」全部映射到已有任务**：Web→理解层 P5（paused）；执行器→理解层 P0「MCP 工具自发使用率 5%→30%」；代码索引→P2 启动注入 briefing + 协作感知 POC-1。缺的不是新计划，是执行顺序。
- 全局优先级：**① 清阻塞（SQLITE_BUSY 硬 bug + 拍板约束 A/B + P1c）→ ② 收 HARNESS P1a/P1b 审核 → ③ 修口径（D2 三口径 + l2_coverage 40.1 vs 52.8 + B0.5 update_status 显式率 0/142）→ ④ 重启 P5 前端面板 → ⑤ 执行器试点（P0 框架内）**。
- codex 补充（被接受）：**影子模式实验提前并行**（零风险，不依赖口径/前端/审核，现在就能跑，直接回答「注入能否改变行为」）；执行器挂 P0 名下但防「并成一项」（T2 死磕纠正与 MCP 使用率是同一家族不同交付，指标不同）。
- B0.5「update_status 显式率 0/142」本身是最便宜的**第一个行为改变实验**（先可发现性审计：没看到/没动机/机制问题）。

## 三、新研究方向（用户提出，明天重点）
**把开发 AIPM 本身看作普通项目开发，评估 aipm 工具的实际帮助与应有帮助**：
- 在我们开发 AIPM 的过程中，aipm 工具实际提供了什么帮助？哪些环节真正省了事，哪些是负担/噪音？
- 对照普通项目开发流程（写代码/查文档/找问题/跑测试/记需求），aipm 应该提供什么帮助？
- 这是「测量者与被测量者」问题的另一个切面：工具在服务它自己的开发时是否有效，是它能否服务其他项目的最直接证据。

## 四、明天待研（按依赖）
1. **拍板**：约束 A/B accepted；P1c 82.4% 确认（done-gate 挂「修复后样本复验 ≥80%」复核条件）
2. **修 SQLITE_BUSY**（MCP 写路径 retry：15s busy_timeout + 指数退避重试 3 次，不是 spool 泛化）——硬阻塞，连读讨论都失败
3. **修口径**：l2_coverage 最小闭环 + 改名 summary_coverage 消歧 + 指标注册表（口径一致性测试）；D2 三口径
4. **执行器试点设计**（影子模式提前并行）：T2 死磕检测信号 → shadow 日志 → 1-2 周后评估；B0.5 update_status 显式率查因
5. **aipm 自身帮助评估**（新研究）：实际帮助盘点 + 应有帮助设计