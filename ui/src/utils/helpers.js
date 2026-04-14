// Status constants
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

// Utility functions
export function splitValues(raw) {
  return String(raw || "")
    .split("|")
    .map((item) => item.trim())
    .filter(Boolean);
}

export function statusColor(status) {
  if (["accepted", "active", "done", "in_progress"].includes(status)) return "green";
  if (["rejected", "obsolete", "dropped", "blocked"].includes(status)) return "red";
  if (["superseded", "archived"].includes(status)) return "default";
  return "gold";
}

export function todayString() {
  return new Date().toISOString().slice(0, 10);
}

export function toTitleMap(items) {
  return new Map((items || []).map((item) => [item.id, item.title]));
}

export function buildTaskPayload(form) {
  return {
    title: form.title,
    priority: form.priority,
    phase: form.phase,
    roadmap_id: form.roadmapId || null,
    plan_id: form.planId || null,
    acceptance: splitValues(form.acceptance),
  };
}

export function buildCanonPayload(form) {
  return {
    decision_id: form.decisionId,
    product_goal: form.productGoal,
    engineering_focus: form.engineeringFocus,
    architecture: form.architecture,
    add_scope: splitValues(form.addScope),
    add_avoid: splitValues(form.addAvoid),
  };
}

export function buildCommitPayload(form) {
  return {
    title: form.title,
    summary: form.summary,
    evidence_summary: form.evidenceSummary,
    review_notes: form.reviewNotes,
    branch: form.branch,
    task_id: form.taskId || null,
    decision_id: form.decisionId || null,
    status: form.status,
    test_status: form.testStatus,
    review_status: form.reviewStatus,
    files: splitValues(form.files),
  };
}

export function buildDocPayload(form) {
  return {
    path: form.path,
    type: form.type,
    status: form.status,
    layer: form.layer,
    create: true,
    source_of_truth: form.sourceOfTruth,
    clear_source_of_truth: !form.sourceOfTruth,
  };
}

export function buildDailyPayload(form) {
  return {
    completed: splitValues(form.completed),
    problems: splitValues(form.problems),
    risks: splitValues(form.risks),
    next: splitValues(form.next),
  };
}
