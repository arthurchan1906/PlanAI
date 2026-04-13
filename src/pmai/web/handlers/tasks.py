"""Handlers for tasks related APIs."""
from __future__ import annotations

from typing import Any, Dict, List

from ...store import create_task, get_task, list_tasks, update_task, update_task_checkpoint


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


def _commit_status_hint(commit: Dict[str, Any]) -> str:
    if commit["review_status"] != "approved":
        return "needs_review"
    if commit["test_status"] != "passed":
        return "needs_verification"
    if commit["status"] == "draft":
        return "draft"
    return "ready"


def handle_list_tasks(status: str | None = None) -> Dict[str, Any]:
    return {"tasks": list_tasks(status)}


def handle_create_task(data: Dict[str, Any]) -> Dict[str, Any]:
    return create_task(data)


def handle_get_task(task_id: str) -> Dict[str, Any]:
    return get_task(task_id)


def handle_update_task(task_id: str, status: str, note: str, allow_without_commit: bool) -> Dict[str, Any]:
    return update_task(task_id, status, note, allow_without_commit)


def handle_update_checkpoint(task_id: str, index: int | None, done: bool) -> Dict[str, Any]:
    return update_task_checkpoint(task_id, index, done)


def build_web_task_detail(task_id: str) -> Dict[str, Any]:
    payload = get_task(task_id)
    task = payload["task"]
    linked_commits = payload.get("linked_commits", [])
    links = payload.get("links", {})
    return {
        "task": {
            **task,
            "status_hint": _task_status_hint(task, linked_commits),
        },
        "closure": payload.get("closure", {}),
        "changed_files": payload.get("changed_files", []),
        "linked_commits": [
            {
                **item,
                "short_hash": (item.get("commit_hash") or "")[:8],
                "status_hint": _commit_status_hint(item),
            }
            for item in linked_commits
        ],
        "links": links,
        "link_summary": {
            "outgoing_count": len(links.get("outgoing", [])),
            "incoming_count": len(links.get("incoming", [])),
        },
    }
