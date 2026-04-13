from __future__ import annotations

import json
from datetime import datetime
from typing import Any, Dict, List, Optional
from uuid import uuid4

from .db import get_connection
from .commits import list_commits
from .links import list_links_for_entity


def today() -> str:
    return datetime.now().strftime("%Y-%m-%d")


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


def list_decisions() -> List[Dict[str, Any]]:
    conn = get_connection()
    try:
        rows = conn.execute("SELECT * FROM decisions ORDER BY date DESC, id DESC").fetchall()
        return [
            {
                "id": row["id"],
                "title": row["title"],
                "date": row["date"],
                "status": row["status"],
                "background": row["background"],
                "decision": row["decision_text"],
                "impact": loads(row["impact_json"], []),
                "alternatives": loads(row["alternatives_json"], []),
                "related_tasks": loads(row["related_tasks_json"], []),
                "updates_canon": bool(row["updates_canon"]),
            }
            for row in rows
        ]
    finally:
        conn.close()


def get_decision(decision_id: str) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM decisions WHERE id = ?", (decision_id,)).fetchone()
        if not row:
            raise KeyError(decision_id)
        related_task_ids = loads(row["related_tasks_json"], [])
        related_tasks: List[Dict[str, Any]] = []
        for task_id in related_task_ids:
            task_row = conn.execute(
                "SELECT id, title, status, priority, phase FROM tasks WHERE id = ?",
                (task_id,),
            ).fetchone()
            if task_row:
                related_tasks.append(
                    {
                        "id": task_row["id"],
                        "title": task_row["title"],
                        "status": task_row["status"],
                        "priority": task_row["priority"],
                        "phase": task_row["phase"],
                    }
                )
        linked_commits = list_commits(decision_id=decision_id)
        return {
            "decision": {
                "id": row["id"],
                "title": row["title"],
                "date": row["date"],
                "status": row["status"],
                "background": row["background"],
                "decision": row["decision_text"],
                "impact": loads(row["impact_json"], []),
                "alternatives": loads(row["alternatives_json"], []),
                "related_tasks": related_task_ids,
                "updates_canon": bool(row["updates_canon"]),
            },
            "linked_tasks": related_tasks,
            "linked_commits": linked_commits,
            "links": list_links_for_entity(decision_id),
        }
    finally:
        conn.close()


def create_decision(payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        decision_id = slug("decision")
        row = {
            "id": decision_id,
            "title": payload["title"],
            "date": today(),
            "status": payload.get("status", "proposed"),
            "background": payload["background"],
            "decision": payload["decision"],
            "impact": payload.get("impact", []),
            "alternatives": payload.get("alternatives", []),
            "related_tasks": payload.get("related_tasks", []),
            "updates_canon": bool(payload.get("updates_canon", False)),
        }
        conn.execute(
            """
            INSERT INTO decisions (
                id, title, date, status, background, decision_text,
                impact_json, alternatives_json, related_tasks_json, updates_canon
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                row["id"],
                row["title"],
                row["date"],
                row["status"],
                row["background"],
                row["decision"],
                dumps(row["impact"]),
                dumps(row["alternatives"]),
                dumps(row["related_tasks"]),
                1 if row["updates_canon"] else 0,
            ),
        )
        conn.commit()
        return row
    finally:
        conn.close()


def update_decision_status(decision_id: str, status: str) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM decisions WHERE id = ?", (decision_id,)).fetchone()
        if not row:
            raise KeyError(decision_id)
        conn.execute("UPDATE decisions SET status = ? WHERE id = ?", (status, decision_id))
        conn.commit()
        updated = conn.execute("SELECT * FROM decisions WHERE id = ?", (decision_id,)).fetchone()
        return {
            "id": updated["id"],
            "title": updated["title"],
            "date": updated["date"],
            "status": updated["status"],
            "background": updated["background"],
            "decision": updated["decision_text"],
            "impact": loads(updated["impact_json"], []),
            "alternatives": loads(updated["alternatives_json"], []),
            "related_tasks": loads(updated["related_tasks_json"], []),
            "updates_canon": bool(updated["updates_canon"]),
        }
    finally:
        conn.close()
