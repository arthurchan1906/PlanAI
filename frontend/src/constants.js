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
export const BUG_STATUSES = ["open", "in_progress", "resolved", "closed", "wont_fix"];
export const BUG_SEVERITIES = ["critical", "major", "minor", "trivial"];

export const NAV_GROUPS = [
  {
    key: "core",
    label: "Core",
    children: [
      { key: "dashboard", label: "Dashboard" },
      { key: "decisions", label: "Decisions" },
      { key: "commits", label: "Commits" },
      { key: "bugs", label: "Bugs" },
    ],
  },
  {
    key: "strategy",
    label: "Planning",
    children: [
      { key: "planning", label: "Plans" },
      { key: "threads", label: "Threads" },
      { key: "visions", label: "Visions" },
      { key: "principles", label: "Principles" },
      { key: "canon", label: "Canon" },
    ],
  },
  {
    key: "knowledge",
    label: "Knowledge",
    children: [
      { key: "ideas", label: "Ideas" },
      { key: "docs", label: "Docs" },
      { key: "daily", label: "Daily" },
      { key: "code", label: "Code" },
    ],
  },
];

export const NAV_ITEMS = NAV_GROUPS.flatMap((group) => group.children);
