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


def list_visions(status: Optional[str] = None) -> List[Dict[str, Any]]:
    conn = get_connection()
    try:
        query = "SELECT * FROM visions"
        params: List[Any] = []
        if status:
            query += " WHERE status = ?"
            params.append(status)
        query += " ORDER BY CASE status WHEN 'active' THEN 0 ELSE 1 END, updated_at DESC, id DESC"
        rows = conn.execute(query, params).fetchall()
        return [
            {
                "id": row["id"],
                "title": row["title"],
                "summary": row["summary"],
                "status": row["status"],
                "horizon": row["horizon"],
                "created_at": row["created_at"],
                "updated_at": row["updated_at"],
            }
            for row in rows
        ]
    finally:
        conn.close()


def get_vision(vision_id: str) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM visions WHERE id = ?", (vision_id,)).fetchone()
        if not row:
            raise KeyError(vision_id)
        return {
            "id": row["id"],
            "title": row["title"],
            "summary": row["summary"],
            "status": row["status"],
            "horizon": row["horizon"],
            "created_at": row["created_at"],
            "updated_at": row["updated_at"],
            "links": list_links_for_entity(vision_id),
        }
    finally:
        conn.close()


def get_active_vision() -> Optional[Dict[str, Any]]:
    visions = list_visions("active")
    return visions[0] if visions else None


def create_vision(payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        vision_id = slug("vision")
        created_at = now_iso()
        status = payload.get("status", "active")
        if status == "active":
            conn.execute("UPDATE visions SET status = 'archived', updated_at = ? WHERE status = 'active'", (created_at,))
        conn.execute(
            """
            INSERT INTO visions (id, title, summary, status, horizon, created_at, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?)
            """,
            (
                vision_id,
                payload["title"],
                payload.get("summary", ""),
                status,
                payload.get("horizon", "long_term"),
                created_at,
                created_at,
            ),
        )
        conn.commit()
        return get_vision(vision_id)
    finally:
        conn.close()


def update_vision(vision_id: str, payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM visions WHERE id = ?", (vision_id,)).fetchone()
        if not row:
            raise KeyError(vision_id)
        next_status = payload.get("status") if payload.get("status") is not None else row["status"]
        updated_at = now_iso()
        if next_status == "active":
            conn.execute(
                "UPDATE visions SET status = 'archived', updated_at = ? WHERE status = 'active' AND id != ?",
                (updated_at, vision_id),
            )
        conn.execute(
            """
            UPDATE visions
            SET title = ?, summary = ?, status = ?, horizon = ?, updated_at = ?
            WHERE id = ?
            """,
            (
                payload.get("title") if payload.get("title") is not None else row["title"],
                payload.get("summary") if payload.get("summary") is not None else row["summary"],
                next_status,
                payload.get("horizon") if payload.get("horizon") is not None else row["horizon"],
                updated_at,
                vision_id,
            ),
        )
        conn.commit()
        return get_vision(vision_id)
    finally:
        conn.close()
