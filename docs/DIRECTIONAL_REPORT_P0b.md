# P0b 方向性报告（对象级加深 + 候选→人工确认闭环）

> 门禁物：按 EXECUTION_PLAN §1 P0b 阶段产出——「019ff89b 复合标签方向性验证（对象级「加深」，不承诺精度）+ 候选→人工确认闭环（10 候选时段）+ 误报率报告 + 用户/Claude 三方抽检一致率」。
> 生成：2026-08-24（codex），配套 CLI `aipmc eval p0b --db <ed-db> --session <019ff89b> --confirm-sessions <c0ad2534>,<01a013f3>`（JSON + 人类可读双输出）。
> 落点声明：P0b ⑤ = 对象级方向性（不承诺精度）；⑥ 候选确认 = L1 高召回标记的人工复核，误报率 = 复核判定的误报数/候选总数（L3 校准层输入）。

---

## 1. 阶段完成状态

| P0b 检测点 | 定义 | 实现 | 真实数据结论 |
|---|---|---|---|
| ⑤ 对象级「加深」方向性 | 019ff89b 复合标签（自以为是+单点死磕+重复调查）的对象级验证：对象扩展率/集中度/重复访问 | `DetectObjectDeepening`（eval/pq_p0b.go） | **加深✗ 方向性成立**：领域集中度 56%（EncryptDrive 域）、top1 LocalVaultStore.swift×43（16%）、4 对象 3h+ 重复访问 ≥20 次（最长跨 7h） |
| ⑥ 候选→人工确认闭环 | 10 候选时段（覆盖 5 检测点），每段读 ≤10 条原始记录，人工判定误报 | `SelectConfirmWindows`（eval/pq_p0b.go） | 10 时段已抽取；codex 自评：明确误报 **2/10（20%）**、uncertain 2/10（L2/三方待确认）、true 6/10 |
| 抽检三方一致率 | 用户/Claude 抽检 3 时段（防 codex 自确认主观性） | SpotCheck 标记（[01][08][10]） | **待用户/Claude 判定**（§5 表格） |

复现命令：

```
aipmc eval p0b \
  --db /Users/dazsec/projects/EncryptDrive/.pmai/data/pmai.db \
  --session 019ff89b-5000-71d0-9684-14b6865f3d08 \
  --confirm-sessions c0ad2534-a2da-4c35-8ef6-33207c5e9a91,01a013f3-e6ca-7f20-8bde-de0414cabe4c
```

---

## 2. ⑤ 对象级「加深」方向性（019ff89b，8/13 09:25→17:56，8h31m）

**对象访问 262 次 / 去重对象 60 个 / 领域集中度 56% 落在 `EncryptDrive` 域。**

top 对象（访问次数，首见→末见）：

| 对象 | 次数 | 时段 |
|---|---|---|
| EncryptDrive/Shared/Storage/LocalVaultStore.swift | 43 | 13:08 → 17:56 |
| EncryptDrive/Features/Main/FilesDecryptTab.swift | 35 | 09:55 → 17:35 |
| EncryptDrive.xcodeproj | 24 | 11:55 → 16:50 |
| EncryptDrive/Features/Main/ContentView+Files.swift | 24 | 10:02 → 17:39 |
| EncryptDrive/Features/Main/ContentView.swift | 16 | 09:25 → 17:12 |
| app/src/main/java/.../FloderFragment.java | 15 | 13:35 → 17:52 |

小时窗扩展率（新对象 ÷ 活跃对象）：09:00=100%、10:00=57%、11:00=50%、12:00=0%、13:00=74%、14:00=43%、15:00=57%、16:00=63%、17:00=28%。

**结论：加深✗ 方向性成立（单点死磕 + 重复调查复合：同文件域打转，对象扩展受限）**
- top1 对象集中度 16%（LocalVaultStore 43/262）——对象集合高度集中，单点死磕（形态 8）方向性成立
- 领域集中度 56% 落在 EncryptDrive——同文件域打转（形态 8）
- 4 个对象 3h+ 内重复访问 ≥20 次（最长跨 7h）——重复调查（形态 7）方向性成立
- 17:00 窗扩展率降至 28%——后期新对象扩展放缓

> 口径注记：对象 = `Record.Tool.Files`（post_tool file_path/rel_path 解析产物）；方向性判定 = 启发式（集中度/重复/扩展率），不承诺精度（SPEC §5 P0b「不承诺精度」）；「自以为是（形态 3）」的对准/收敛轴不在对象级，归信号层（P0a 已验）。

---

## 3. ⑥ 候选→人工确认闭环（10 时段，codex 自评）

每时段读 ≤10 条原始记录后判定：`true_positive` = L1 声称被记录证实；`false_positive` = 记录反驳 L1 声称（或该时段实为良性）；`uncertain` = 事实成立但语义（该用/该判）需 L2。

> 快照注记：本表为 commit `953a793` 时刻快照（codex 人工复核原始记录后判定）。2026-08-24 计数修复（📡 text 行识别 + 双行去重，见 §6.2）后重跑：14:02 提示正确归为 responded（原 [04] 假阳性闭环）；10:48 窗口判 missed 语义亦偏弱（用户认可语「很好 继续」+ 此前 10:44-10:46 已密集检索响应，P1 L2）。误报率口径见 §6.2。

| # | 检测点 | session | 时段 | L1 声称 | codex 自评 | 依据 |
|---|---|---|---|---|---|---|
| 01 抽 | 死循环时段该用未用 | c0ad2534 | 15:00-15:30 | 死循环零自发 aipm | **uncertain** | 记录 = nm/objdump/grep 二进制深挖（方法性推进），零 aipm 事实成立；死循环性/「该用」需 L2（15:25 已有 edit 推进） |
| 02 | 死循环时段该用未用 | c0ad2534 | 16:00-16:30 | 死循环零自发 aipm | **true_positive** | 「加诊断日志→重跑→再加 print→再跑」迭代循环，零自发检索——该用未用方向成立 |
| 03 | 用户提示后响应 | c0ad2534 | 17:20-17:50 | hint_responded aipm=13 | **true_positive** | 17:20 提示后 5 秒内 4 连发 aipm_search，窗口内 13 次调用 |
| 04 | 用户提示后响应（missed） | c0ad2534 | 14:02-14:32 | hint_missed aipm=0 | **false_positive** | 14:02:55「查看opencode的分析」→ 14:02:58 `aipm_read_discussions ✅`（3 秒响应）——legacy 格式 read_discussions 为 text 行未被计为 aipm 工具，实际已响应 |
| 05 | 自建记录利用 | c0ad2534 | 11:44-12:44 | record_bug 后 94 条零检索 | **false_positive** | 收尾记录（11:44 record_bug → 11:48 commit+push 收工）——创建即收工非「该用未用」，报告 §3 已知局限预标注同类 |
| 06 | 用户提示后响应 | 01a013f3 | 16:19-16:49 | hint_responded aipm=5 | **true_positive** | 16:19:51 提示「查看Claude的讨论」→ 16:19:56 list_sessions + read_discussions + git 调查 |
| 07 | 静态可核对 | 01a013f3 | 08:35-09:05 | 真机轮次前无 SDK 核对 | **true_positive** | 窗口内 git 基线/资产迁移检索，无 SDK 头文件核对；轮次（09:05 xcodebuild）前确无静态核对 |
| 08 抽 | 重复验证点 | 01a013f3 | 8/19 15:33→8/20 08:49 | 同验证点重复 9 次请求 | **true_positive** | 15:33 fix commit 后 16:45 起继续 ShareExtension 调查，轮内 17:25/17:40 等 9 次「Xcode Run 再测」；用户 8/20 10:16 抗议实证 |
| 09 | 重复验证点 | 01a013f3 | 8/18 16:19→8/19 08:47 | 同验证点重复 5 次请求（跨日轮） | **true_positive** | 与 [08] 同源不同自然轮（跨夜休眠切分），方向一致 |
| 10 抽 | 自建记录利用 | 01a013f3 | 8/18 17:54→8/19 09:07 | create_task 后零检索（延迟 22min 扣休眠） | **uncertain** | 17:54 create_task（新问题）后为当日收尾动作（update/收尾汇报），次日 09:07 首次检索——收尾创建方向性弱，扣休眠后 22min 基本合理 |

---

## 4. 误报率报告（codex 自评，L3 校准层输入）

| 检测点 | 候选数 | true | false | uncertain | 误报率（明确） |
|---|---|---|---|---|---|
| 死循环时段该用未用 | 2 | 1 | 0 | 1 | 0/2 |
| 用户提示后响应（含 missed） | 3 | 2 | **1** | 0 | 1/3（33%） |
| 静态可核对 | 1 | 1 | 0 | 0 | 0/1 |
| 重复验证点 | 2 | 2 | 0 | 0 | 0/2 |
| 自建记录利用 | 2 | 0 | **1** | 1 | 1/2（50%） |
| **合计** | **10** | **6** | **2** | **2** | **2/10（20%）** |

**误报根因（2 个明确误报，均为已知 L1 局限的实证）**：
1. `hint_missed`（[04]）：legacy session（c0ad2534，6/23）`read_discussions` 以 assistant text 行存在（📡 摘要），未解析为 `mcp_aipm_*` 工具 → L1 漏计为「零响应」。**修复方向**：P1 L2 或 text 行 `📡 aipm_` 前缀纳入响应计数（需验证不误伤）。
2. `自建记录利用`（[05]）：收尾记录（record_bug 创建即收工）非「该用未用」——P0a2 报告 §3 已知局限（跨问题域/收尾记录类）首次以原始记录实证。

**uncertain 2/10 的语义待确认**（[01] 死循环「该用」、[10] 收尾创建），归 P1 L2 判定。

---

## 5. 三方抽检材料（用户/Claude 交叉验证，防 codex 自确认）

抽检 3 时段（覆盖 3 个检测点），请用户与 Claude 各自独立判定后填入：

| 抽检时段 | 检测点 | 原始记录要点 | codex 判定 | 用户判定 | Claude 判定 |
|---|---|---|---|---|---|
| [01] c0ad2534 15:00-15:30 | 死循环该用未用 | nm/objdump/grep DZ_Pal + 头文件对比，零 aipm 调用 | uncertain（零 aipm 事实成立，「该用」待 L2） | 待填 | **uncertain**（Claude 第十一轮实测：11 条 objdump/nm 反汇编 + 2 真构建 + 2 edit——定向二进制深挖，「死循环/该用」均无实证） |
| [08] 01a013f3 8/19 15:33→8/20 08:49 | 重复验证点 | fix commit 后 9 次「Xcode Run 再测」请求 | true_positive | 待填 | **true_positive**（Claude 第十一轮实测：8/19 晚 9 次「Xcode Run 再测」+ 用户 8/20 10:16 抗议原文） |
| [10] 01a013f3 8/18 17:54→8/19 09:07 | 自建记录利用 | create_task 后收尾，次日 09:07 首次检索（扣休眠 22min） | uncertain（收尾创建方向性弱） | 待填 | **uncertain**（Claude 第十一轮实测：22min 工作延迟、收尾创建、次日开工即检索——「未利用」语义弱） |

三方判定一致率 = 三方同判数 ÷ 抽检时段数（填齐后计算）。codex=Claude 已 3/3 同判（uncertain/true/uncertain）；用户同判 → 一致率 100% ≥80%，P0b 过门禁。

---

## 6. 已知局限与下一步

1. **窗口记录采样**：每时段仅取 ≤10 条原始记录，长窗口（如 [08] 15:33→次日 08:49）可能遗漏关键请求行——完整判定需原始库复核（复现命令给出）。
2. **legacy 格式漏计**（[04] 实证）：c0ad2534 类 6 月 session 的 aipm 工具行解析不完整，`hint_missed` 与检索计数类检测点存在解析漏报——**已提前修复（2026-08-24，`eval/pq_aipm.go`，对应 Claude 第十一轮「值得提前修」建议）**：`aipmCallName` 将 📡 text 行经 `classifyMcp` 同语义归一化（read_discussions→mcp_aipm_read 等），`aipmCallKey` 按「工具名@秒」去重 mcp_tool+post_tool+text 多行。实测闭环：14:02 提示（原 [04] 假阳性）修复后正确归为 `hint_responded aipm=1`；[06] 双行去重 5→3。连带影响（T4 计数，方向不变）：c0ad2534 被动 46→41、例行 1→11（重复行/文本行计入）、自发/被动 0.04→0.05；死循环候选 15h/16h 不变。修复后重跑新 [04]（10:48）语义偏弱见 §3 注记，归 P1 L2。
3. **方向性不承诺精度**（⑤）：对象级加深判定为启发式（集中度/重复/扩展率），阈值未校准——L3 校准层用本报告误报率输入。
4. **下一步**：用户/Claude 填 §5 抽检表 → 三方一致率 ≥80% 过 P0b 门禁 → P1（形态 5-10 全库扫描 + L2 确认器 + 20 例小标注集）。
