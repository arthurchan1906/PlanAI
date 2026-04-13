from __future__ import annotations

import json
from datetime import datetime
from typing import Any, Dict, List, Optional
from uuid import uuid4

from .db import get_connection


def now_iso() -> str:
    return datetime.now().isoformat(timespec="seconds")


def today() -> str:
    return datetime.now().strftime("%Y-%m-%d")


def slug(prefix: str) -> str:
    stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    return f"{prefix}-{stamp}-{uuid4().hex[:6]}"


def list_roadmaps(vision_id: Optional[str] = None) -> List[Dict[str, Any]]:
    conn = get_connection()
    try:
        query = "SELECT * FROM roadmap"
        params = []
        if vision_id:
            query += " WHERE vision_id = ?"
            params.append(vision_id)
        query += " ORDER BY CASE status WHEN 'active' THEN 0 WHEN 'planned' THEN 1 ELSE 2 END, target_date ASC"

        rows = conn.execute(query, params).fetchall()
        return [
            {
                "id": r["id"],
                "vision_id": r["vision_id"],
                "title": r["title"],
                "target_date": r["target_date"],
                "status": r["status"],
                "priority": r["priority"],
                "created_at": r["created_at"],
                "updated_at": r["updated_at"]
            }
            for r in rows
        ]
    finally:
        conn.close()


def get_roadmap(roadmap_id: str) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM roadmap WHERE id = ?", (roadmap_id,)).fetchone()
        if not row:
            raise KeyError(roadmap_id)

        tasks = conn.execute("SELECT id, title, status FROM tasks WHERE roadmap_id = ?", (roadmap_id,)).fetchall()

        return {
            "id": row["id"],
            "vision_id": row["vision_id"],
            "title": row["title"],
            "target_date": row["target_date"],
            "status": row["status"],
            "priority": row["priority"],
            "created_at": row["created_at"],
            "updated_at": row["updated_at"],
            "tasks": [{"id": t["id"], "title": t["title"], "status": t["status"]} for t in tasks]
        }
    finally:
        conn.close()


def create_roadmap(payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        rid = slug("rdm")
        now = now_iso()
        conn.execute(
            """
            INSERT INTO roadmap (id, vision_id, title, target_date, status, priority, created_at, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                rid,
                payload.get("vision_id"),
                payload["title"],
                payload.get("target_date", ""),
                payload.get("status", "planned"),
                payload.get("priority", "P1"),
                now,
                now,
            ),
        )
        conn.commit()
        return get_roadmap(rid)
    finally:
        conn.close()


def update_roadmap(roadmap_id: str, payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM roadmap WHERE id = ?", (roadmap_id,)).fetchone()
        if not row:
            raise KeyError(roadmap_id)

        fields = ["title", "target_date", "status", "priority", "vision_id"]
        updates = []
        params = []
        for f in fields:
            if f in payload:
                updates.append(f"{f} = ?")
                params.append(payload[f])

        if updates:
            updates.append("updated_at = ?")
            params.append(now_iso())
            params.append(roadmap_id)
            query = f"UPDATE roadmap SET {', '.join(updates)} WHERE id = ?"
            conn.execute(query, params)
            conn.commit()

        return get_roadmap(roadmap_id)
    finally:
        conn.close()
