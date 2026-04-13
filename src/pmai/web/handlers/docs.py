"""Handlers for docs related APIs."""
from __future__ import annotations

from typing import Any, Dict

from ...store import audit_docs, list_doc_records, update_doc_record


def handle_list_docs(status: str | None = None, layer: str | None = None) -> Dict[str, Any]:
    return {"records": list_doc_records(status, layer)}


def handle_update_doc(data: Dict[str, Any]) -> Dict[str, Any]:
    return update_doc_record(data)


def handle_audit_docs() -> Dict[str, Any]:
    return audit_docs()
