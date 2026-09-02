# D1 100 条双标分歧裁决证据表（24 条）

> 归属：M 线 task-20260827-111105-56a2fb（D1 自发率，协议 D1_ATTRIBUTION_PROTOCOL.md）。
> 现状：100 条双标（codex vs claude）严格 label 相同一致率 **76% < 80% 目标**，共 24 条分歧。
> 裁决依据：协议 §1 Q1/Q2 反事实 + §2 判例（#1/#8/#10/#11/#18/#20/#31/#42/#47/#86）+ §5 观察者隔离。
> 仲裁者：**Claude（独立新会话，2026-09-02 定稿）**——既非 8/28 claude 原判者，也非 codex，第三方视角成立（8/26「19 例人工判定回填」判例）。
> 来源：`metrics/d1_disagreements_100.json`。

## 汇总

- 24 条分歧：**12 条倾向 codex 原判**、**12 条倾向 claude 原判**。
- 协议推定标签分布：{'半自发': 12, '任务映射': 11, '被动': 1}。
- 判例支持明确（`clear`）16 条 / 边界可议（`ambiguous`）8 条。
- 若要 76%→80%：只需裁决/修正 **≥4 条**明显错误的标注（将错误方回填为正确方），即可越线。

## 🏛️ 仲裁定稿（2026-09-02，Claude）

> 定稿标签分布：**{半自发: 12, 任务映射: 11, 被动: 1}**（与协议推定完全收敛）。
> 需回填修正：**claude 12 条**（#6 #17 #31 #32 #42 #47 #51 #59 #63 #71 → 任务映射/被动 → 半自发；#86 #97 → 半自发 → 任务映射）、**codex 12 条**（#15 #18 #36 #46 #49 #50 #57 #61 #72 → 半自发 → 任务映射；#56 → 任务映射 → 被动；#83 #89 → 任务映射 → 半自发）。
> 双方在 24 条分歧内与定稿的一致率：**codex 12/24、claude 12/24（对称）**。
> ⚠️ 口径修正（2026-09-02，采纳 codex 审计意见）：**严格双标一致率 = 76.0%（实际测量，<80% 协议门禁，如实未达标）**。分层：aipmc/claude 84%、aipmc/codex 68%、ed/claude 68%、ed/codex 84%（`python3 metrics/d1_agreement.py` 复现）。24 条经第三方裁定回填后全同 = **恒等式，不是可靠性证据**——本轮交付是「82%→76%→? 校准」：产出共识标签集（gold/校准集），**达标验证留待下一批独立双标**（复测窗口）。每标注者 vs 裁定标准吻合度：codex 88/100、claude 88/100。
> 数据缺口提示：24 条 `claude_note` 均为空（codex 有简注），claude 侧理由不可见——但不影响协议标准 §1 反事实判定（依据 user_msg + 工具序列 + 判例可完全判定）。

### 逐条认定

| idx | 场景 | 定稿认定 | 修正方 | 认定理由 |
|---|---|---|---|---|
| 6 | 提交到aipm和git + search_context/smart_search | 半自发 | claude | #20 判例同型：「提交」不需检索，主动检索证据=超出字面 |
| 15 | 回顾昨天的讨论内容 | 任务映射 | codex | 全部检索工具直接实现「回顾」；record_commit/link/update_status 为 #10 回填 |
| 17 | 查看codex的讨论 + update_decision/create_task | 半自发 | claude | 「查看」字面外有写入动作（update_decision/create_task），Q2 超出字面 |
| 18 | 查看codex的回应 注意两个codex session | 任务映射 | codex | 点名「查看」→ read/search_discussions 直接服务，无超字面动作 |
| 31 | 继续 → update_task/record_decision | 半自发 | claude | §2 极简指令判例（#31 即协议判例原样） |
| 32 | 继续 → record_commit/update_bug | 半自发 | claude | §2 极简指令判例 |
| 36 | 长消息 +「你也可以查看Claude的分析」 | 任务映射 | codex | #86 规则：用户建议检索=点名的检索=任务映射 |
| 42 | 继续 → 多查询 search_context/search_discussions | 半自发 | claude | §2 极简指令判例，多查询为自主推进特征 |
| 46 | 查看Claude的challenge | 任务映射 | codex | 点名查看=直接；record_commit/append_task_note 回填(#10) |
| 47 | 好的 → record_commit/append_task_note | 半自发 | claude | §2 极简指令判例（#47 即协议判例原样） |
| 49 | 查看Claude的审核意见 | 任务映射 | codex | 点名查看=直接；record_commit 回填 |
| 50 | 查看Claude的审核意见（晚些） | 任务映射 | codex | 同 #49；append_task_note 回填 |
| 51 | 看cursor修改成果 + record_decision/link_entities | 半自发 | claude | 「看」点名，但 record_decision/link_entities 超出字面；未点名绑定故非被动（claude 被动过严） |
| 56 | 已经安装了 → aipmc_vision | 被动 | codex | #11 判例直接对应：简短确认「已经安装了」隐含点名验证=被动 |
| 57 | task-notification 触发（系统） | 任务映射 | codex | 非用户 prompt 的自主目标：通知驱动的工作流延续，检索+记录均为原任务链；判半自发过宽 |
| 59 | 提交到aipm和git + smart_search/search_context | 半自发 | claude | 与 #6/#20 同型一致 |
| 61 | 总结还未提交的更改 提交到aipm和git | 任务映射 | codex | 点名「总结+提交」，read/list/get 全为总结收集，record_commit 直接，无超字面 |
| 63 | 继续 → get_task/search_context/record | 半自发 | claude | §2 极简指令判例 |
| 71 | 提交到aipm + create_task/link/search | 半自发 | claude | 与 #6 同型（#20 判例） |
| 72 | 你的提示最好竖直居中 | 任务映射 | codex | UI 反馈=隐含修复任务；search_context/get_task 直接实现，record_commit 回填 |
| 83 | 提交到aipm + search_context「备份还原 Android」 | 半自发 | codex | **具体查询词**远超「提交」字面（#20 判例）；codex「标准工作流」判定偏保守 → 需修正 |
| 86 | 我建议你在aipm中搜索相关实现修改记录 | 任务映射 | claude | 协议显式判例 #86（用户明确建议搜索=任务映射） |
| 89 | 很好 提交到aipm + search_context「数据管理/文件页/卡片样式」 | 半自发 | codex | 同 #83：具体查询词超字面；codex 需修正 |
| 97 | 长篇样式修改指令 + record_commit | 任务映射 | claude | 点名修改任务；record_commit 为 #10 回填，非半自发 |

### 仲裁说明

1. **收敛性**：定稿与 codex 的中立协议推定（clear 16 + 8 ambiguous 的推定方向）完全一致——24 条全部按推定方向落定。codex 的克制（分「clear/ambiguous」、留出独立核查）被完全兑付；我逐条独立过了一遍而非照单全收，尤其对 8 条 ambiguous 和 2 条「codex 偏保守」的 clear（#83/#89）做了重点验证：决定性证据是 search_context 的**具体查询词**（「备份还原Android」「数据管理/文件页/卡片样式」）与「提交」无任何字面关系——确认半自发。
2. **分组一致性**：#6/#59/#71（同型但无查询词）与 #83/#89（有查询词）统一按 #20 判例落半自发；#61 因用户点名「总结」（检索即直接实现）落任务映射——「点名检索性任务」与「提交+自主检索」的区别是本案判例群的核心分界线。
3. **「被动」仅施于 #56**（#11 判例精确对应）；#51 曾标被动，但无半点名绑定 → 半自发。
4. **达标判断（如实口径）**：实际测量的严格双标一致率 **76.0%，未达协议 §5 的 80% 门禁**。24 条裁定修正产出的是**共识校准集**，回填后 100% 系恒等式——不能作为「一致性达标」的证据；每标注者 vs 裁定标准各 88%，达标验证应在下一批独立双标（判例增强后预期提高）中检验。任何涉及本批的引用应写「严格双标 76%（门禁未过）+ 24 条经第三方裁定收敛为共识」。
5. **对称性说明（回应 codex 10:43 审计的「平衡注水」质疑，2026-09-02 双方收敛后的表述）**：12/12 对称由**判例族的方向驱动**（极简指令→半自发/点名查看→任务映射/建议检索→任务映射/修改回填→任务映射/#11→被动/#20 检索词→半自发），并非独立公正性证据。分区：**判例直接决定 16 条** = 极简指令 5（#31/32/42/47/63）+ 点名查看无写入 4（#18/46/49/50）+ 用户建议检索 2（#36/#86）+ 修改后回填 2（#72/#97）+ #11 判例 1（#56）+ #20 含检索词 2（#83/#89）；**归族判断+内容裁量 8 条**（#6/#15/#17/#51/#57/#59/#61/#71，其中「某条归哪个族」本身即裁量——如 #15「回顾」归直接实现族与归回溯检索族皆可争，#57 为系统通知特殊情形）→ 裁定为 **5 半自发 / 3 任务映射（并不对半）**。对半出现在判例确定性部分，非仲裁者的方向平衡（撤回「任何忠实执行者同一拆分」这一过强表述）。codex 先后给出「偏宽 19 条」（不含 #56 边缘）与「20 条」（#56 按宽松尺度并入，该组 claude 标签实为 18 任务映射 + 2 被动 + 0 半自发）——两数字按不同口径各自成立，但口径切换未声明；其确凿数字错误为 10:50 称「claude 在该组内判 10 半自发 / 10 任务映射」（实为 0 半自发）。
6. **数据缺口**：24 条 `claude_note` 全空（下批需双方标注理由，否则不可复盘）。

## 逐条证据

格式：`#idx` · 项目/agent · turn · 推荐标签（置信度）

### #6 [aipmc/claude-code] 2026-07-30T13:02:43
- **user_msg**：提交到aipm和git
- **tools**：aipm_get_briefing, aipm_search_context, aipm_list_plans, aipm_get_plan, aipm_list_tasks, aipm_smart_search, aipm_create_task, aipm_record_commit …
- **codex 原判（半自发）**：用户未点名+agent主动检索/记录超出字面
- **claude 原判（任务映射）**：—
- **协议推定 → 半自发**（倾向 codex，置信=`ambiguous`）：#20 判例：「提交到aipm」+ agent 主动 search_context/smart_search 检索证据→半自发；但「提交」本身是直接任务，属边界可议

### #15 [aipmc/claude-code] 2026-08-13T08:57:58
- **user_msg**：回顾昨天的讨论内容
- **tools**：aipm_read_discussions, aipm_get_decision, aipm_get_briefing, aipm_search_context, aipm_list_tasks, aipm_list_plans, aipm_get_task, aipm_record_commit …
- **codex 原判（半自发）**：用户未点名+agent主动检索/记录超出字面
- **claude 原判（任务映射）**：—
- **协议推定 → 任务映射**（倾向 claude，置信=`ambiguous`）：「回顾昨天的讨论」=直接实现（read_discussions/get_decision 服务目标）；record_commit/link/update_status 属任务后回填流程一环（#10）

### #17 [aipmc/claude-code] 2026-08-13T16:37:33
- **user_msg**：查看codex的讨论
- **tools**：aipm_read_discussions, aipm_get_decision, aipm_search_context, aipm_update_decision, aipm_list_tasks, aipm_search_discussions, aipm_list_plans, aipm_get_task …
- **codex 原判（半自发）**：用户点名查看,但update_decision/create_task超字面
- **claude 原判（任务映射）**：—
- **协议推定 → 半自发**（倾向 codex，置信=`ambiguous`）：用户点名「查看」，但 update_decision/create_task/record_commit 明确超出「查看」字面；#8/#18/#20 主动超字面→半自发

### #18 [aipmc/claude-code] 2026-08-14T13:22:15
- **user_msg**：查看codex的回应 注意目前有两个codex session
- **tools**：aipm_read_discussions, aipm_search_discussions
- **codex 原判（半自发）**：用户未点名+agent主动检索/记录超出字面
- **claude 原判（任务映射）**：—
- **协议推定 → 任务映射**（倾向 claude，置信=`clear`）：用户点名「查看codex的回应」，read_discussions/search_discussions 全部直接服务目标，无超字面动作

### #31 [aipmc/codex-cli] 2026-08-07T10:46:59
- **user_msg**：继续
- **tools**：aipm_update_task, aipm_record_decision
- **codex 原判（半自发）**：用户未点名+agent主动检索/记录超出字面
- **claude 原判（任务映射）**：—
- **协议推定 → 半自发**（倾向 codex，置信=`clear`）：极简指令「继续」→最接近自发边界，保留半自发（协议 §2 #31 判例）

### #32 [aipmc/codex-cli] 2026-08-07T13:55:35
- **user_msg**：继续
- **tools**：aipm_record_commit, aipm_update_bug
- **codex 原判（半自发）**：用户未点名+agent主动检索/记录超出字面
- **claude 原判（任务映射）**：—
- **协议推定 → 半自发**（倾向 codex，置信=`clear`）：极简指令「继续」→半自发（同 #31）

### #36 [aipmc/codex-cli] 2026-08-12T17:14:00
- **user_msg**：其实还有一些 agent的消息又时候也会说其他agent在修改之类的 用户除了通过语言命令agent控制行为 还会直接暂停和碰撞的agent进一步交互 等待其他的agent完成之后再考虑要不要继续等手段。另外你也可以查看Claude的分析
- **tools**：aipm_read_discussions, aipm_append_task_note
- **codex 原判（半自发）**：用户未点名+agent主动检索/记录超出字面
- **claude 原判（任务映射）**：—
- **协议推定 → 任务映射**（倾向 claude，置信=`clear`）：用户明确建议「你也可以查看Claude的分析」→ 用户点名/建议的检索=任务映射（协议 #86 修正）

### #42 [aipmc/codex-cli] 2026-08-18T12:06:26
- **user_msg**：继续
- **tools**：aipm_get_plan, aipm_list_tasks, aipm_search_context, aipm_search_context "feedback 修复", aipm_list_plans, aipm_get_task, aipm_update_commit, aipm_update_task …
- **codex 原判（半自发）**：用户未点名+agent主动检索/记录超出字面
- **claude 原判（任务映射）**：—
- **协议推定 → 半自发**（倾向 codex，置信=`clear`）：极简指令「继续」→半自发（协议 #42 判例，含多查询 search_context/search_discussions）

### #46 [aipmc/codex-cli] 2026-08-24T12:29:03
- **user_msg**：查看Claude的challenge
- **tools**：aipm_read_discussions, aipm_record_commit, aipm_append_task_note
- **codex 原判（半自发）**：用户未点名+agent主动检索/记录超出字面
- **claude 原判（任务映射）**：—
- **协议推定 → 任务映射**（倾向 claude，置信=`clear`）：用户点名「查看Claude的challenge」→ read_discussions 直接；record_commit/append_task_note 属流程回填（#10）

### #47 [aipmc/codex-cli] 2026-08-24T14:26:36
- **user_msg**：好的
- **tools**：aipm_record_commit, aipm_append_task_note
- **codex 原判（半自发）**：用户未点名+agent主动检索/记录超出字面
- **claude 原判（任务映射）**：—
- **协议推定 → 半自发**（倾向 codex，置信=`clear`）：极简指令「好的」→半自发（协议 #47 判例）

### #49 [aipmc/codex-cli] 2026-08-26T14:52:54
- **user_msg**：查看Claude的审核意见
- **tools**：aipm_read_discussions, aipm_record_commit
- **codex 原判（半自发）**：用户未点名+agent主动检索/记录超出字面
- **claude 原判（任务映射）**：—
- **协议推定 → 任务映射**（倾向 claude，置信=`clear`）：用户点名「查看Claude的审核意见」→ read_discussions 直接；record_commit 回填

### #50 [aipmc/codex-cli] 2026-08-26T17:09:16
- **user_msg**：查看Claude的审核意见
- **tools**：aipm_read_discussions, aipm_record_commit, aipm_append_task_note
- **codex 原判（半自发）**：用户未点名+agent主动检索/记录超出字面
- **claude 原判（任务映射）**：—
- **协议推定 → 任务映射**（倾向 claude，置信=`clear`）：用户点名「查看Claude的审核意见」→ 同 #49；append_task_note 回填

### #51 [ed/claude-code] 2026-06-16T11:40:03
- **user_msg**：cursor已经修改完成 你看一下他的修改成果
- **tools**：aipm_search_discussions, aipm_record_decision, aipm_record_commit, aipm_link_entities
- **codex 原判（半自发）**：用户点名看成果,但record_decision/commit超出查看字面
- **claude 原判（被动）**：—
- **协议推定 → 半自发**（倾向 codex，置信=`ambiguous`）：用户点名「看修改成果」，但 record_decision/link_entities 超出「看」字面→半自发；非「被动」（未被点名绑定）

### #56 [ed/claude-code] 2026-07-21T16:38:27
- **user_msg**：已经安装了
- **tools**：aipmc_vision
- **codex 原判（任务映射）**：系统通知/贴日志/简短确认→agent被动响应
- **claude 原判（被动）**：—
- **协议推定 → 被动**（倾向 claude，置信=`clear`）：协议 #11 判例：「已经安装了」+ 视觉工具→隐含点名验证，归被动

### #57 [ed/claude-code] 2026-07-27T09:40:11
- **user_msg**：<task-notification>
<task-id>khbdsa1tx</task-id>
<status>completed</status>
<summary>MCP task khbdsa1t (aipm/aipm_get_briefing) completed.</summary>
<result>
🏗️
- **tools**：aipm_read_discussions, aipm_search_discussions, aipm_search_context, aipm_smart_search, aipm_record_commit, aipm_link_entities, aipm_update_task_status, aipm_append_task_note
- **codex 原判（半自发）**：用户未点名+agent主动检索/记录超出字面
- **claude 原判（任务映射）**：—
- **协议推定 → 任务映射**（倾向 claude，置信=`ambiguous`）：task-notification（系统通知）触发，非用户指令；agent 响应自身 MCP 任务完成继续工作流——偏任务映射，但非典型用户prompt

### #59 [ed/claude-code] 2026-07-29T10:47:50
- **user_msg**：提交到aipm和git
- **tools**：aipm_record_commit, aipm_smart_search, aipm_search_context, aipm_get_briefing, aipm_create_task
- **codex 原判（半自发）**：用户未点名+agent主动检索/记录超出字面
- **claude 原判（任务映射）**：—
- **协议推定 → 半自发**（倾向 codex，置信=`ambiguous`）：「提交到aipm和git」+ smart_search/search_context 主动检索→#20 判例偏半自发；但「提交」本身为直接任务

### #61 [ed/claude-code] 2026-07-30T17:56:12
- **user_msg**：总结还未提交的更改 提交到aipm和git中
- **tools**：aipm_read_discussions, aipm_search_context, aipm_get_task, aipm_list_tasks, aipm_list_commits, aipm_get_commit, aipm_get_briefing, aipm_record_commit
- **codex 原判（半自发）**：用户未点名+agent主动检索/记录超出字面
- **claude 原判（任务映射）**：—
- **协议推定 → 任务映射**（倾向 claude，置信=`clear`）：用户点名「总结…提交到aipm和git」；list_commits/get_commit/get_task/read_discussions 全为总结收集，record_commit 直接，无超字面

### #63 [ed/claude-code] 2026-08-03T09:07:16
- **user_msg**：继续
- **tools**：aipm_get_task, aipm_search_context, aipm_record_commits, aipm_list_commits
- **codex 原判（半自发）**：用户未点名+agent主动检索/记录超出字面
- **claude 原判（任务映射）**：—
- **协议推定 → 半自发**（倾向 codex，置信=`clear`）：极简指令「继续」→半自发（同 #31）

### #71 [ed/claude-code] 2026-08-12T09:37:01
- **user_msg**：提交到aipm
- **tools**：aipm_list_tasks, aipm_create_task, aipm_record_commit, aipm_link_entities, aipm_update_task_status, aipm_search_context
- **codex 原判（半自发）**：用户未点名+agent主动检索/记录超出字面
- **claude 原判（任务映射）**：—
- **协议推定 → 半自发**（倾向 codex，置信=`ambiguous`）：「提交到aipm」+ create_task/link_entities/search_context 超字面→#20 偏半自发

### #72 [ed/claude-code] 2026-08-13T10:07:31
- **user_msg**：你的提示最好竖直居中
- **tools**：aipm_search_context, aipm_get_task, aipm_record_commit
- **codex 原判（半自发）**：用户未点名+agent主动检索/记录超出字面
- **claude 原判（任务映射）**：—
- **协议推定 → 任务映射**（倾向 claude，置信=`ambiguous`）：用户给 UI 反馈「提示竖直居中」→ 隐含修复任务，search_context/get_task 为直接实现路径；record_commit 回填

### #83 [ed/codex-cli] 2026-08-10T17:49:46
- **user_msg**：提交到aipm
- **tools**：aipm_search_context "备份还原 Android", aipm_list_commits, aipm_get_commit
- **codex 原判（任务映射）**：用户点名提交/记录→工具是标准工作流
- **claude 原判（半自发）**：—
- **协议推定 → 半自发**（倾向 claude，置信=`clear`）：#20 判例：「提交到aipm」+ 主动 search_context「备份还原 Android」远超字面→半自发；codex「标准工作流」偏保守

### #86 [ed/codex-cli] 2026-08-12T10:27:51
- **user_msg**：另外 我建议你在aipm中搜索相关的实现修改记录 掌握充足的背景资料再做决定
- **tools**：aipm_search_context "查看器保存 编辑 数据丢失", aipm_search_context "私记 导入 去重 mock", aipm_search_context "injectMockData 模拟数据 假数据", aipm_search_context "密友录备份 dzpalsbackup 导出", aipm_search_context "查看器 编辑器 保存 图片查看", aipm_search_context "外部入口 文件关联 快捷操作", aipm_get_briefing, aipm_search_context "对齐审核 review" …
- **codex 原判（任务映射）**：用户明确建议搜索→search_context是用户点名
- **claude 原判（半自发）**：—
- **协议推定 → 任务映射**（倾向 codex，置信=`clear`）：协议显式判例 #86：用户明确建议「在aipm中搜索…」→搜索=用户点名=任务映射

### #89 [ed/codex-cli] 2026-08-13T10:22:43
- **user_msg**：很好 提交到aipm
- **tools**：aipm_search_context "数据管理 文件页 我的文件夹 接收文件 卡片样式", aipm_get_task, aipm_record_commit
- **codex 原判（任务映射）**：用户点名提交/记录→工具是标准工作流
- **claude 原判（半自发）**：—
- **协议推定 → 半自发**（倾向 claude，置信=`clear`）：「很好 提交到aipm」+ search_context「数据管理/文件页/卡片样式」超字面→#20 偏半自发；codex 默认工作流偏保守

### #97 [ed/codex-cli] 2026-08-24T14:29:25
- **user_msg**：样式上的问题需要修改一下。加密分享sheet选中的密友不需要有按钮选中状态，只需要checkbox状态即可。另外有些功能没有根安卓对齐，如在线刷新当前设备按钮，以及“下一步”对应的安卓文案是“开始分享”。还有就是弹出的系统分享菜单后 如果用户没有点击菜单 而是点击了其他的地方 加密分享sheet和分享菜单sheet都会
- **tools**：aipm_record_commit
- **codex 原判（任务映射）**：默认任务映射
- **claude 原判（半自发）**：—
- **协议推定 → 任务映射**（倾向 codex，置信=`clear`）：用户点名样式修改任务；record_commit 为任务完成后回填流程一环（#10），非半自发
