from __future__ import annotations

import json
from datetime import datetime
from typing import Any, Dict, List, Optional
from uuid import uuid4

from .db import get_connection


def now_iso() -> str:
    return datetime.now().isoformat(timespec="seconds")


def slug(prefix: str) -> str:
    stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    return f"{prefix}-{stamp}-{uuid4().hex[:6]}"


def get_idea(idea_id: str) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM ideas WHERE id = ?", (idea_id,)).fetchone()
        if not row:
            raise KeyError(idea_id)
        return {
            "id": row["id"],
            "title": row["title"],
            "summary": row["summary"],
            "impact": row["impact"],
            "source": row["source"],
            "status": row["status"],
            "canon_conflict": bool(row["canon_conflict"]),
            "created_at": row["created_at"],
        }
    finally:
        conn.close()


def list_ideas(status: Optional[str] = None) -> List[Dict[str, Any]]:
    conn = get_connection()
    try:
        query = "SELECT * FROM ideas"
        params: List[Any] = []
        if status:
            query += " WHERE status = ?"
            params.append(status)
        query += " ORDER BY created_at DESC"
        rows = conn.execute(query, params).fetchall()
        return [
            {
                "id": row["id"],
                "title": row["title"],
                "summary": row["summary"],
                "impact": row["impact"],
                "source": row["source"],
                "status": row["status"],
                "canon_conflict": bool(row["canon_conflict"]),
                "created_at": row["created_at"],
            }
            for row in rows
        ]
    finally:
        conn.close()


def create_idea(payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        idea_id = slug("idea")
        row = {
            "id": idea_id,
            "title": payload["title"],
            "summary": payload["summary"],
            "impact": payload.get("impact", ""),
            "source": payload.get("source", "web"),
            "status": "inbox",
            "canon_conflict": 1 if payload.get("canon_conflict") else 0,
            "created_at": now_iso(),
        }
        conn.execute(
            """
            INSERT INTO ideas (
                id, title, summary, impact, source, status, canon_conflict, created_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                row["id"],
                row["title"],
                row["summary"],
                row["impact"],
                row["source"],
                row["status"],
                row["canon_conflict"],
                row["created_at"],
            ),
        )
        conn.commit()
        row["canon_conflict"] = bool(row["canon_conflict"])
        return row
    finally:
        conn.close()


def review_idea(idea_id: str, status: str, note: str = "") -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM ideas WHERE id = ?", (idea_id,)).fetchone()
        if not row:
            raise KeyError(idea_id)
        summary = row["summary"]
        if note:
            summary = summary.rstrip() + f"\n\n[review-note] {note}"
        conn.execute(
            "UPDATE ideas SET status = ?, summary = ? WHERE id = ?",
            (status, summary, idea_id),
        )
        conn.commit()
        updated = conn.execute("SELECT * FROM ideas WHERE id = ?", (idea_id,)).fetchone()
        return {
            "id": updated["id"],
            "title": updated["title"],
            "summary": updated["summary"],
            "impact": updated["impact"],
            "source": updated["source"],
            "status": updated["status"],
            "canon_conflict": bool(updated["canon_conflict"]),
            "created_at": updated["created_at"],
        }
    finally:
        conn.close()
