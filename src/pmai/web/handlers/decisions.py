"""Handlers for decisions related APIs."""
from __future__ import annotations

from typing import Any, Dict, List

from ...store import create_decision, get_decision, list_decisions, update_decision_status


def _commit_status_hint(commit: Dict[str, Any]) -> str:
    if commit["review_status"] != "approved":
        return "needs_review"
    if commit["test_status"] != "passed":
        return "needs_verification"
    if commit["status"] == "draft":
        return "draft"
    return "ready"


def handle_list_decisions() -> Dict[str, Any]:
    return {"decisions": list_decisions()}


def handle_create_decision(data: Dict[str, Any]) -> Dict[str, Any]:
    return create_decision(data)


def handle_get_decision(decision_id: str) -> Dict[str, Any]:
    return get_decision(decision_id)


def handle_update_decision_status_handler(decision_id: str, status: str) -> Dict[str, Any]:
    return update_decision_status(decision_id, status)


def build_web_decision_detail(decision_id: str) -> Dict[str, Any]:
    payload = get_decision(decision_id)
    decision = payload["decision"]
    linked_commits = payload.get("linked_commits", [])
    links = payload.get("links", {})
    return {
        "decision": {
            **decision,
            "linked_commit_count": len(linked_commits),
            "related_task_count": len(payload.get("linked_tasks", [])),
        },
        "linked_tasks": payload.get("linked_tasks", []),
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
