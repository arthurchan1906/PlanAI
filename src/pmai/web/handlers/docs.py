"""Handlers for docs related APIs."""
from __future__ import annotations

from typing import Any, Dict

from ...store import audit_docs, list_doc_records, update_doc_record, read_doc_content
from ...store.doc_governance import sync_docs_with_fs, prune_archived_docs


def handle_list_docs(status: str | None = None, layer: str | None = None) -> Dict[str, Any]:
    return {"records": list_doc_records(status, layer)}


def handle_sync_docs() -> Dict[str, Any]:
    return sync_docs_with_fs()


def handle_prune_docs() -> Dict[str, Any]:
    return prune_archived_docs()


def handle_update_doc(data: Dict[str, Any]) -> Dict[str, Any]:
    return update_doc_record(data)


def handle_audit_docs() -> Dict[str, Any]:
    return audit_docs()


def handle_get_doc_content(path: str) -> Dict[str, Any]:
    return {"content": read_doc_content(path)}
