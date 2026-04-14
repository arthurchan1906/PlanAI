"""Handlers for ideas related APIs."""
from __future__ import annotations

from typing import Any, Dict

from ...store import create_idea, create_idea_comment, convert_idea, get_idea, list_ideas, review_idea, update_idea


def handle_list_ideas(status: str | None = None) -> Dict[str, Any]:
    return {"ideas": list_ideas(status)}


def handle_create_idea(data: Dict[str, Any]) -> Dict[str, Any]:
    return create_idea(data)


def handle_get_idea(idea_id: str) -> Dict[str, Any]:
    return get_idea(idea_id)


def handle_review_idea(idea_id: str, status: str, note: str) -> Dict[str, Any]:
    return review_idea(idea_id, status, note)


def handle_update_idea(idea_id: str, data: Dict[str, Any]) -> Dict[str, Any]:
    return update_idea(idea_id, data)


def handle_create_idea_comment(idea_id: str, data: Dict[str, Any]) -> Dict[str, Any]:
    return create_idea_comment(
        idea_id,
        content=data["content"],
        kind=data.get("kind", "comment"),
        author_type=data.get("author_type", "ai"),
        author_name=data.get("author_name", "aipmc"),
    )


def handle_convert_idea(idea_id: str, data: Dict[str, Any]) -> Dict[str, Any]:
    return convert_idea(idea_id, data["target_type"])
