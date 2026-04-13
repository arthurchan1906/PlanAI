"""Handlers for ideas related APIs."""
from __future__ import annotations

from typing import Any, Dict

from ...store import create_idea, get_idea, list_ideas, review_idea


def handle_list_ideas(status: str | None = None) -> Dict[str, Any]:
    return {"ideas": list_ideas(status)}


def handle_create_idea(data: Dict[str, Any]) -> Dict[str, Any]:
    return create_idea(data)


def handle_get_idea(idea_id: str) -> Dict[str, Any]:
    return get_idea(idea_id)


def handle_review_idea(idea_id: str, status: str, note: str) -> Dict[str, Any]:
    return review_idea(idea_id, status, note)
