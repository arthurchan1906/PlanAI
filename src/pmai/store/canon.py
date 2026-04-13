from __future__ import annotations

import json
from datetime import datetime
from typing import Any, Dict, List, Optional

from .db import get_connection


def today() -> str:
    return datetime.now().strftime("%Y-%m-%d")


def dumps(value: Any) -> str:
    return json.dumps(value, ensure_ascii=False)


def loads(value: Optional[str], default: Any) -> Any:
    if not value:
        return default
    try:
        return json.loads(value)
    except (json.JSONDecodeError, TypeError):
        return default


def empty_canon() -> Dict[str, Any]:
    return {
        "id": "canon-current",
        "updated_at": None,
        "product_goal": "",
        "engineering_focus": "",
        "architecture": "",
        "version_scope": [],
        "avoid_now": [],
        "top_tasks": [],
        "source_docs": [],
        "related_decisions": [],
    }


def fetch_canon() -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM canon WHERE id = 1").fetchone()
        if not row:
            return empty_canon()
        items = conn.execute(
            "SELECT item_type, position, value FROM canon_items ORDER BY item_type, position"
        ).fetchall()
        grouped: Dict[str, List[str]] = {}
        for item in items:
            grouped.setdefault(item["item_type"], []).append(item["value"])
        return {
            "id": "canon-current",
            "updated_at": row["updated_at"],
            "product_goal": row["product_goal"],
            "engineering_focus": row["engineering_focus"],
            "architecture": row["architecture"],
            "version_scope": grouped.get("version_scope", []),
            "avoid_now": grouped.get("avoid_now", []),
            "top_tasks": grouped.get("top_tasks", []),
            "source_docs": grouped.get("source_docs", []),
            "related_decisions": grouped.get("related_decisions", []),
        }
    finally:
        conn.close()


def update_canon(payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        decision = conn.execute(
            "SELECT * FROM decisions WHERE id = ? AND status = 'accepted'",
            (payload["decision_id"],),
        ).fetchone()
        if not decision:
            raise KeyError(payload["decision_id"])

        row = conn.execute("SELECT * FROM canon WHERE id = 1").fetchone()
        if not row:
            conn.execute(
                """
                INSERT INTO canon (id, updated_at, product_goal, engineering_focus, architecture)
                VALUES (1, ?, ?, ?, ?)
                """,
                (today(), "", "", ""),
            )
            conn.commit()

        canon = fetch_canon()
        version_scope = canon["version_scope"][:]
        avoid_now = canon["avoid_now"][:]
        related_decisions = canon["related_decisions"][:]

        for item in payload.get("add_scope", []):
            if item and item not in version_scope:
                version_scope.append(item)
        for item in payload.get("add_avoid", []):
            if item and item not in avoid_now:
                avoid_now.append(item)
        if payload["decision_id"] not in related_decisions:
            related_decisions.append(payload["decision_id"])

        conn.execute(
            """
            UPDATE canon
            SET updated_at = ?, product_goal = ?, engineering_focus = ?, architecture = ?
            WHERE id = 1
            """,
            (
                today(),
                payload.get("product_goal") or canon["product_goal"],
                payload.get("engineering_focus") or canon["engineering_focus"],
                payload.get("architecture") or canon["architecture"],
            ),
        )
        conn.execute("DELETE FROM canon_items WHERE item_type = 'version_scope'")
        conn.execute("DELETE FROM canon_items WHERE item_type = 'avoid_now'")
        conn.execute("DELETE FROM canon_items WHERE item_type = 'related_decisions'")
        for idx, value in enumerate(version_scope):
            conn.execute(
                "INSERT INTO canon_items (item_type, position, value) VALUES (?, ?, ?)",
                ("version_scope", idx, value),
            )
        for idx, value in enumerate(avoid_now):
            conn.execute(
                "INSERT INTO canon_items (item_type, position, value) VALUES (?, ?, ?)",
                ("avoid_now", idx, value),
            )
        for idx, value in enumerate(related_decisions):
            conn.execute(
                "INSERT INTO canon_items (item_type, position, value) VALUES (?, ?, ?)",
                ("related_decisions", idx, value),
            )
        conn.commit()
        return fetch_canon()
    finally:
        conn.close()
