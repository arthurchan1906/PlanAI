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


def _serialize_link(row: Any) -> Dict[str, Any]:
    return {
        "id": row["id"],
        "source_type": row["source_type"],
        "source_id": row["source_id"],
        "relation": row["relation"],
        "target_type": row["target_type"],
        "target_id": row["target_id"],
        "note": row["note"],
        "created_at": row["created_at"],
    }


def list_links(source_id: Optional[str] = None, target_id: Optional[str] = None, relation: Optional[str] = None) -> List[Dict[str, Any]]:
    conn = get_connection()
    try:
        query = "SELECT * FROM links"
        params: List[Any] = []
        conditions: List[str] = []
        if source_id:
            conditions.append("source_id = ?")
            params.append(source_id)
        if target_id:
            conditions.append("target_id = ?")
            params.append(target_id)
        if relation:
            conditions.append("relation = ?")
            params.append(relation)
        if conditions:
            query += " WHERE " + " AND ".join(conditions)
        query += " ORDER BY created_at DESC, id DESC"
        rows = conn.execute(query, params).fetchall()
        return [_serialize_link(row) for row in rows]
    finally:
        conn.close()


def list_links_for_entity(entity_id: str) -> Dict[str, List[Dict[str, Any]]]:
    conn = get_connection()
    try:
        outgoing = conn.execute(
            "SELECT * FROM links WHERE source_id = ? ORDER BY created_at DESC, id DESC",
            (entity_id,),
        ).fetchall()
        incoming = conn.execute(
            "SELECT * FROM links WHERE target_id = ? ORDER BY created_at DESC, id DESC",
            (entity_id,),
        ).fetchall()
        return {
            "outgoing": [_serialize_link(row) for row in outgoing],
            "incoming": [_serialize_link(row) for row in incoming],
        }
    finally:
        conn.close()


def create_link(payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        link_id = slug("link")
        row = {
            "id": link_id,
            "source_type": payload["source_type"],
            "source_id": payload["source_id"],
            "relation": payload["relation"],
            "target_type": payload["target_type"],
            "target_id": payload["target_id"],
            "note": payload.get("note", ""),
            "created_at": now_iso(),
        }
        conn.execute(
            """
            INSERT INTO links (id, source_type, source_id, relation, target_type, target_id, note, created_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                row["id"],
                row["source_type"],
                row["source_id"],
                row["relation"],
                row["target_type"],
                row["target_id"],
                row["note"],
                row["created_at"],
            ),
        )
        conn.commit()
        return row
    finally:
        conn.close()


def delete_link(link_id: str) -> bool:
    conn = get_connection()
    try:
        cur = conn.execute("DELETE FROM links WHERE id = ?", (link_id,))
        conn.commit()
        return cur.rowcount > 0
    finally:
        conn.close()
