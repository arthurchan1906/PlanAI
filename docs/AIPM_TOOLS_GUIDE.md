# AIPM 工具映射（指令→唯一工具名；勿猜）

> 本文件为「注入版 guidelines」的 git 镜像：实际注入文件为 `<project>/.pmai/guidelines.md`（`loadGuidelines()`，600B 预算内整段注入，见 `proxy/context_inject.go`）。P0 ②（8/28 建模板 → 9/1 措辞收敛落地）：改的是「指令 → 唯一工具名」直映射 + 语义相近工具的「何时不用」排除句，消除 search_context 类误用。

决策 → list_decisions/get_decision（勿用 search_context）
任务/计划 → list_tasks/get_task/list_plans/get_plan（勿用 search_context）
概况/待办 → get_briefing　提交 → record_commit（已自动；补关联 update_commit）
缺陷 → record_bug　状态流转 → update_task_status　事件 → mark_event_processed（仅已读）
讨论 → read_discussions/search_discussions　关联 → link_entities　追溯 → trace_context
检索实现/变更 → search_context/smart_search（有 ID 再 get_* 下钻）
