// 状态常量
export const TASK_STATUSES = ["todo", "in_progress", "blocked", "done", "dropped"];
export const COMMIT_STATUSES = ["draft", "committed", "merged", "released", "dropped"];
export const COMMIT_TEST_STATUSES = ["not_run", "passed", "failed"];
export const COMMIT_REVIEW_STATUSES = ["pending", "approved", "changes_requested"];
export const IDEA_STATUSES = ["inbox", "under_review", "accepted", "rejected", "obsolete"];
export const DECISION_STATUSES = ["proposed", "accepted", "rejected", "superseded"];
export const DOC_STATUSES = ["draft", "active", "archived", "obsolete"];
export const DOC_LAYERS = ["baseline", "decision", "task", "exploration", "history", "topic"];
export const VISION_STATUSES = ["active", "archived", "draft"];
export const PRINCIPLE_STATUSES = ["active", "archived", "draft"];
export const PRINCIPLE_KINDS = ["governance", "engineering", "product", "meta"];

// 导航菜单分组
export const NAV_GROUPS = [
  {
    key: "core",
    label: "核心工作流",
    children: [
      { key: "dashboard", label: "工作台" },
      { key: "tasks", label: "任务" },
      { key: "decisions", label: "决策" },
      { key: "commits", label: "提交" },
    ]
  },
  {
    key: "strategy",
    label: "战略规划",
    children: [
      { key: "visions", label: "愿景" },
      { key: "principles", label: "原则" },
      { key: "canon", label: "规范" },
    ]
  },
  {
    key: "knowledge",
    label: "知识管理",
    children: [
      { key: "ideas", label: "想法" },
      { key: "docs", label: "文档" },
      { key: "daily", label: "日报" },
      { key: "code", label: "代码" },
    ]
  },
];

// 扁平化用于 Menu 组件
export const NAV_ITEMS = NAV_GROUPS.flatMap(g => g.children);
