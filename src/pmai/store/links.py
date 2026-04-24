from __future__ import annotations

from datetime import datetime
from typing import Any, Dict, List, Optional
from uuid import uuid4

from .db import get_connection
from .docs import normalize_doc_path

SUPPORTED_ENTITY_TYPES = {
    "commit": "commits",
    "decision": "decisions",
    "doc": "doc_records",
    "idea": "ideas",
    "plan": "plans",
    "principle": "principles",
    "roadmap": "roadmap",
    "task": "tasks",
    "vision": "visions",
}


def now_iso() -> str:
    return datetime.now().isoformat(timespec="seconds")


def slug(prefix: str) -> str:
    stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    return f"{prefix}-{stamp}-{uuid4().hex[:6]}"


def _normalize_entity_ref(entity_type: str, entity_id: str) -> str:
    if entity_type not in SUPPORTED_ENTITY_TYPES:
        raise ValueError(f"unsupported entity type: {entity_type}")
    value = str(entity_id or "").strip()
    if not value:
        raise ValueError(f"{entity_type} id is required")
    if entity_type == "doc":
        return normalize_doc_path(value)
    return value


def _entity_exists(conn, entity_type: str, entity_id: str) -> bool:
    table_name = SUPPORTED_ENTITY_TYPES[entity_type]
    if entity_type == "doc":
        aliases = [entity_id]
        windows_variant = entity_id.replace("/", "\\")
        if windows_variant != entity_id:
            aliases.append(windows_variant)
        placeholders = ", ".join("?" for _ in aliases)
        row = conn.execute(
            f"SELECT path FROM {table_name} WHERE path IN ({placeholders}) LIMIT 1",
            aliases,
        ).fetchone()
        return row is not None
    row = conn.execute(f"SELECT id FROM {table_name} WHERE id = ? LIMIT 1", (entity_id,)).fetchone()
    return row is not None


def _normalize_link_payload(payload: Dict[str, Any]) -> Dict[str, Any]:
    source_type = str(payload["source_type"] or "").strip()
    target_type = str(payload["target_type"] or "").strip()
    source_id = _normalize_entity_ref(source_type, payload["source_id"])
    target_id = _normalize_entity_ref(target_type, payload["target_id"])
    relation = str(payload.get("relation") or "").strip()
    if source_type == target_type and source_id == target_id and relation != "references":
        raise ValueError("self-links are not allowed for this relation")
    return {
        "source_type": source_type,
        "source_id": source_id,
        "relation": relation,
        "target_type": target_type,
        "target_id": target_id,
        "note": payload.get("note", ""),
    }


def _serialize_link(row: Any) -> Dict[str, Any]:
    source_id = row["source_id"]
    target_id = row["target_id"]
    if row["source_type"] == "doc":
        source_id = normalize_doc_path(source_id)
    if row["target_type"] == "doc":
        target_id = normalize_doc_path(target_id)
    return {
        "id": row["id"],
        "source_type": row["source_type"],
        "source_id": source_id,
        "relation": row["relation"],
        "target_type": row["target_type"],
        "target_id": target_id,
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


def _fetch_entity_title(conn, entity_type: str, entity_id: str) -> str:
    if entity_type == "doc":
        return entity_id
    table_name = SUPPORTED_ENTITY_TYPES.get(entity_type)
    if not table_name:
        return ""
    try:
        # Most tables have 'title', some might have 'name' or just 'id'
        row = conn.execute(f"SELECT * FROM {table_name} WHERE id = ? LIMIT 1", (entity_id,)).fetchone()
        if not row:
            return ""
        for col in ["title", "name", "content", "summary"]:
            if col in row.keys() and row[col]:
                val = str(row[col])
                return (val[:50] + "...") if len(val) > 53 else val
        return entity_id
    except Exception:
        return entity_id


def list_links_for_entity(entity_id: str) -> Dict[str, List[Dict[str, Any]]]:
    conn = get_connection()
    try:
        outgoing_rows = conn.execute(
            "SELECT * FROM links WHERE source_id = ? ORDER BY created_at DESC, id DESC",
            (entity_id,),
        ).fetchall()
        incoming_rows = conn.execute(
            "SELECT * FROM links WHERE target_id = ? ORDER BY created_at DESC, id DESC",
            (entity_id,),
        ).fetchall()
        
        outgoing = []
        for row in outgoing_rows:
            link = _serialize_link(row)
            link["target_title"] = _fetch_entity_title(conn, row["target_type"], row["target_id"])
            outgoing.append(link)
            
        incoming = []
        for row in incoming_rows:
            link = _serialize_link(row)
            link["source_title"] = _fetch_entity_title(conn, row["source_type"], row["source_id"])
            incoming.append(link)

        return {
            "outgoing": outgoing,
            "incoming": incoming,
        }
    finally:
        conn.close()


def create_link(payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        normalized = _normalize_link_payload(payload)
        if not normalized["relation"]:
            raise ValueError("relation is required")
        if not _entity_exists(conn, normalized["source_type"], normalized["source_id"]):
            raise KeyError(f"missing {normalized['source_type']}: {normalized['source_id']}")
        if not _entity_exists(conn, normalized["target_type"], normalized["target_id"]):
            raise KeyError(f"missing {normalized['target_type']}: {normalized['target_id']}")
        duplicate = conn.execute(
            """
            SELECT id FROM links
            WHERE source_type = ? AND source_id = ? AND relation = ? AND target_type = ? AND target_id = ?
            LIMIT 1
            """,
            (
                normalized["source_type"],
                normalized["source_id"],
                normalized["relation"],
                normalized["target_type"],
                normalized["target_id"],
            ),
        ).fetchone()
        if duplicate:
            raise ValueError("duplicate link already exists")
        row = {
            "id": slug("link"),
            **normalized,
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
