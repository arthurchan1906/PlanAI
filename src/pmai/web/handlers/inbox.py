"""Handlers for inbox related APIs."""
from __future__ import annotations

from typing import Any, Dict

from ...store import get_inbox_summary


def handle_get_inbox() -> Dict[str, Any]:
    return get_inbox_summary()
