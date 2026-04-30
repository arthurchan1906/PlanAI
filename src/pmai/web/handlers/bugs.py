"""Handlers for bugs related APIs."""
from __future__ import annotations

from typing import Any, Dict

from ...store import create_bug, get_bug, list_bugs, update_bug


def build_web_bug_detail(bug_id: str) -> Dict[str, Any]:
    payload = get_bug(bug_id)
    bug = payload["bug"]
    return {
        "bug": bug,
        "linked_commit": payload.get("linked_commit"),
        "links": payload.get("links", {}),
    }
