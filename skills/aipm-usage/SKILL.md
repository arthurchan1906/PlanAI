---
name: aipm-usage
description: 在接入 AIPM 的项目中，让 claude 主动使用 AIPM 工具获取项目上下文并记录工作。供路2 A/B（skill 开/关）作为干预对象。
---

# AIPM 使用引导

当工作目录存在 `.pmai/`（项目已接入 AIPM）时，践行以下习惯：

1. **开工先看上下文**：调 `aipm_get_briefing` 读项目简报（任务/风险/待办）。
2. **动手前对齐**：用 `aipm_search_context` / `aipm_list_plans` / `aipm_list_tasks` 对齐计划，避免重复或偏离。
3. **过程中记录**：完成一处关键改动后用 `aipm_record_commit`；发现 bug 用 `aipm_record_bug`；有新想法入想法/thread。
4. **收尾闭环**：最终提交用 `aipm_record_commit` 记录，完成后用 `aipm_update_task_status` 标 done。
5. **主动发起**：不被动等用户指示，先看 AIPM 上下文再动手——这是「自发使用」的引导。

> 本 skill 是路2 A/B 的干预对象：ON = 已安装到 claude skill 目录；OFF = 未安装。判定 A/B 效果时，before/after 窗口唯一差异应只在本 skill 开/关。
