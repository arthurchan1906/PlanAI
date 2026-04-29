from __future__ import annotations

import json
from typing import Any, List, Optional


def _loads(value: Optional[str], default: Any) -> Any:
    if not value:
        return default
    try:
        return json.loads(value)
    except (json.JSONDecodeError, TypeError):
        return default


def _dumps(value: Any) -> str:
    return json.dumps(value, ensure_ascii=False)


def _normalize_task_ids(task_ids: List[str]) -> List[str]:
    normalized: List[str] = []
    for task_id in task_ids:
        candidate = str(task_id or "").strip()
        if candidate and candidate not in normalized:
            normalized.append(candidate)
    return normalized


def _ensure_task_exists(conn, task_id: str) -> Any:
    row = conn.execute("SELECT id, plan_id, roadmap_id FROM tasks WHERE id = ?", (task_id,)).fetchone()
    if not row:
        raise KeyError(task_id)
    return row


def _ensure_plan_exists(conn, plan_id: str) -> Any:
    row = conn.execute("SELECT id, roadmap_id, task_ids_json FROM plans WHERE id = ?", (plan_id,)).fetchone()
    if not row:
        raise KeyError(plan_id)
    return row


def _ensure_roadmap_exists(conn, roadmap_id: str) -> Any:
    row = conn.execute("SELECT id FROM roadmap WHERE id = ?", (roadmap_id,)).fetchone()
    if not row:
        raise KeyError(roadmap_id)
    return row


def _write_plan_task_ids(conn, plan_id: str, task_ids: List[str]) -> None:
    conn.execute(
        "UPDATE plans SET task_ids_json = ? WHERE id = ?",
        (_dumps(_normalize_task_ids(task_ids)), plan_id),
    )


def _add_task_to_plan(conn, plan_id: str, task_id: str) -> None:
    plan_row = _ensure_plan_exists(conn, plan_id)
    task_ids = _normalize_task_ids(_loads(plan_row["task_ids_json"], []))
    if task_id not in task_ids:
        task_ids.append(task_id)
        _write_plan_task_ids(conn, plan_id, task_ids)


def _remove_task_from_plan(conn, plan_id: str, task_id: str) -> None:
    plan_row = _ensure_plan_exists(conn, plan_id)
    task_ids = [item for item in _normalize_task_ids(_loads(plan_row["task_ids_json"], [])) if item != task_id]
    _write_plan_task_ids(conn, plan_id, task_ids)


def sync_task_membership(
    conn,
    *,
    task_id: str,
    plan_id: Optional[str] = None,
    roadmap_id: Optional[str] = None,
) -> None:
    task_row = _ensure_task_exists(conn, task_id)
    current_plan_id = task_row["plan_id"]
    effective_plan_id = current_plan_id if plan_id is None else plan_id
    effective_roadmap_id = task_row["roadmap_id"] if roadmap_id is None else roadmap_id

    if effective_plan_id:
        plan_row = _ensure_plan_exists(conn, effective_plan_id)
        if plan_row["roadmap_id"]:
            effective_roadmap_id = plan_row["roadmap_id"]
        elif roadmap_id is None:
            effective_roadmap_id = plan_row["roadmap_id"]
    if effective_roadmap_id:
        _ensure_roadmap_exists(conn, effective_roadmap_id)

    if current_plan_id and current_plan_id != effective_plan_id:
        _remove_task_from_plan(conn, current_plan_id, task_id)
    if effective_plan_id:
        _add_task_to_plan(conn, effective_plan_id, task_id)

    conn.execute(
        "UPDATE tasks SET plan_id = ?, roadmap_id = ? WHERE id = ?",
        (effective_plan_id, effective_roadmap_id, task_id),
    )


def sync_plan_task_ids(
    conn,
    *,
    plan_id: str,
    task_ids: List[str],
    roadmap_id: Optional[str],
) -> None:
    plan_row = _ensure_plan_exists(conn, plan_id)
    current_task_ids = _normalize_task_ids(_loads(plan_row["task_ids_json"], []))
    back_linked_task_ids = [
        row["id"]
        for row in conn.execute("SELECT id FROM tasks WHERE plan_id = ?", (plan_id,)).fetchall()
    ]
    for task_id in back_linked_task_ids:
        if task_id not in current_task_ids:
            current_task_ids.append(task_id)
    next_task_ids = _normalize_task_ids(task_ids)

    for task_id in next_task_ids:
        _ensure_task_exists(conn, task_id)

    removed = [item for item in current_task_ids if item not in next_task_ids]
    added = [item for item in next_task_ids if item not in current_task_ids]

    for task_id in removed:
        task_row = _ensure_task_exists(conn, task_id)
        if task_row["plan_id"] == plan_id:
            conn.execute("UPDATE tasks SET plan_id = NULL WHERE id = ?", (task_id,))

    for task_id in added:
        sync_task_membership(conn, task_id=task_id, plan_id=plan_id, roadmap_id=roadmap_id)

    _write_plan_task_ids(conn, plan_id, next_task_ids)
