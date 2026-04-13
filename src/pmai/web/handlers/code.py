"""Handlers for code related APIs."""
from __future__ import annotations

from typing import Any, Dict

from ...store import get_git_diff, get_git_recent_commits, get_git_worktree_status


def handle_code_status() -> Dict[str, Any]:
    return get_git_worktree_status()


def handle_code_diff() -> Dict[str, Any]:
    return {"diff": get_git_diff()}


def handle_code_recent(limit: int = 10) -> Dict[str, Any]:
    return {"commits": get_git_recent_commits(limit)}
