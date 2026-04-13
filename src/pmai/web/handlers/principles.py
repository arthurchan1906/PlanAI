"""Handlers for principles related APIs."""
from __future__ import annotations

from typing import Any, Dict

from ...store import create_principle, get_principle, list_principles, update_principle


def handle_list_principles() -> Dict[str, Any]:
    return {"principles": list_principles()}


def handle_create_principle(data: Dict[str, Any]) -> Dict[str, Any]:
    return create_principle(data)


def handle_get_principle(principle_id: str) -> Dict[str, Any]:
    return get_principle(principle_id)


def handle_update_principle(principle_id: str, data: Dict[str, Any]) -> Dict[str, Any]:
    return update_principle(principle_id, data)
