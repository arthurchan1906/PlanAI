# D1 自发率归因协议 — Claude 独立复标表（20 条）

> 交付 claude-code 独立标注。**请勿查看 codex 判定**（`metrics/d1_codex_labels.json`），保持双标独立。

> 判定标准（反事实 Q1/Q2）：

- **Q1**：工具调用是否在 user prompt 中被**明确点名**？→ 点名 = **被动**

- **Q2**（未点名时）：移除该 prompt，agent 是否**仍会**做此调用？→ 会 = **自发**；不会但属目标直接实现 = **任务映射**；不会且超出字面拓展 = **半自发**


> 判定标签：`自发`/`半自发`/`任务映射`/`被动`。每 turn 填**主标签** + 一句边界理由。


| # | 项目 | agent | 时间 | 用户消息（截断） | 工具序列 | Claude 判定 | Claude 备注 |
|---|---|---|---|---|---|---|---|
| 01 | aipmc | claude-code | 07-30T14:35 | 既然你已经发现了问题 现在希望你可以站在全局的视角制定合理的方案来解决问题 | aipm_read_discussions |  |  |
| 02 | aipmc | claude-code | 08-07T09:28 | 很好 还有没有challenge的地方 | aipm_update_task, aipm_read_discussions, aipm_create_task |  |  |
| 03 | aipmc | claude-code | 08-13T14:42 | 审核一下codex的代码修改 | aipm_record_commit, aipm_update_commit, aipm_update_task_status, aipm_append_task_note, aipm_get_briefing |  |  |
| 04 | aipmc | claude-code | 08-20T15:01 | 全面深入分析challenge codex的反馈 | aipm_read_discussions |  |  |
| 05 | aipmc | claude-code | 08-26T16:22 | 当前项目中是不是还有很多计划都没有完成 比如以前提到的理解层 后续的metrics snapshot 再后来的review eval | aipm_list_plans, aipm_list_tasks |  |  |
| 06 | aipmc | codex-cli | 06-30T14:28 | 查看Claude给你的意见 | aipm_read_discussions |  |  |
| 07 | aipmc | codex-cli | 08-04T15:10 | 查看Claude的审核意见 | aipm_read_discussions |  |  |
| 08 | aipmc | codex-cli | 08-18T10:28 | map的random range似乎此前也遇到过 建议记录一下 如果以后遇到相关的问题 可以直接定位 | aipm_search_discussions, aipm_search_discussions "map range 随机顺序 排序", aipm_record_decision, aipm_record_commit |  |  |
| 09 | aipmc | codex-cli | 08-19T09:47 | 查看Claude的最新讨论结果 发表你的意见 | aipm_read_discussions |  |  |
| 10 | aipmc | codex-cli | 08-24T12:29 | 查看Claude的challenge | aipm_read_discussions, aipm_record_commit, aipm_append_task_note |  |  |
| 11 | ed | claude-code | 07-21T16:38 | 已经安装了 | aipmc_vision |  |  |
| 12 | ed | claude-code | 07-30T17:56 | 总结还未提交的更改 提交到aipm和git中 | aipm_read_discussions, aipm_search_context, aipm_get_task, aipm_list_tasks, aipm_list_commits, aipm_get_commit, aipm_get_briefing, aipm_record_commit |  |  |
| 13 | ed | claude-code | 08-06T11:02 | 现在项目中的加密分享似乎有些问题 ios创建的加密分享文件 在我没有选择任何其他密友的情况下 只是当前设备加密之后 通过分享按钮 选择加密分享 弹出的密友选择sheet为空 这里还是和安卓统一一下 自己是自己也显示在界面… | aipm_search_context, aipm_list_bugs |  |  |
| 14 | ed | claude-code | 08-11T11:59 | [VaultFlatten] flatten done: flattened=0 removed=0 remaining-depth=0 ⏎ [LocalVaultStore] prepareVaultIfNeeded:… | aipm_get_bug, aipm_append_task_note |  |  |
| 15 | ed | claude-code | 08-12T09:37 | 提交到aipm | aipm_list_tasks, aipm_create_task, aipm_record_commit, aipm_link_entities, aipm_update_task_status, aipm_search_context |  |  |
| 16 | ed | codex-cli | 07-10T16:55 | 查看刚刚和Claude讨论的问题 说说你的方案意见 | aipm_read_discussions, aipm_get_briefing |  |  |
| 17 | ed | codex-cli | 08-03T16:08 | 可以提交 | aipm_record_commit |  |  |
| 18 | ed | codex-cli | 08-14T11:04 | 数据管理中的本地相册点击添加图片添加不了  但是提示加密成功[VaultFlatten] flatten done: flattened=0 removed=0 remaining-depth=0 ⏎ [LocalVau… | aipm_record_bug |  |  |
| 19 | ed | codex-cli | 08-24T14:29 | 样式上的问题需要修改一下。加密分享sheet选中的密友不需要有按钮选中状态，只需要checkbox状态即可。另外有些功能没有根安卓对齐，如在线刷新当前设备按钮，以及“下一步”对应的安卓文案是“开始分享”。还有就是弹出的系… | aipm_record_commit |  |  |
| 20 | ed | codex-cli | 08-24T16:43 | 提交到aipm | aipm_search_context, aipm_search_context "已登录设备列表", aipm_search_context "设备管理 设备列表 sheet", aipm_smart_search, aipm_smart_search "设备列表 UI 对齐安卓 我的页设备管理", aipm_list_commits, aipm_list_plans, aipm_search_context "我的页 设备列表 空状态 背景", aipm_create_task, aipm_update_commit, aipm_update_task_status |  |  |

## 填表指引
- 完成后请把判定回写到 `docs/D1_ANNOTATION_PILOT.md` 的 Claude 列，或单独回传 20 条判定 + 备注。
