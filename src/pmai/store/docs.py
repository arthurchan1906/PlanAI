from __future__ import annotations

import json
from datetime import datetime
from typing import Any, Dict, List, Optional

from .db import get_connection
from .config import get_db_path, get_project_root


def today() -> str:
    return datetime.now().strftime("%Y-%m-%d")


def list_doc_records(status: Optional[str] = None, layer: Optional[str] = None) -> List[Dict[str, Any]]:
    conn = get_connection()
    try:
        query = "SELECT * FROM doc_records"
        params: List[Any] = []
        conditions: List[str] = []
        if status:
            conditions.append("status = ?")
            params.append(status)
        if layer:
            conditions.append("layer = ?")
            params.append(layer)
        if conditions:
            query += " WHERE " + " AND ".join(conditions)
        query += " ORDER BY source_of_truth DESC, status, path"
        rows = conn.execute(query, params).fetchall()
        return [
            {
                "path": row["path"],
                "type": row["type"],
                "status": row["status"],
                "layer": row["layer"],
                "source_of_truth": bool(row["source_of_truth"]),
                "last_reviewed": row["last_reviewed"],
                "superseded_by": row["superseded_by"],
            }
            for row in rows
        ]
    finally:
        conn.close()


def update_doc_record(payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        path = payload["path"]
        row = conn.execute("SELECT * FROM doc_records WHERE path = ?", (path,)).fetchone()
        if not row and not payload.get("create"):
            raise KeyError(path)
        if not row:
            conn.execute(
                """
                INSERT INTO doc_records (
                    path, type, status, layer, source_of_truth, last_reviewed, superseded_by
                ) VALUES (?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    path,
                    payload.get("type", "unknown"),
                    payload.get("status", "draft"),
                    payload.get("layer", "exploration"),
                    1 if payload.get("source_of_truth") else 0,
                    payload.get("last_reviewed") or today(),
                    payload.get("superseded_by"),
                ),
            )
        else:
            next_type = payload.get("type") or row["type"]
            next_status = payload.get("status") or row["status"]
            next_layer = payload.get("layer") or row["layer"]
            next_source = row["source_of_truth"]
            if payload.get("source_of_truth") is True:
                next_source = 1
            if payload.get("clear_source_of_truth"):
                next_source = 0
            next_reviewed = payload.get("last_reviewed") or today()
            next_superseded_by = row["superseded_by"]
            if "superseded_by" in payload:
                next_superseded_by = payload.get("superseded_by")
            conn.execute(
                """
                UPDATE doc_records
                SET type = ?, status = ?, layer = ?, source_of_truth = ?, last_reviewed = ?, superseded_by = ?
                WHERE path = ?
                """,
                (
                    next_type,
                    next_status,
                    next_layer,
                    next_source,
                    next_reviewed,
                    next_superseded_by,
                    path,
                ),
            )
        conn.commit()
        updated = conn.execute("SELECT * FROM doc_records WHERE path = ?", (path,)).fetchone()
        return {
            "path": updated["path"],
            "type": updated["type"],
            "status": updated["status"],
            "layer": updated["layer"],
            "source_of_truth": bool(updated["source_of_truth"]),
            "last_reviewed": updated["last_reviewed"],
            "superseded_by": updated["superseded_by"],
        }
    finally:
        conn.close()


def audit_docs() -> Dict[str, Any]:
    from .doc_governance import audit_docs_comprehensive
    return audit_docs_comprehensive()


def read_doc_content(path: str) -> str:
    project_root = get_project_root()
    file_path = (project_root / path).resolve()
    if not file_path.exists() or not file_path.is_file():
        raise FileNotFoundError(f"Document not found: {path} (Project Root: {project_root}, Full Path: {file_path})")
    return file_path.read_text(encoding="utf-8")
