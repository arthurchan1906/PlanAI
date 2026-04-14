from __future__ import annotations

import json
from datetime import datetime
from typing import Any, Dict, List, Optional
from uuid import uuid4

from .db import get_connection
from .commits import list_commits
from .links import list_links_for_entity


def now_iso() -> str:
    return datetime.now().isoformat(timespec="seconds")


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


def list_tasks(
    status: Optional[str] = None,
    roadmap_id: Optional[str] = None,
    plan_id: Optional[str] = None,
) -> List[Dict[str, Any]]:
    conn = get_connection()
    try:
        query = "SELECT * FROM tasks"
        params: List[Any] = []
        where_clauses = []
        if status:
            where_clauses.append("status = ?")
            params.append(status)
        if roadmap_id:
            where_clauses.append("roadmap_id = ?")
            params.append(roadmap_id)
        if plan_id:
            where_clauses.append("plan_id = ?")
            params.append(plan_id)

        if where_clauses:
            query += " WHERE " + " AND ".join(where_clauses)

        query += """
            ORDER BY CASE status
                WHEN 'in_progress' THEN 0
                WHEN 'todo' THEN 1
                WHEN 'blocked' THEN 2
                ELSE 3
            END, priority, updated_at DESC
            """
        rows = conn.execute(query, params).fetchall()
        tasks = []
        for row in rows:
            acceptance = loads(row["acceptance_json"], [])
            structured_acceptance = []
            done_count = 0
            for item in acceptance:
                if isinstance(item, str):
                    structured_acceptance.append({"text": item, "done": False})
                else:
                    structured_acceptance.append(item)
                    if item.get("done"):
                        done_count += 1

            progress = 0
            if structured_acceptance:
                progress = int((done_count / len(structured_acceptance)) * 100)
            elif row["status"] == "done":
                progress = 100

            tasks.append({
                "id": row["id"],
                "title": row["title"],
                "status": row["status"],
                "priority": row["priority"],
                "phase": row["phase"],
                "roadmap_id": row["roadmap_id"],
                "plan_id": row["plan_id"],
                "acceptance": structured_acceptance,
                "progress": progress,
                "related_docs": loads(row["related_docs_json"], []),
                "related_decisions": loads(row["related_decisions_json"], []),
                "last_note": row["last_note"] or "",
                "updated_at": row["updated_at"],
            })
        return tasks
    finally:
        conn.close()


def get_task(task_id: str) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM tasks WHERE id = ?", (task_id,)).fetchone()
        if not row:
            raise KeyError(task_id)

        acceptance = loads(row["acceptance_json"], [])
        structured_acceptance = []
        done_count = 0
        for item in acceptance:
            if isinstance(item, str):
                structured_acceptance.append({"text": item, "done": False})
            else:
                structured_acceptance.append(item)
                if item.get("done"):
                    done_count += 1

        progress = 0
        if structured_acceptance:
            progress = int((done_count / len(structured_acceptance)) * 100)
        elif row["status"] == "done":
            progress = 100

        linked_commits = list_commits(task_id=task_id)
        approved_commits = [
            item
            for item in linked_commits
            if item["status"] in ("committed", "merged") and item["review_status"] == "approved"
        ]
        verified_approved_commits = [
            item
            for item in approved_commits
            if item["test_status"] == "passed"
        ]
        blocker_reasons: List[str] = []
        if not linked_commits:
            blocker_reasons.append("no_linked_commit")
        if linked_commits and not approved_commits:
            blocker_reasons.append("no_approved_commit")
        if linked_commits and not verified_approved_commits:
            blocker_reasons.append("verification_incomplete")
        changed_files: List[str] = []
        for commit in linked_commits:
            for file_path in commit.get("files", []):
                if file_path not in changed_files:
                    changed_files.append(file_path)
        return {
            "task": {
                "id": row["id"],
                "title": row["title"],
                "status": row["status"],
                "priority": row["priority"],
                "phase": row["phase"],
                "roadmap_id": row["roadmap_id"],
                "plan_id": row["plan_id"],
                "acceptance": structured_acceptance,
                "progress": progress,
                "related_docs": loads(row["related_docs_json"], []),
                "related_decisions": loads(row["related_decisions_json"], []),
                "last_note": row["last_note"] or "",
                "updated_at": row["updated_at"],
            },
            "linked_commits": linked_commits,
            "links": list_links_for_entity(task_id),
            "changed_files": changed_files,
            "closure": {
                "linked_commit_count": len(linked_commits),
                "approved_commit_count": len(approved_commits),
                "verified_approved_commit_count": len(verified_approved_commits),
                "can_mark_done": bool(verified_approved_commits),
                "blocker_reasons": blocker_reasons,
            },
        }
    finally:
        conn.close()


def create_task(payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        task_id = slug("task")
        conn.execute(
            """
            INSERT INTO tasks (
                id, title, status, priority, phase, roadmap_id, plan_id,
                acceptance_json, related_docs_json, related_decisions_json, last_note, updated_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                task_id,
                payload["title"],
                payload.get("status", "todo"),
                payload.get("priority", "P1"),
                payload.get("phase", "general"),
                payload.get("roadmap_id"),
                payload.get("plan_id"),
                dumps(payload.get("acceptance", [])),
                dumps([]),
                dumps([]),
                "",
                today(),
            ),
        )
        conn.commit()
        return get_task(task_id)["task"]
    finally:
        conn.close()


def update_task(
    task_id: str,
    status: str,
    note: str = "",
    allow_without_commit: bool = False,
    roadmap_id: Optional[str] = None,
    plan_id: Optional[str] = None,
) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM tasks WHERE id = ?", (task_id,)).fetchone()
        if not row:
            raise KeyError(task_id)
        if status == "done" and row["status"] != "done" and not allow_without_commit:
            ready_commit = conn.execute(
                """
                SELECT id
                FROM commits
                WHERE task_id = ?
                  AND status IN ('committed', 'merged')
                  AND review_status = 'approved'
                  AND test_status = 'passed'
                ORDER BY updated_at DESC, created_at DESC, id DESC
                LIMIT 1
                """,
                (task_id,),
            ).fetchone()
            if not ready_commit:
                raise ValueError(
                    "task cannot be marked done without at least one verified approved commit "
                    "(status=committed|merged, review_status=approved, test_status=passed) linked by --task-id"
                )

        updates: List[Any] = [status, note, today()]
        set_clauses = ["status = ?", "last_note = ?", "updated_at = ?"]

        if roadmap_id is not None:
            set_clauses.append("roadmap_id = ?")
            updates.append(roadmap_id)
        if plan_id is not None:
            set_clauses.append("plan_id = ?")
            updates.append(plan_id)

        updates.append(task_id)
        conn.execute(
            f"UPDATE tasks SET {', '.join(set_clauses)} WHERE id = ?",
            updates,
        )
        conn.commit()
        return get_task(task_id)["task"]
    finally:
        conn.close()


def plan_task(task_id: str, steps: List[str]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM tasks WHERE id = ?", (task_id,)).fetchone()
        if not row:
            raise KeyError(task_id)

        acceptance = [{"text": step, "done": False} for step in steps]
        conn.execute(
            "UPDATE tasks SET acceptance_json = ?, updated_at = ? WHERE id = ?",
            (dumps(acceptance), today(), task_id),
        )
        conn.commit()
        return get_task(task_id)["task"]
    finally:
        conn.close()


def update_task_checkpoint(task_id: str, index: int, done: bool) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM tasks WHERE id = ?", (task_id,)).fetchone()
        if not row:
            raise KeyError(task_id)

        acceptance = loads(row["acceptance_json"], [])
        structured = []
        for i, item in enumerate(acceptance):
            if isinstance(item, str):
                structured.append({"text": item, "done": False})
            else:
                structured.append(item)

        if 0 <= index < len(structured):
            structured[index]["done"] = done
        else:
            raise IndexError(f"Checkpoint index {index} out of range (0-{len(structured)-1})")

        conn.execute(
            "UPDATE tasks SET acceptance_json = ?, updated_at = ? WHERE id = ?",
            (dumps(structured), today(), task_id),
        )
        conn.commit()
        return get_task(task_id)["task"]
    finally:
        conn.close()


def get_module_progress() -> List[Dict[str, Any]]:
    tasks = list_tasks()
    modules: Dict[str, Dict[str, Any]] = {}

    for t in tasks:
        mod = t["phase"] or "general"
        if mod not in modules:
            modules[mod] = {"name": mod, "total_tasks": 0, "done_tasks": 0, "total_progress": 0}

        modules[mod]["total_tasks"] += 1
        if t["status"] == "done":
            modules[mod]["done_tasks"] += 1
        modules[mod]["total_progress"] += t["progress"]

    result = []
    for mod_data in modules.values():
        mod_data["progress"] = int(mod_data["total_progress"] / mod_data["total_tasks"])
        result.append(mod_data)

    return sorted(result, key=lambda x: x["progress"], reverse=True)
