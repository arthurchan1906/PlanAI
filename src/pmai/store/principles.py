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


def list_principles(status: Optional[str] = None, kind: Optional[str] = None) -> List[Dict[str, Any]]:
    conn = get_connection()
    try:
        query = "SELECT * FROM principles"
        params: List[Any] = []
        conditions: List[str] = []
        if status:
            conditions.append("status = ?")
            params.append(status)
        if kind:
            conditions.append("kind = ?")
            params.append(kind)
        if conditions:
            query += " WHERE " + " AND ".join(conditions)
        query += " ORDER BY CASE status WHEN 'active' THEN 0 ELSE 1 END, updated_at DESC, id DESC"
        rows = conn.execute(query, params).fetchall()
        return [
            {
                "id": row["id"],
                "title": row["title"],
                "summary": row["summary"],
                "kind": row["kind"],
                "status": row["status"],
                "created_at": row["created_at"],
                "updated_at": row["updated_at"],
            }
            for row in rows
        ]
    finally:
        conn.close()


def get_principle(principle_id: str) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM principles WHERE id = ?", (principle_id,)).fetchone()
        if not row:
            raise KeyError(principle_id)
        return {
            "id": row["id"],
            "title": row["title"],
            "summary": row["summary"],
            "kind": row["kind"],
            "status": row["status"],
            "created_at": row["created_at"],
            "updated_at": row["updated_at"],
            "links": list_links_for_entity(principle_id),
        }
    finally:
        conn.close()


def get_active_principles(limit: int = 5) -> List[Dict[str, Any]]:
    return list_principles(status="active")[:limit]


def list_active_principles(limit: int = 5) -> List[Dict[str, Any]]:
    return list_principles(status="active")[:limit]


def create_principle(payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        principle_id = slug("principle")
        created_at = now_iso()
        conn.execute(
            """
            INSERT INTO principles (id, title, summary, kind, status, created_at, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?)
            """,
            (
                principle_id,
                payload["title"],
                payload.get("summary", ""),
                payload.get("kind", "governance"),
                payload.get("status", "active"),
                created_at,
                created_at,
            ),
        )
        conn.commit()
        return get_principle(principle_id)
    finally:
        conn.close()


def update_principle(principle_id: str, payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM principles WHERE id = ?", (principle_id,)).fetchone()
        if not row:
            raise KeyError(principle_id)
        conn.execute(
            """
            UPDATE principles
            SET title = ?, summary = ?, kind = ?, status = ?, updated_at = ?
            WHERE id = ?
            """,
            (
                payload.get("title") if payload.get("title") is not None else row["title"],
                payload.get("summary") if payload.get("summary") is not None else row["summary"],
                payload.get("kind") if payload.get("kind") is not None else row["kind"],
                payload.get("status") if payload.get("status") is not None else row["status"],
                now_iso(),
                principle_id,
            ),
        )
        conn.commit()
        return get_principle(principle_id)
    finally:
        conn.close()
