# AIPM 工具快捷映射（指令→工具，直映射，勿凭语义相近误判）

- 查技术决策 / 做新决策前：`list_decisions`、`get_decision`（不要用 `search_context` 搜「决策」）
- 看项目概况 / 待办 / 风险：「`get_briefing`」（勿当 PM 管理视图误信）
- 查任务 / 计划：「`list_tasks`、`get_task`、`list_plans`、`get_plan`」（不要用 `search_context` 找 task/plan）
- 提交代码到 PM：「`record_commit`」（git post-commit 已自动，手动补 task 关联用 `update_commit`）
- 记录缺陷：「`record_bug`」
- 回看 / 搜历史讨论：「`read_discussions`、`search_discussions`」
- 找相关实现 / 变更记录：「`search_context`、`smart_search`」
- 标记事件已处理：「`mark_event_processed`」；仅标记已读：「`mark_consumed`」
- 关联实体：「`link_entities`」；追溯关系：「`trace_context`」

要点：先想「这是哪类动作」再选唯一工具名；拿不准先 `list_decisions`/`list_bugs` 浏览，再 `get_*` 下钻。
