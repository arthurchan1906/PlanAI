"""Handlers for visions related APIs."""
from __future__ import annotations

from typing import Any, Dict

from ...store import create_vision, get_vision, list_visions, update_vision


def handle_list_visions() -> Dict[str, Any]:
    return {"visions": list_visions()}


def handle_create_vision(data: Dict[str, Any]) -> Dict[str, Any]:
    return create_vision(data)


def handle_get_vision(vision_id: str) -> Dict[str, Any]:
    return get_vision(vision_id)


def handle_update_vision(vision_id: str, data: Dict[str, Any]) -> Dict[str, Any]:
    return update_vision(vision_id, data)
