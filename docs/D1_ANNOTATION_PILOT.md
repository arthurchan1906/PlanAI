# D1 自发率归因协议 — 试点标注表（20 条）

> 生成：2026-08-28 · codex · 数据源：aipmc + ED 双库 `discussion_log`（`metrics/d1_annotation_pilot.json` 可复算）

> 抽样：排除点破日（≥2026-08-27），仅 `codex-cli` + `claude-code`（用户明确 cursor/opencode 暂不考量），4 组×5 条均匀分布。

> 标注单位：**user turn** = 一条 user 消息 → 下一条 user 消息之间的工具调用序列。

> 判定标准（反事实 Q1/Q2）：

- **Q1**：工具调用是否在 user prompt 中被**明确点名**？→ 点名 = 明确绑定（**被动**）

- **Q2**（未点名时）：移除该 prompt，agent 是否**仍会**做此调用？→ 会 = **自发**；不会但属目标直接实现 = **任务映射**（不算自发）；不会且超出字面拓展 = **半自发**


> 判定标签：`自发` / `半自发` / `任务映射` / `被动`。每个 turn 判定**主标签** + 备注边界理由。

> 100 条正式双标批：`metrics/d1_baseline_100.json` + `docs/D1_BASELINE_100.md` + 24 条分歧裁定 `docs/D1_DISAGREEMENTS_100_ADJUDICATION.md` + gold 共识 `metrics/d1_gold_100.json`（严格双标 76% below-gate，详见 `D1_ATTRIBUTION_PROTOCOL.md`）。本试点表为 20 条 pilot，已被正式批 supersede 作参照。


## 样本与判定

| # | 项目 | agent | 时间 | 用户消息（截断） | 工具序列 | codex 判定 | codex 备注 | Claude 判定 | Claude 备注 | 一致? |
|---|---|---|---|---|---|---|---|---|---|---|
| 01 | aipmc | claude-code | 07-30T14:35 | 既然你已经发现了问题 现在希望你可以站在全局的视角制定合理的方案来解决问题 | aipm_read_discussions |半自发 |用户点目标制定方案,未点名工具;read_discussions 查全局证据属目标直接实现,但主动读讨论带查证倾向|任务映射 |工具调用属用户指令目标直接实现|❌|
| 02 | aipmc | claude-code | 08-07T09:28 | 很好 还有没有challenge的地方 | aipm_update_task, aipm_read_discussions, aipm_create_task |任务映射 |用户点名challenge→read_discussions 拉待挑战点,update/create 属挑战后常规管理,均任务直接实现|任务映射 |工具调用属用户指令目标直接实现|✅|
| 03 | aipmc | claude-code | 08-13T14:42 | 审核一下codex的代码修改 | aipm_record_commit, aipm_update_commit, aipm_update_task_status, aipm_append_task_note, aipm_get_briefing |任务映射 |用户点名审核代码→record/update/append/get_briefing 审核后回填常规动作,属任务直接实现|任务映射 |工具调用属用户指令目标直接实现|✅|
| 04 | aipmc | claude-code | 08-20T15:01 | 全面深入分析challenge codex的反馈 | aipm_read_discussions |任务映射 |用户点名分析challenge→read_discussions 直接实现|任务映射 |工具调用属用户指令目标直接实现|✅|
| 05 | aipmc | claude-code | 08-26T16:22 | 当前项目中是不是还有很多计划都没有完成 比如以前提到的理解层 后续的metrics snapshot 再后来的review eval | aipm_list_plans, aipm_list_tasks |任务映射 |用户点名计划是否完成→list_plans/list_tasks 直接回应|任务映射 |工具调用属用户指令目标直接实现|✅|
| 06 | aipmc | codex-cli | 06-30T14:28 | 查看Claude给你的意见 | aipm_read_discussions |任务映射 |用户点名查看Claude意见→read_discussions 直接实现|任务映射 |工具调用属用户指令目标直接实现|✅|
| 07 | aipmc | codex-cli | 08-04T15:10 | 查看Claude的审核意见 | aipm_read_discussions |任务映射 |用户点名查看审核意见→read_discussions 直接实现|任务映射 |工具调用属用户指令目标直接实现|✅|
| 08 | aipmc | codex-cli | 08-18T10:28 | map的random range似乎此前也遇到过 建议记录一下 如果以后遇到相关的问题 可以直接定位 | aipm_search_discussions, aipm_search_discussions "map range 随机顺序 排序", aipm_record_decision, aipm_record_commit |半自发 |用户点名记录→record_decision/commit 是实现;但 search_discussions 主动回溯map range经验,超出字面记录,属半自发查证|半自发 |用户点名记录,但search_discussions map range主动回溯超字面|✅|
| 09 | aipmc | codex-cli | 08-19T09:47 | 查看Claude的最新讨论结果 发表你的意见 | aipm_read_discussions |任务映射 |用户点名查看最新讨论→read_discussions 直接实现|任务映射 |工具调用属用户指令目标直接实现|✅|
| 10 | aipmc | codex-cli | 08-24T12:29 | 查看Claude的challenge | aipm_read_discussions, aipm_record_commit, aipm_append_task_note |半自发 |用户点名查看challenge→read_discussions 是实现;但 record/append_note 阅读后主动回填,超出查看字面,属半自发|任务映射 |工具调用属用户指令目标直接实现|❌|
| 11 | ed | claude-code | 07-21T16:38 | 已经安装了 | aipmc_vision |任务映射 |用户简短确认已安装;aipmc_vision 视觉工具,非aipm_决策类,代表性弱|被动 |已安装+aipmc_vision,用户点名验证功能|❌|
| 12 | ed | claude-code | 07-30T17:56 | 总结还未提交的更改 提交到aipm和git中 | aipm_read_discussions, aipm_search_context, aipm_get_task, aipm_list_tasks, aipm_list_commits, aipm_get_commit, aipm_get_briefing, aipm_record_commit |任务映射 |用户点名总结并提交→record_commit 直接实现;read/search 为总结前置查证|任务映射 |工具调用属用户指令目标直接实现|✅|
| 13 | ed | claude-code | 08-06T11:02 | 现在项目中的加密分享似乎有些问题 ios创建的加密分享文件 在我没有选择任何其他密友的情况下 只是当前设备加密之后 通过分享按钮 选择加密分享 弹出的密友选择sheet为空 这里还是和安卓统一一下 自己是自己也显示在界面… | aipm_search_context, aipm_list_bugs |任务映射 |用户点名加密分享问题→search_context/list_bugs 定位相关bug|任务映射 |工具调用属用户指令目标直接实现|✅|
| 14 | ed | claude-code | 08-11T11:59 | [VaultFlatten] flatten done: flattened=0 removed=0 remaining-depth=0 ⏎ [LocalVaultStore] prepareVaultIfNeeded:… | aipm_get_bug, aipm_append_task_note |任务映射 |用户贴日志描述悬浮问题→get_bug/append_note 定位记录|任务映射 |工具调用属用户指令目标直接实现|✅|
| 15 | ed | claude-code | 08-12T09:37 | 提交到aipm | aipm_list_tasks, aipm_create_task, aipm_record_commit, aipm_link_entities, aipm_update_task_status, aipm_search_context |任务映射 |用户点名提交到aipm→list/create/record/link/update 标准工作流|任务映射 |工具调用属用户指令目标直接实现|✅|
| 16 | ed | codex-cli | 07-10T16:55 | 查看刚刚和Claude讨论的问题 说说你的方案意见 | aipm_read_discussions, aipm_get_briefing |任务映射 |用户点名查看刚讨论问题→read/get_briefing 直接实现|任务映射 |工具调用属用户指令目标直接实现|✅|
| 17 | ed | codex-cli | 08-03T16:08 | 可以提交 | aipm_record_commit |任务映射 |用户说可以提交→record_commit 直接实现|任务映射 |工具调用属用户指令目标直接实现|✅|
| 18 | ed | codex-cli | 08-14T11:04 | 数据管理中的本地相册点击添加图片添加不了  但是提示加密成功[VaultFlatten] flatten done: flattened=0 removed=0 remaining-depth=0 ⏎ [LocalVau… | aipm_record_bug |半自发 |用户贴117KB设备日志+一句查看昨天修改→record_bug 记录;但未点名aipm工具且日志噪音大,agent主动定位发bug属半自发倾向;边界案例|半自发 |用户贴大日志,agent主动record_bug是主动响应|✅|
| 19 | ed | codex-cli | 08-24T14:29 | 样式上的问题需要修改一下。加密分享sheet选中的密友不需要有按钮选中状态，只需要checkbox状态即可。另外有些功能没有根安卓对齐，如在线刷新当前设备按钮，以及“下一步”对应的安卓文案是“开始分享”。还有就是弹出的系… | aipm_record_commit |任务映射 |用户点名样式问题修改→record_commit 记录改动|任务映射 |工具调用属用户指令目标直接实现|✅|
| 20 | ed | codex-cli | 08-24T16:43 | 提交到aipm | aipm_search_context, aipm_search_context "已登录设备列表", aipm_search_context "设备管理 设备列表 sheet", aipm_smart_search, aipm_smart_search "设备列表 UI 对齐安卓 我的页设备管理", aipm_list_commits, aipm_list_plans, aipm_search_context "我的页 设备列表 空状态 背景", aipm_create_task, aipm_update_commit, aipm_update_task_status |半自发 |用户点名提交→record/update 是实现;但 search_context/smart_search 多处主动检索UI对齐证据,超出提交字面,属半自发查证|半自发 |提交到aipm,但search_context/smart_search主动检索UI对齐超字面|✅|

# 一致率统计: 17/20 = 85.0% (≥80% 达标, 严格 label 相同)

| 项目×agent | 样本 | 一致 | 不一致 | 一致率 |
|---|---|---|---|---|
| aipmc×claude | 5 | 4 | 1 | 80.0% |
| aipmc×codex | 5 | 4 | 1 | 80.0% |
| ed×claude | 5 | 4 | 1 | 80.0% |
| ed×codex | 5 | 5 | 0 | 100.0% |
| 合计 | 20 | 17 | 3 | 85.0% |

## 分歧边界案例（协议参照, 需人工裁决)

| # | 项目/agent | 用户消息 | 工具序列 | codex | claude | 分歧本质 |
|---|---|---|---|---|---|---|
| 01 | aipmc/claude | 站在全局视角制定方案 | read_discussions | 半自发 | 任务映射 | 多义动词「全局视角」是否构成超字面查证 |
| 10 | aipmc/codex | 查看challenge | read+record+append | 半自发 | 任务映射 | 任务完成后的回填动作是否算超字面 |
| 11 | ed/claude | 已经安装了 | aipmc_vision | 任务映射 | 被动 | 「被动 vs 任务映射」边界需明确(user是否点名) |

## 分歧裁决（2026-08-28，用户采纳 Claude 判定）

| # | 原分歧 | 裁决 | 意义 |
|---|---|---|---|
| #1 | codex=半自发 / claude=任务映射 | **任务映射** | 「全局视角」是修饰语不构成超字面 |
| #10 | codex=半自发 / claude=任务映射 | **任务映射** | 任务后回填是流程一环 |
| #11 | codex=任务映射 / claude=被动 | **被动** | 「已经安装了」隐含点名验证功能 |

**裁决后的基准判定**（codex 试用判定 15/5 → 裁决后调整）：半自发参照范例 = #8/#18/#20；反面参照（不算半自发）= #1/#10/#11。

