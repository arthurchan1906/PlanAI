"""Handlers for links related APIs."""
from __future__ import annotations

from typing import Any, Dict

from ...store import create_link, delete_link, list_links


def handle_list_links() -> Dict[str, Any]:
    return {"links": list_links()}


def handle_create_link(data: Dict[str, Any]) -> Dict[str, Any]:
    return create_link(data)


def handle_delete_link(link_id: str) -> Dict[str, Any]:
    return {"ok": delete_link(link_id)}
