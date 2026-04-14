from __future__ import annotations

import json
from datetime import datetime
from typing import Any, Dict, List, Optional
from uuid import uuid4

from .db import get_connection
from .idea_comments import list_idea_comments
from .idea_conversion import enrich_idea_with_conversion


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
        comments = list_idea_comments(idea_id)
        return enrich_idea_with_conversion({
            "id": row["id"],
            "title": row["title"],
            "summary": row["summary"],
            "impact": row["impact"],
            "source": row["source"],
            "status": row["status"],
            "canon_conflict": bool(row["canon_conflict"]),
            "current_summary": row["current_summary"] or row["summary"],
            "main_question": row["main_question"] or "",
            "recommended_next_action": row["recommended_next_action"] or "",
            "updated_at": row["updated_at"] or row["created_at"],
            "created_at": row["created_at"],
            "comments": comments,
            "comment_count": len(comments),
        })
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
            enrich_idea_with_conversion({
                "id": row["id"],
                "title": row["title"],
                "summary": row["summary"],
                "impact": row["impact"],
                "source": row["source"],
                "status": row["status"],
                "canon_conflict": bool(row["canon_conflict"]),
                "current_summary": row["current_summary"] or row["summary"],
                "main_question": row["main_question"] or "",
                "recommended_next_action": row["recommended_next_action"] or "",
                "updated_at": row["updated_at"] or row["created_at"],
                "created_at": row["created_at"],
                "comment_count": conn.execute(
                    "SELECT COUNT(*) FROM idea_comments WHERE idea_id = ?",
                    (row["id"],),
                ).fetchone()[0],
            })
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
            "current_summary": payload.get("current_summary") or payload["summary"],
            "main_question": payload.get("main_question", ""),
            "recommended_next_action": payload.get("recommended_next_action", "continue_discussion"),
            "updated_at": now_iso(),
            "created_at": now_iso(),
        }
        conn.execute(
            """
            INSERT INTO ideas (
                id, title, summary, impact, source, status, canon_conflict,
                current_summary, main_question, recommended_next_action, updated_at, created_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                row["id"],
                row["title"],
                row["summary"],
                row["impact"],
                row["source"],
                row["status"],
                row["canon_conflict"],
                row["current_summary"],
                row["main_question"],
                row["recommended_next_action"],
                row["updated_at"],
                row["created_at"],
            ),
        )
        conn.commit()
        row["canon_conflict"] = bool(row["canon_conflict"])
        row["comment_count"] = 0
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
            "UPDATE ideas SET status = ?, summary = ?, current_summary = ?, updated_at = ? WHERE id = ?",
            (status, summary, summary, now_iso(), idea_id),
        )
        conn.commit()
        return get_idea(idea_id)
    finally:
        conn.close()


def update_idea(idea_id: str, payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM ideas WHERE id = ?", (idea_id,)).fetchone()
        if not row:
            raise KeyError(idea_id)

        updates = []
        params = []
        for field in ("title", "summary", "impact", "source", "status", "current_summary", "main_question", "recommended_next_action"):
            if field in payload and payload[field] is not None:
                updates.append(f"{field} = ?")
                params.append(payload[field])

        if updates:
            updates.append("updated_at = ?")
            params.append(now_iso())
            params.append(idea_id)
            conn.execute(f"UPDATE ideas SET {', '.join(updates)} WHERE id = ?", params)
            conn.commit()
        return get_idea(idea_id)
    finally:
        conn.close()
