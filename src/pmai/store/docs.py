from __future__ import annotations

from datetime import datetime
from pathlib import Path, PurePosixPath
from typing import Any, Dict, List, Optional

from .config import get_project_root
from .db import get_connection

MANAGED_DOC_PREFIXES = ("doc/", "docs/")


def today() -> str:
    return datetime.now().strftime("%Y-%m-%d")


def normalize_doc_path(path: str) -> str:
    raw = str(path or "").strip().replace("\\", "/")
    while raw.startswith("./"):
        raw = raw[2:]
    if not raw:
        raise ValueError("document path is required")
    pure = PurePosixPath(raw)
    parts = pure.parts
    if pure.is_absolute() or any(part in {"", ".", ".."} for part in parts):
        raise ValueError(f"invalid document path: {path}")
    if any(":" in part for part in parts):
        raise ValueError(f"invalid document path: {path}")
    normalized = str(pure)
    if not normalized.lower().endswith(".md"):
        raise ValueError("managed documents must be markdown files")
    if "/" not in normalized:
        return normalized
    if normalized.startswith(MANAGED_DOC_PREFIXES):
        return normalized
    raise ValueError(f"unsupported document location: {path}")


def is_doc_path_normalized(path: str) -> bool:
    try:
        return normalize_doc_path(path) == str(path or "").strip()
    except ValueError:
        return False


def resolve_doc_path(project_root: Path, path: str) -> Path:
    normalized = normalize_doc_path(path)
    root = project_root.resolve()
    file_path = (root / normalized).resolve()
    try:
        file_path.relative_to(root)
    except ValueError as exc:
        raise ValueError(f"document path escapes project root: {path}") from exc
    return file_path


def _doc_path_aliases(path: str) -> List[str]:
    normalized = normalize_doc_path(path)
    aliases = [normalized]
    windows_variant = normalized.replace("/", "\\")
    if windows_variant != normalized:
        aliases.append(windows_variant)
    return aliases


def _find_doc_row(conn, path: str):
    aliases = _doc_path_aliases(path)
    placeholders = ", ".join("?" for _ in aliases)
    return conn.execute(
        f"SELECT * FROM doc_records WHERE path IN ({placeholders}) ORDER BY path = ? DESC LIMIT 1",
        [*aliases, aliases[0]],
    ).fetchone()


def _serialize_doc_row(row: Any) -> Dict[str, Any]:
    return {
        "path": normalize_doc_path(row["path"]),
        "type": row["type"],
        "status": row["status"],
        "layer": row["layer"],
        "source_of_truth": bool(row["source_of_truth"]),
        "last_reviewed": row["last_reviewed"],
        "superseded_by": normalize_doc_path(row["superseded_by"]) if row["superseded_by"] else None,
    }


def list_doc_records(status: Optional[str] = None, layer: Optional[str] = None, normalize: bool = True) -> List[Dict[str, Any]]:
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
        if normalize:
            return [_serialize_doc_row(row) for row in rows]
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
        path = normalize_doc_path(payload["path"])
        row = _find_doc_row(conn, path)
        if not row and not payload.get("create"):
            raise KeyError(path)
        superseded_by = payload.get("superseded_by")
        normalized_superseded_by = None
        if "superseded_by" in payload and superseded_by:
            normalized_superseded_by = normalize_doc_path(superseded_by)
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
                    normalized_superseded_by,
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
                next_superseded_by = normalized_superseded_by
            conn.execute(
                """
                UPDATE doc_records
                SET path = ?, type = ?, status = ?, layer = ?, source_of_truth = ?, last_reviewed = ?, superseded_by = ?
                WHERE path = ?
                """,
                (
                    path,
                    next_type,
                    next_status,
                    next_layer,
                    next_source,
                    next_reviewed,
                    next_superseded_by,
                    row["path"],
                ),
            )
        conn.commit()
        updated = _find_doc_row(conn, path)
        return _serialize_doc_row(updated)
    finally:
        conn.close()


def audit_docs() -> Dict[str, Any]:
    from .doc_governance import audit_docs_comprehensive

    return audit_docs_comprehensive()


def read_doc_content(path: str) -> str:
    project_root = get_project_root()
    file_path = resolve_doc_path(project_root, path)
    if not file_path.exists() or not file_path.is_file():
        raise FileNotFoundError(f"Document not found: {normalize_doc_path(path)}")
    return file_path.read_text(encoding="utf-8")
