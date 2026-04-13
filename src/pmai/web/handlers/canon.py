"""Handlers for canon related APIs."""
from __future__ import annotations

from typing import Any, Dict

from ...store import fetch_canon, update_canon


def handle_get_canon() -> Dict[str, Any]:
    return fetch_canon()


def handle_update_canon(data: Dict[str, Any]) -> Dict[str, Any]:
    return update_canon(data)
