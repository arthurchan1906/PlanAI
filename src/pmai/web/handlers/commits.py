"""Handlers for commits related APIs."""
from __future__ import annotations

from typing import Any, Dict

from ...store import create_commit, get_commit, list_commits, update_commit


def _commit_status_hint(commit: Dict[str, Any]) -> str:
    if commit["review_status"] != "approved":
        return "needs_review"
    if commit["test_status"] != "passed":
        return "needs_verification"
    if commit["status"] == "draft":
        return "draft"
    return "ready"


def handle_list_commits(status: str | None = None, task_id: str | None = None, decision_id: str | None = None) -> Dict[str, Any]:
    return {"commits": list_commits(status, task_id, decision_id)}


def handle_create_commit(data: Dict[str, Any]) -> Dict[str, Any]:
    return create_commit(data)


def handle_get_commit(commit_id: str) -> Dict[str, Any]:
    return get_commit(commit_id)


def handle_update_commit(commit_id: str, data: Dict[str, Any]) -> Dict[str, Any]:
    return update_commit(commit_id, data)


def build_web_commit_detail(commit_id: str) -> Dict[str, Any]:
    payload = get_commit(commit_id)
    commit = payload["commit"]
    git = payload.get("git", {})
    return {
        "commit": {
            **commit,
            "short_hash": (commit.get("commit_hash") or "")[:8],
            "file_count": len(commit.get("files", [])),
            "status_hint": _commit_status_hint(commit),
        },
        "linked_task": payload.get("linked_task"),
        "linked_decision": payload.get("linked_decision"),
        "links": payload.get("links", {}),
        "git": {
            **git,
            "short_hash": (git.get("commit_hash") or "")[:8],
            "file_count": len(git.get("files", [])),
        },
    }
