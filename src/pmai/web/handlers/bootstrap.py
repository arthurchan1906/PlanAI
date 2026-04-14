"""Handler for /pmai/web/bootstrap endpoint."""
from __future__ import annotations

from typing import Any, Dict, List

from ...store import (
    audit_docs,
    fetch_canon,
    build_context_pack,
    build_handoff_packet,
    build_next_action_packet,
    get_daily_note,
    get_dashboard_summary,
    get_git_recent_commits,
    get_git_worktree_status,
    get_inbox_summary,
    get_module_progress,
    list_commits,
    list_decisions,
    list_doc_records,
    list_ideas,
    list_links,
    list_principles,
    list_plans,
    list_roadmaps,
    list_tasks,
    list_visions,
)


def _commit_status_hint(commit: Dict[str, Any]) -> str:
    if commit["review_status"] != "approved":
        return "needs_review"
    if commit["test_status"] != "passed":
        return "needs_verification"
    if commit["status"] == "draft":
        return "draft"
    return "ready"


def _task_status_hint(task: Dict[str, Any], linked_commits: List[Dict[str, Any]]) -> str:
    if task["status"] == "done":
        return "completed"
    if not linked_commits:
        return "needs_commit"
    if not any(item["review_status"] == "approved" for item in linked_commits):
        return "needs_review"
    if any(item["test_status"] != "passed" for item in linked_commits):
        return "needs_verification"
    return "ready"


def build_web_bootstrap() -> Dict[str, Any]:
    canon = fetch_canon()
    visions = list_visions()
    principles = list_principles()
    roadmaps = list_roadmaps()
    plans = list_plans()
    tasks = list_tasks()
    commits = list_commits()
    decisions = list_decisions()
    ideas = list_ideas()
    docs = list_doc_records()
    doc_audit = audit_docs()
    inbox = get_inbox_summary()
    dashboard = get_dashboard_summary()
    code_status = get_git_worktree_status()
    recent_git_commits = get_git_recent_commits(10)
    daily = get_daily_note()
    module_progress = get_module_progress()
    ai_context = build_context_pack()
    next_packet = build_next_action_packet()
    handoff = build_handoff_packet()

    task_titles = {item["id"]: item["title"] for item in tasks}
    decision_titles = {item["id"]: item["title"] for item in decisions}
    idea_titles = {item["id"]: item["title"] for item in ideas}
    canon_related_decisions = set(canon.get("related_decisions", []))
    converted_links = list_links(relation="converted_to")
    source_ideas_by_target: Dict[str, Dict[str, str]] = {}
    for link in converted_links:
        if link.get("source_type") != "idea":
            continue
        source_ideas_by_target[link["target_id"]] = {
            "id": link["source_id"],
            "title": idea_titles.get(link["source_id"], link["source_id"]),
        }

    commits_by_task: Dict[str, List[Dict[str, Any]]] = {}
    for commit in commits:
        if commit.get("task_id"):
            commits_by_task.setdefault(commit["task_id"], []).append(commit)

    web_tasks = []
    for task in tasks:
        linked_commits = commits_by_task.get(task["id"], [])
        approved_commits = [item for item in linked_commits if item["review_status"] == "approved"]
        verified_commits = [item for item in linked_commits if item["test_status"] == "passed"]
        latest_evidence = next((item for item in linked_commits if item.get("evidence_summary")), None)
        closure_reasons: List[str] = []
        if not linked_commits:
            closure_reasons.append("no_linked_commit")
        if linked_commits and not approved_commits:
            closure_reasons.append("no_approved_commit")
        if linked_commits and not verified_commits:
            closure_reasons.append("verification_incomplete")
        web_tasks.append(
            {
                **task,
                "linked_commit_count": len(linked_commits),
                "approved_commit_count": len(approved_commits),
                "verified_commit_count": len(verified_commits),
                "related_decision_titles": [
                    decision_titles[item]
                    for item in task.get("related_decisions", [])
                    if item in decision_titles
                ],
                "source_idea": source_ideas_by_target.get(task["id"]),
                "latest_evidence_summary": latest_evidence.get("evidence_summary", "") if latest_evidence else "",
                "closure_reasons": closure_reasons,
                "status_hint": _task_status_hint(task, linked_commits),
            }
        )

    web_commits = []
    for commit in commits:
        web_commits.append(
            {
                **commit,
                "task_title": task_titles.get(commit.get("task_id") or "", ""),
                "decision_title": decision_titles.get(commit.get("decision_id") or "", ""),
                "short_hash": (commit.get("commit_hash") or "")[:8],
                "file_count": len(commit.get("files", [])),
                "status_hint": _commit_status_hint(commit),
            }
        )

    web_decisions = []
    for decision in decisions:
        web_decisions.append(
            {
                **decision,
                "related_task_titles": [
                    task_titles[item]
                    for item in decision.get("related_tasks", [])
                    if item in task_titles
                ],
                "source_idea": source_ideas_by_target.get(decision["id"]),
                "canon_synced": decision["id"] in canon_related_decisions,
                "linked_commit_count": len([item for item in commits if item.get("decision_id") == decision["id"]]),
            }
        )

    web_docs = []
    invalid_truth_records = set(doc_audit.get("invalid_truth_records", []))
    obsolete_without_replacement = set(doc_audit.get("obsolete_without_replacement", []))
    for doc in docs:
        issues: List[str] = []
        if doc["path"] in invalid_truth_records:
            issues.append("invalid_truth_record")
        if doc["path"] in obsolete_without_replacement:
            issues.append("obsolete_without_replacement")
        web_docs.append({**doc, "issues": issues})

    plans_by_roadmap: Dict[str, List[Dict[str, Any]]] = {}
    for plan in plans:
        if plan.get("roadmap_id"):
            plans_by_roadmap.setdefault(plan["roadmap_id"], []).append(plan)

    web_roadmaps = []
    for roadmap in roadmaps:
        roadmap_tasks = [item for item in web_tasks if item.get("roadmap_id") == roadmap["id"]]
        done_count = len([item for item in roadmap_tasks if item["status"] == "done"])
        progress = int((done_count / len(roadmap_tasks)) * 100) if roadmap_tasks else 0
        roadmap_plans = plans_by_roadmap.get(roadmap["id"], [])
        web_roadmaps.append(
            {
                **roadmap,
                "task_count": len(roadmap_tasks),
                "plan_count": len(roadmap_plans),
                "progress": progress,
            }
        )

    return {
        "dashboard": dashboard,
        "ai_context": ai_context,
        "next_packet": next_packet,
        "handoff": handoff,
        "inbox": inbox,
        "canon": canon,
        "visions": visions,
        "principles": principles,
        "plans": plans,
        "code_status": code_status,
        "recent_git_commits": recent_git_commits,
        "tasks": web_tasks,
        "commits": web_commits,
        "ideas": ideas,
        "docs": web_docs,
        "doc_audit": doc_audit,
        "decisions": web_decisions,
        "daily": daily,
        "module_progress": module_progress,
        "roadmaps": web_roadmaps,
    }
