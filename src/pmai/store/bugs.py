from __future__ import annotations

import json
from datetime import datetime
from typing import Any, Dict, List, Optional
from uuid import uuid4

from .db import get_connection
from .links import list_links_for_entity


def now_iso() -> str:
    return datetime.now().isoformat(timespec="seconds")


def slug(prefix: str) -> str:
    stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    return f"{prefix}-{stamp}-{uuid4().hex[:6]}"


def dumps(value: Any) -> str:
    return json.dumps(value, ensure_ascii=False)


def loads(value: Optional[str], default: Any) -> Any:
    if not value:
        return default
    try:
        return json.loads(value)
    except (json.JSONDecodeError, TypeError):
        return default


def list_bugs(
    status: Optional[str] = None,
    severity: Optional[str] = None,
    commit_id: Optional[str] = None,
    limit: Optional[int] = None,
) -> List[Dict[str, Any]]:
    conn = get_connection()
    try:
        query = "SELECT * FROM bugs"
        params: List[Any] = []
        where_clauses: List[str] = []
        if status:
            where_clauses.append("status = ?")
            params.append(status)
        if severity:
            where_clauses.append("severity = ?")
            params.append(severity)
        if commit_id:
            where_clauses.append("commit_id = ?")
            params.append(commit_id)
        if where_clauses:
            query += " WHERE " + " AND ".join(where_clauses)
        query += " ORDER BY created_at DESC, id DESC"
        if limit is not None and limit > 0:
            query += " LIMIT ?"
            params.append(limit)
        rows = conn.execute(query, params).fetchall()
        return [
            {
                "id": row["id"],
                "title": row["title"],
                "description": row["description"],
                "severity": row["severity"],
                "status": row["status"],
                "commit_id": row["commit_id"],
                "created_at": row["created_at"],
                "updated_at": row["updated_at"],
            }
            for row in rows
        ]
    finally:
        conn.close()


def get_bug(bug_id: str) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM bugs WHERE id = ?", (bug_id,)).fetchone()
        if not row:
            raise KeyError(bug_id)
        linked_commit = None
        if row["commit_id"]:
            commit_row = conn.execute(
                "SELECT id, title, status, commit_hash FROM commits WHERE id = ?",
                (row["commit_id"],),
            ).fetchone()
            if commit_row:
                linked_commit = {
                    "id": commit_row["id"],
                    "title": commit_row["title"],
                    "status": commit_row["status"],
                    "commit_hash": commit_row["commit_hash"],
                }
        bug = {
            "id": row["id"],
            "title": row["title"],
            "description": row["description"],
            "severity": row["severity"],
            "status": row["status"],
            "commit_id": row["commit_id"],
            "created_at": row["created_at"],
            "updated_at": row["updated_at"],
        }
        return {
            "bug": bug,
            "linked_commit": linked_commit,
            "links": list_links_for_entity(bug_id),
        }
    finally:
        conn.close()


def create_bug(payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        if payload.get("commit_id"):
            commit = conn.execute("SELECT id FROM commits WHERE id = ?", (payload["commit_id"],)).fetchone()
            if not commit:
                raise KeyError(f"commit not found: {payload['commit_id']}")

        bug_id = slug("bug")
        created_at = now_iso()
        row = {
            "id": bug_id,
            "title": payload["title"],
            "description": payload.get("description", ""),
            "severity": payload.get("severity", "minor"),
            "status": payload.get("status", "open"),
            "commit_id": payload.get("commit_id"),
            "created_at": created_at,
            "updated_at": created_at,
        }
        conn.execute(
            """
            INSERT INTO bugs (id, title, description, severity, status, commit_id, created_at, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                row["id"],
                row["title"],
                row["description"],
                row["severity"],
                row["status"],
                row["commit_id"],
                row["created_at"],
                row["updated_at"],
            ),
        )
        conn.commit()
        return row
    finally:
        conn.close()


def update_bug(bug_id: str, payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM bugs WHERE id = ?", (bug_id,)).fetchone()
        if not row:
            raise KeyError(bug_id)

        next_commit_id = row["commit_id"]
        if payload.get("clear_commit_id"):
            next_commit_id = None
        elif "commit_id" in payload and payload.get("commit_id"):
            commit = conn.execute("SELECT id FROM commits WHERE id = ?", (payload["commit_id"],)).fetchone()
            if not commit:
                raise KeyError(f"commit not found: {payload['commit_id']}")
            next_commit_id = payload["commit_id"]

        conn.execute(
            """
            UPDATE bugs
            SET title = ?, description = ?, severity = ?, status = ?, commit_id = ?, updated_at = ?
            WHERE id = ?
            """,
            (
                payload.get("title") if payload.get("title") is not None else row["title"],
                payload.get("description") if payload.get("description") is not None else row["description"],
                payload.get("severity") if payload.get("severity") is not None else row["severity"],
                payload.get("status") if payload.get("status") is not None else row["status"],
                next_commit_id,
                now_iso(),
                bug_id,
            ),
        )
        conn.commit()
        updated = conn.execute("SELECT * FROM bugs WHERE id = ?", (bug_id,)).fetchone()
        return {
            "id": updated["id"],
            "title": updated["title"],
            "description": updated["description"],
            "severity": updated["severity"],
            "status": updated["status"],
            "commit_id": updated["commit_id"],
            "created_at": updated["created_at"],
            "updated_at": updated["updated_at"],
        }
    finally:
        conn.close()
