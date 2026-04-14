from __future__ import annotations

from datetime import datetime
from typing import Any, Dict, List
from uuid import uuid4

from .db import get_connection


def now_iso() -> str:
    return datetime.now().isoformat(timespec="seconds")


def slug(prefix: str) -> str:
    stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    return f"{prefix}-{stamp}-{uuid4().hex[:6]}"


def list_idea_comments(idea_id: str) -> List[Dict[str, Any]]:
    conn = get_connection()
    try:
        rows = conn.execute(
            "SELECT * FROM idea_comments WHERE idea_id = ? ORDER BY created_at ASC, id ASC",
            (idea_id,),
        ).fetchall()
        return [
            {
                "id": row["id"],
                "idea_id": row["idea_id"],
                "author_type": row["author_type"],
                "author_name": row["author_name"],
                "kind": row["kind"],
                "content": row["content"],
                "created_at": row["created_at"],
            }
            for row in rows
        ]
    finally:
        conn.close()


def create_idea_comment(
    idea_id: str,
    *,
    content: str,
    kind: str = "comment",
    author_type: str = "ai",
    author_name: str = "aipmc",
) -> Dict[str, Any]:
    conn = get_connection()
    try:
        idea = conn.execute("SELECT id FROM ideas WHERE id = ?", (idea_id,)).fetchone()
        if not idea:
            raise KeyError(idea_id)

        comment_id = slug("idea-comment")
        created_at = now_iso()
        conn.execute(
            """
            INSERT INTO idea_comments (
                id, idea_id, author_type, author_name, kind, content, created_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?)
            """,
            (comment_id, idea_id, author_type, author_name, kind, content, created_at),
        )
        conn.execute(
            "UPDATE ideas SET updated_at = ? WHERE id = ?",
            (created_at, idea_id),
        )
        conn.commit()
        return {
            "id": comment_id,
            "idea_id": idea_id,
            "author_type": author_type,
            "author_name": author_name,
            "kind": kind,
            "content": content,
            "created_at": created_at,
        }
    finally:
        conn.close()
