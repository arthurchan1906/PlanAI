# P0a2 方向性报告（主动触发 / 静态可核对 / P3 计数基线，2026-08-24）

> 门禁物：按 EXECUTION_PLAN §1 P0a2 阶段产出——「方向性报告（§2.1 分层标注的 P0a2 层检测点均出候选）」。
> 数据源：ED pmai.db 副本（`/tmp/ed_t5_check.db`，2026-08-24 复制，c0ad2534 会话 + 01a013f3 会话）。
> 复现：`aipmc eval p0a2 --session <id> [--fix-hash d628b7a] --db <副本>`（JSON + 人类可读双输出，见文末命令）。
> 代码版本：本报告对应的 eval 提交（pq_proactive / pq_staticcheck / pq_counts / pq_p0a2 + CLI `p0a2` 子命令）。

**落点声明**：P0a2 检测点 = 方向性报告（每检测点出候选即成立），不承诺阈值/验收（PROCESS_QUALITY_SPEC §2.1 口径冻结）。候选为 L1 高召回低精度标记，精确判定归 P1 L2。

---

## 1. 阶段完成状态（阶段 × 检测点 × 对照物）

| P0a2 检测点 | 定义（§2.1） | 实现 | 实证对照物 | 真实数据候选数 |
|---|---|---|---|---|
| 主动触发·死循环时段该用未用 | 死循环候选时段内零自发 aipm 检索（工具采用） | `DetectProactiveTriggers` ① | c0ad2534 15h/16h（47 条去重 aipm 调用全在用户提示后，自发=0） | c0ad2534: **2**（15h/16h 命中 ✓）；01a: 0（无死循环候选，方向一致） |
| 主动触发·用户提示后响应 | 用户提示「查看 aipm/历史/记录/跨 agent 讨论」后窗口内 aipm 检索 | `DetectProactiveTriggers` ② | c0ad2534 17:20「每次在你修改代码之前 你或许最好可以查看搜索aipm中有没有相关记录」→ 30 秒内响应（14 次调用） | c0ad2534: 11（responded 10 + missed 1）；01a: 10（responded 全 ✓） |
| 静态可核对 | 真机轮次前 SDK 头文件/API 签名核对（`open:` vs `openURL:` 教训） | `DetectStaticCheckMisses` | 01a013f3 10:50 真机构建 → 10:52 等你真机验证 → 10:56 崩溃 → 11:15 才 grep iPhoneOS SDK 头文件 | c0ad2534: 0（无 iOS 真机轮次，方向一致）；01a: **7**（10:50/11:00/11:14 真机构建 + 09:42/10:26 崩溃，全在 11:15 首次核对前 ✓） |
| P3 基线·重复验证点 | 同一验证点（无 fix commit/休眠间隔）重复真机验证请求 N 次 | `DetectRepeatedVerification` | 01a013f3 8/19 17:25「请 Xcode Run…再测」+ 8/20 09:05「你直接 Xcode Run 到真机测」→ 8/20 10:16 用户抗议「为什么你要一而再再而三的测试？」 | c0ad2534: 0；01a: **5**（8/19 轮 9 次 + 8/20 轮 3 次，跨夜休眠切分，含两锚点 ✓） |
| P3 基线·自建记录利用 | 自己/aipm 已有记录（bug/commit/task）在后续调试中是否被访问利用 | `DetectSelfRecordUsage` | 01a013f3 8/19 15:32 record_bug（bug-20260819-153222-dd3d52）→ 17:29 才首次检索（延迟 117 分钟） | c0ad2534: 1（11:44 收尾 record_bug 后未再检索，见 §3 局限）；01a: **8**（15:32 record_bug 候选命中 ✓ + 7 个同类） |

---

## 2. 真实数据明细

### 2.1 c0ad2534（死循环样本）

**主动触发**
- `deadloop_no_aipm` ×2：15:00-16:00（build=16 自发=0）、16:00-17:00（build=11 自发=0）——死循环时段该用未用实证命中（对照物：全部 47 条去重 aipm 调用均发生在用户提示后，首条 14:02:58 紧随 14:02:55 提示）。
- `hint_responded` ×10：14:02（aipm=1）、15:50（3）、17:20（14）、17:38（8）、09:11（7）、10:09（3）、10:44（11）、10:45（6）、11:43（5）、11:46（2）——用户提示后窗口内主动检索 ✓（2026-08-24 📡 text 行识别 + 双行去重后，14:02/15:50/11:46 由 missed 归正）。
- `hint_missed` ×1：10:48「我就说aipm中又记录 但是可能你搜索错了位置 很好 继续」（窗口内无 aipm 检索——语义偏弱：认可语，此前 10:44-10:46 已密集检索响应，P1 L2）。

**静态可核对**：无候选（桌面 C/SDK 会话，无 iOS 真机轮次，方向一致）。

**重复验证点**：无候选（无祈使型真机验证请求）。

**自建记录利用**：1 候选——11:44 `record_bug`（收尾记录，用户 11:43 指示「好好记录下来」）后至会话结束零 aipm 检索（工作 94 条，延迟 253 分钟）。局限：记录创建于问题收尾（fix commit 11:48 前），后续 11:50 KSN 新 bug 属新问题域，L1 无跨问题判别 → 该候选为方向性误报样本（见 §3）。

### 2.2 01a013f3（分享扩展 saga）

**主动触发**：10 条 hint 全 responded（16:19/16:29/17:10/17:17/17:29/09:46/11:14/13:06/13:54/15:36）——「查看Claude的讨论/分析/建议」均有 read_discussions 等 aipm 响应 ✓。

**静态可核对**：7 候选，全部发生在 11:15 首次 SDK 头文件核对之前：
| 轮次 | 类型 | 说明 |
|---|---|---|
| 8/19 09:05 | device_cmd | 真机构建 |
| 8/20 09:01 | device_cmd | simctl boot+install 链 |
| 8/20 09:42 | device_error_msg | 用户崩溃栈（EXSinkLoadOperator） |
| 8/20 10:26 | device_error_msg | CoreDeviceError 安装失败 |
| 8/20 10:50 | device_cmd | **open: vs openURL: 教训轮**（改造后直接真机构建） |
| 8/20 11:00 | device_cmd | 修复轮再次真机构建 |
| 8/20 11:14 | device_cmd | 第三次真机构建（11:15 才核对头文件） |

**重复验证点**：5 候选。关键轮次区间（8/19 15:33 commit → 8/20 10:40 commit）经跨夜休眠（≥6h 且跨日，T1 同源口径）切分为两个自然验证轮次：8/19 请求 9 次（16:51-17:45）+ 8/20 请求 3 次（08:53-09:38），含锚点 17:25 + 09:05 —— 即用户 10:16 抗议「为什么你要一而再再而三的测试」所指（合并计算 12 次跨天失真）。

**自建记录利用**：8 候选，每条创建事件后首次 aipm 检索前均有 ≥5 条工作记录。锚点候选 = 8/19 15:32 `record_bug` → 17:29 才首次检索（工作 41 条，延迟 117 分钟）。其余 7 条同类（create_task/record_bug/record_decision 创建后延迟 18-170 分钟才检索——延迟已扣跨夜休眠，17:54 create_task 隔夜 912 分钟实为 22 分钟工作延迟；17:18 record_bug 至会话结束未检索）。

---

## 3. 已知局限（方向性报告如实记录）

1. **L1 时间窗局限**：`hint_missed` 判定依赖 30 分钟窗口——c0ad2534 10:48「我就说aipm中又记录 但是可能你搜索错了位置 很好 继续」为认可语（此前 10:44-10:46 已密集检索响应），窗口内无 aipm 仍报 missed，语义偏弱；精确响应判定归 L2。
2. **跨问题域无判别**：c0ad2534 11:44 record_bug 后 94 条工作记录实为新 bug（KSN）域，L1 无法区分 → 自建记录利用候选含方向性误报。
3. **「查了但查错 API」归 L2**：01a013f3 11:23 第二次崩溃（UIScene options 类型）前窗口内有 11:15 头文件核对（查的是 selector 而非 options 类型）——L1 判为已核对、不候选；「核对不足」需 L2 语义确认（P1）。
4. **assistant 文本消息 Tool 归属**：`_type:stop` 的 last_assistant_message 解析为 `unknown`（非 llm_message）——P0a2 侧以 `isAssistantText`（Role=assistant + unknown + 有内容）兼容，未改 parse.go（避免影响 M1-M5 归因口径）。
5. **工具采用口径**：`aipmRetrievalInWindow` 计全部 `mcp_aipm_*`（search/trace/get/list/read/other）——「用了 aipm 设施」即算采用，不区分检索/状态读取/例行；与 T4 检索意识三分类正交。

---

## 4. 复现命令

```
# 构建（纯 Go，无 CGO/gmssl）
go build -o dist/aipmc .

# c0ad2534（死循环样本，fix commit d628b7a）
./dist/aipmc eval p0a2 --session c0ad2534-a2da-4c35-8ef6-33207c5e9a91 --fix-hash d628b7a --db /tmp/ed_t5_check.db

# 01a013f3（分享扩展 saga）
./dist/aipmc eval p0a2 --session 01a013f3-e6ca-7f20-8bde-de0414cabe4c --db /tmp/ed_t5_check.db
```

测试：`go test ./eval/`（新增 `TestDetectProactiveTriggers` / `TestRecordHintReAnchors` / `TestDetectStaticCheckMisses` / `TestStaticCheckDeviceCmd` / `TestDetectRepeatedVerification` / `TestDetectSelfRecordUsage` / `TestDetectSelfRecordUsageConsultedImmediately`）。
