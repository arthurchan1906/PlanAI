from __future__ import annotations

import json
from datetime import datetime
from typing import Any, Dict, List, Optional

from .db import get_connection


def now_iso() -> str:
    return datetime.now().isoformat(timespec="seconds")


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


def get_daily_note(note_date: Optional[str] = None) -> Dict[str, Any]:
    target_date = note_date or today()
    conn = get_connection()
    try:
        row = conn.execute(
            "SELECT * FROM daily_notes WHERE note_date = ?",
            (target_date,),
        ).fetchone()
        if not row:
            return {
                "note_date": target_date,
                "completed": [],
                "problems": [],
                "risks": [],
                "next": [],
                "updated_at": None,
            }
        return {
            "note_date": row["note_date"],
            "completed": loads(row["completed_json"], []),
            "problems": loads(row["problems_json"], []),
            "risks": loads(row["risks_json"], []),
            "next": loads(row["next_json"], []),
            "updated_at": row["updated_at"],
        }
    finally:
        conn.close()


def list_daily_notes(limit: int = 30) -> List[Dict[str, Any]]:
    conn = get_connection()
    try:
        rows = conn.execute(
            "SELECT note_date, updated_at FROM daily_notes ORDER BY note_date DESC LIMIT ?",
            (limit,),
        ).fetchall()
        return [
            {"note_date": row["note_date"], "updated_at": row["updated_at"]}
            for row in rows
        ]
    finally:
        conn.close()


def append_daily_note(payload: Dict[str, Any], note_date: Optional[str] = None) -> Dict[str, Any]:
    target_date = note_date or today()
    current = get_daily_note(target_date)
    completed = current["completed"] + payload.get("completed", [])
    problems = current["problems"] + payload.get("problems", [])
    risks = current["risks"] + payload.get("risks", [])
    next_items = current["next"] + payload.get("next", [])
    conn = get_connection()
    try:
        conn.execute(
            """
            INSERT INTO daily_notes (
                note_date, completed_json, problems_json, risks_json, next_json, updated_at
            ) VALUES (?, ?, ?, ?, ?, ?)
            ON CONFLICT(note_date) DO UPDATE SET
                completed_json = excluded.completed_json,
                problems_json = excluded.problems_json,
                risks_json = excluded.risks_json,
                next_json = excluded.next_json,
                updated_at = excluded.updated_at
            """,
            (
                target_date,
                dumps(completed),
                dumps(problems),
                dumps(risks),
                dumps(next_items),
                now_iso(),
            ),
        )
        conn.commit()
        return get_daily_note(target_date)
    finally:
        conn.close()


def replace_daily_note(payload: Dict[str, Any], note_date: Optional[str] = None) -> Dict[str, Any]:
    target_date = note_date or today()
    conn = get_connection()
    try:
        conn.execute(
            """
            INSERT INTO daily_notes (
                note_date, completed_json, problems_json, risks_json, next_json, updated_at
            ) VALUES (?, ?, ?, ?, ?, ?)
            ON CONFLICT(note_date) DO UPDATE SET
                completed_json = excluded.completed_json,
                problems_json = excluded.problems_json,
                risks_json = excluded.risks_json,
                next_json = excluded.next_json,
                updated_at = excluded.updated_at
            """,
            (
                target_date,
                dumps(payload.get("completed", [])),
                dumps(payload.get("problems", [])),
                dumps(payload.get("risks", [])),
                dumps(payload.get("next", [])),
                now_iso(),
            ),
        )
        conn.commit()
        return get_daily_note(target_date)
    finally:
        conn.close()


def build_daily_summary_from_activity(
    *,
    include_commits: bool = False,
    include_tasks: bool = False,
    note_date: Optional[str] = None,
) -> Dict[str, List[str]]:
    target_date = note_date or today()
    payload: Dict[str, List[str]] = {
        "completed": [],
        "problems": [],
        "risks": [],
        "next": [],
    }

    if include_commits:
        from .commits import list_commits

        commits = list_commits(since=target_date)
        for commit in commits:
            title = commit["title"]
            status = commit["status"]
            review = commit["review_status"]
            tests = commit["test_status"]
            payload["completed"].append(f"Commit: {title} ({status}, review={review}, tests={tests})")
            if tests not in ("passed", "not_applicable"):
                payload["risks"].append(f"Verification pending for commit {commit['id']}: test_status={tests}")
            if review != "approved":
                payload["risks"].append(f"Review pending for commit {commit['id']}: review_status={review}")

    if include_tasks:
        from .tasks import list_tasks

        in_progress = list_tasks(status="in_progress")
        blocked = list_tasks(status="blocked")
        for task in in_progress[:5]:
            payload["next"].append(f"Continue task {task['id']}: {task['title']}")
        for task in blocked[:5]:
            payload["problems"].append(f"Blocked task {task['id']}: {task['title']}")

    return payload
