"""Handlers for daily related APIs."""
from __future__ import annotations

from typing import Any, Dict

from ...store import append_daily_note, get_daily_note, list_daily_notes, replace_daily_note


def handle_get_daily_note(date: str | None = None) -> Dict[str, Any]:
    return get_daily_note(date)


def handle_list_daily_notes() -> Dict[str, Any]:
    return {"items": list_daily_notes()}


def handle_append_daily_note(data: Dict[str, Any], date: str | None = None) -> Dict[str, Any]:
    return append_daily_note(data, date)


def handle_replace_daily_note(data: Dict[str, Any], date: str | None = None) -> Dict[str, Any]:
    return replace_daily_note(data, date)
