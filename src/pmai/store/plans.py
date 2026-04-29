from __future__ import annotations

from datetime import datetime
from typing import Any, Dict, List, Optional
from uuid import uuid4

from .canon import fetch_canon
from .decisions import list_decisions
from .db import get_connection
from .plan_runtime import enrich_plan, extract_id_from_command
from .principles import list_active_principles
from .relationship_sync import sync_plan_task_ids
from .roadmaps import get_roadmap, list_roadmaps
from .tasks import create_task, list_tasks
from .visions import get_active_vision, get_vision


def now_iso() -> str:
    return datetime.now().isoformat(timespec="seconds")


def slug(prefix: str) -> str:
    stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    return f"{prefix}-{stamp}-{uuid4().hex[:6]}"


def _dumps(value: Any) -> str:
    import json

    return json.dumps(value, ensure_ascii=False)


def _loads(value: Optional[str], default: Any) -> Any:
    import json

    if not value:
        return default
    try:
        return json.loads(value)
    except (json.JSONDecodeError, TypeError):
        return default


def list_plans(roadmap_id: Optional[str] = None, status: Optional[str] = None) -> List[Dict[str, Any]]:
    conn = get_connection()
    try:
        query = "SELECT * FROM plans"
        params: List[Any] = []
        clauses: List[str] = []
        if roadmap_id:
            clauses.append("roadmap_id = ?")
            params.append(roadmap_id)
        if status:
            clauses.append("status = ?")
            params.append(status)
        if clauses:
            query += " WHERE " + " AND ".join(clauses)
        query += " ORDER BY CASE status WHEN 'active' THEN 0 WHEN 'draft' THEN 1 ELSE 2 END, priority, updated_at DESC"
        rows = conn.execute(query, params).fetchall()
        return [enrich_plan(_row_to_plan(row)) for row in rows]
    finally:
        conn.close()


def get_plan(plan_id: str) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM plans WHERE id = ?", (plan_id,)).fetchone()
        if not row:
            raise KeyError(plan_id)
        return enrich_plan(_row_to_plan(row))
    finally:
        conn.close()


def create_plan(payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        plan_id = slug("plan")
        now = now_iso()
        roadmap_id = payload.get("roadmap_id")
        task_ids = payload.get("task_ids", [])
        conn.execute(
            """
            INSERT INTO plans (
                id, roadmap_id, vision_id, title, goal, status, priority,
                scope_json, risks_json, assumptions_json, task_ids_json,
                source, created_at, updated_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                plan_id,
                roadmap_id,
                payload.get("vision_id"),
                payload["title"],
                payload.get("goal", ""),
                payload.get("status", "draft"),
                payload.get("priority", "P1"),
                _dumps(payload.get("scope", [])),
                _dumps(payload.get("risks", [])),
                _dumps(payload.get("assumptions", [])),
                _dumps(task_ids),
                payload.get("source", "manual"),
                now,
                now,
            ),
        )
        if task_ids:
            sync_plan_task_ids(conn, plan_id=plan_id, task_ids=task_ids, roadmap_id=roadmap_id)
        conn.commit()
        return get_plan(plan_id)
    finally:
        conn.close()


def update_plan(plan_id: str, payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM plans WHERE id = ?", (plan_id,)).fetchone()
        if not row:
            raise KeyError(plan_id)

        scalar_fields = ["roadmap_id", "vision_id", "title", "goal", "status", "priority", "source"]
        json_fields = {
            "scope": "scope_json",
            "risks": "risks_json",
            "assumptions": "assumptions_json",
            "task_ids": "task_ids_json",
        }
        updates: List[str] = []
        params: List[Any] = []
        for field in scalar_fields:
            if field in payload:
                updates.append(f"{field} = ?")
                params.append(payload[field])
        for field, column in json_fields.items():
            if field in payload:
                updates.append(f"{column} = ?")
                params.append(_dumps(payload[field]))
        if updates:
            updates.append("updated_at = ?")
            params.append(now_iso())
            params.append(plan_id)
            conn.execute(f"UPDATE plans SET {', '.join(updates)} WHERE id = ?", params)
            if "task_ids" in payload or "roadmap_id" in payload:
                refreshed_row = conn.execute(
                    "SELECT roadmap_id, task_ids_json FROM plans WHERE id = ?",
                    (plan_id,),
                ).fetchone()
                sync_plan_task_ids(
                    conn,
                    plan_id=plan_id,
                    task_ids=_loads(refreshed_row["task_ids_json"], []),
                    roadmap_id=refreshed_row["roadmap_id"],
                )
            conn.commit()
        return get_plan(plan_id)
    finally:
        conn.close()


def advance_plan(plan_id: str) -> Dict[str, Any]:
    plan = get_plan(plan_id)
    recommendations = plan.get("recommendations", [])
    if not recommendations:
        return {
            "ok": False,
            "plan": plan,
            "detail": "No automatic advance action is available for this plan.",
        }

    action = recommendations[0]
    result: Dict[str, Any] | None = None

    if action["kind"] == "start_task":
        task_id = extract_id_from_command(action["command"], "--id")
        if not task_id:
            raise ValueError(f"Cannot parse task id from command: {action['command']}")
        from .tasks import update_task

        result = update_task(task_id, "in_progress")
    elif action["kind"] == "close_plan":
        result = update_plan(plan_id, {"status": "completed"})
    else:
        return {
            "ok": False,
            "plan": plan,
            "detail": f"Automatic advance is not implemented for action kind `{action['kind']}` yet.",
            "recommended_action": action,
        }

    refreshed = get_plan(plan_id)
    return {
        "ok": True,
        "applied_action": action,
        "result": result,
        "plan": refreshed,
    }


def generate_plan(
    *,
    roadmap_id: Optional[str] = None,
    vision_id: Optional[str] = None,
    title: str = "",
    create_tasks_for_plan: bool = False,
    task_limit: int = 4,
) -> Dict[str, Any]:
    roadmap = get_roadmap(roadmap_id) if roadmap_id else None
    vision = _pick_vision(vision_id, roadmap)
    canon = fetch_canon()
    principles = list_active_principles(limit=6)
    decisions = [item for item in list_decisions() if item["status"] == "accepted"][:5]
    existing_tasks = list_tasks(roadmap_id=roadmap_id) if roadmap_id else list_tasks()

    focus = title or (roadmap["title"] if roadmap else vision.get("title") if vision else "AI planning workstream")
    scope = _build_scope(roadmap, vision, canon)
    risks = _build_risks(existing_tasks, decisions)
    assumptions = _build_assumptions(principles, canon)
    suggestions = _build_task_suggestions(
        focus=focus,
        roadmap=roadmap,
        vision=vision,
        canon=canon,
        existing_tasks=existing_tasks,
        decisions=decisions,
        limit=task_limit,
    )

    created_task_ids: List[str] = []
    created_tasks: List[Dict[str, Any]] = []

    plan_id = slug("plan")
    plan = _create_plan_with_id(
        plan_id,
        {
            "roadmap_id": roadmap_id,
            "vision_id": vision.get("id") if vision else None,
            "title": f"{focus} plan",
            "goal": _build_goal(focus, roadmap, vision, canon),
            "status": "active" if create_tasks_for_plan else "draft",
            "priority": roadmap["priority"] if roadmap else "P1",
            "scope": scope,
            "risks": risks,
            "assumptions": assumptions,
            "task_ids": [],
            "source": "generated",
        }
    )

    if create_tasks_for_plan:
        for suggestion in suggestions:
            task = create_task(
                {
                    "title": suggestion["title"],
                    "priority": suggestion["priority"],
                    "status": "todo",
                    "phase": suggestion["phase"],
                    "roadmap_id": roadmap_id,
                    "plan_id": plan_id,
                    "acceptance": suggestion["acceptance"],
                }
            )
            created_task_ids.append(task["id"])
            created_tasks.append(task)
        plan = update_plan(plan_id, {"task_ids": created_task_ids})

    return {
        "plan": enrich_plan(plan),
        "task_suggestions": suggestions,
        "created_tasks": created_tasks,
        "context": {
            "roadmap": roadmap,
            "vision": vision,
            "accepted_decisions": decisions,
        },
    }


def _create_plan_with_id(plan_id: str, payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        now = now_iso()
        conn.execute(
            """
            INSERT INTO plans (
                id, roadmap_id, vision_id, title, goal, status, priority,
                scope_json, risks_json, assumptions_json, task_ids_json,
                source, created_at, updated_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                plan_id,
                payload.get("roadmap_id"),
                payload.get("vision_id"),
                payload["title"],
                payload.get("goal", ""),
                payload.get("status", "draft"),
                payload.get("priority", "P1"),
                _dumps(payload.get("scope", [])),
                _dumps(payload.get("risks", [])),
                _dumps(payload.get("assumptions", [])),
                _dumps(payload.get("task_ids", [])),
                payload.get("source", "manual"),
                now,
                now,
            ),
        )
        conn.commit()
        return get_plan(plan_id)
    finally:
        conn.close()


def _pick_vision(vision_id: Optional[str], roadmap: Optional[Dict[str, Any]]) -> Dict[str, Any]:
    target_id = vision_id or (roadmap.get("vision_id") if roadmap else None)
    if target_id:
        return get_vision(target_id)
    try:
        return get_active_vision()
    except KeyError:
        roadmaps = list_roadmaps()
        return {"id": "", "title": roadmaps[0]["title"] if roadmaps else "", "summary": "", "status": "draft", "horizon": ""}


def _build_goal(
    focus: str,
    roadmap: Optional[Dict[str, Any]],
    vision: Dict[str, Any],
    canon: Dict[str, Any],
) -> str:
    segments = [f"Deliver {focus} with a manager-readable execution plan."]
    if roadmap:
        segments.append(f"Align with roadmap milestone `{roadmap['title']}`.")
    if vision and vision.get("title"):
        segments.append(f"Support active vision `{vision['title']}`.")
    if canon.get("engineering_focus"):
        segments.append(f"Keep engineering focus on {canon['engineering_focus']}.")
    return " ".join(segments)


def _build_scope(
    roadmap: Optional[Dict[str, Any]],
    vision: Dict[str, Any],
    canon: Dict[str, Any],
) -> List[str]:
    scope: List[str] = []
    if roadmap:
        scope.append(f"Roadmap milestone: {roadmap['title']}")
    if vision and vision.get("summary"):
        scope.append(f"Vision anchor: {vision['summary']}")
    if canon.get("architecture"):
        scope.append(f"Architecture boundary: {canon['architecture']}")
    for item in canon.get("version_scope", [])[:3]:
        scope.append(f"Version scope: {item}")
    return scope[:6]


def _build_risks(existing_tasks: List[Dict[str, Any]], decisions: List[Dict[str, Any]]) -> List[str]:
    risks: List[str] = []
    if any(task["status"] == "blocked" for task in existing_tasks):
        risks.append("There are already blocked tasks in this area; sequencing needs explicit cleanup.")
    if len(existing_tasks) > 6:
        risks.append("Workstream already has many tasks; adding more without consolidation may create manager noise.")
    if not decisions:
        risks.append("No accepted decisions found; generated plan may need human boundary confirmation.")
    return risks[:4]


def _build_assumptions(principles: List[Dict[str, Any]], canon: Dict[str, Any]) -> List[str]:
    assumptions = [f"Follow principle: {item['title']}" for item in principles[:3]]
    if canon.get("product_goal"):
        assumptions.append(f"Target product goal remains: {canon['product_goal']}")
    return assumptions[:5]


def _build_task_suggestions(
    *,
    focus: str,
    roadmap: Optional[Dict[str, Any]],
    vision: Dict[str, Any],
    canon: Dict[str, Any],
    existing_tasks: List[Dict[str, Any]],
    decisions: List[Dict[str, Any]],
    limit: int,
) -> List[Dict[str, Any]]:
    phase = _suggest_phase(focus, roadmap, existing_tasks)
    base = [
        {
            "title": f"Clarify scope for {focus}",
            "phase": "foundation",
            "priority": "P1",
            "acceptance": [
                "Scope boundaries are written down",
                "Dependencies and non-goals are explicit",
                "Plan owner can explain why this work matters now",
            ],
        },
        {
            "title": f"Implement core path for {focus}",
            "phase": phase,
            "priority": roadmap["priority"] if roadmap else "P1",
            "acceptance": [
                "Primary workflow is executable end-to-end",
                "Critical behavior is visible in UI or CLI",
                "Open blockers are captured as explicit follow-up tasks",
            ],
        },
        {
            "title": f"Verify and govern {focus}",
            "phase": "polish",
            "priority": "P2",
            "acceptance": [
                "Validation or smoke checks are documented",
                "Docs or canon follow-ups are identified",
                "Task can be closed without manager guesswork",
            ],
        },
    ]
    if vision and vision.get("title"):
        base[0]["acceptance"].append(f"Vision alignment references `{vision['title']}`")
    if canon.get("engineering_focus"):
        base[1]["acceptance"].append(f"Implementation respects `{canon['engineering_focus']}`")
    if decisions:
        base.insert(
            1,
            {
                "title": f"Resolve decision coupling for {focus}",
                "phase": "general",
                "priority": "P1",
                "acceptance": [
                    "Accepted decisions impacting this plan are listed",
                    "Missing decisions are called out explicitly",
                    "Execution can proceed without hidden policy gaps",
                ],
            },
        )
    return base[: max(1, limit)]


def _suggest_phase(focus: str, roadmap: Optional[Dict[str, Any]], existing_tasks: List[Dict[str, Any]]) -> str:
    focus_lower = focus.lower()
    if "docs" in focus_lower or "plan" in focus_lower:
        return "foundation"
    if any(task["phase"] == "implementation" for task in existing_tasks):
        return "implementation"
    if roadmap and roadmap["status"] == "active":
        return "implementation"
    return "general"


def _row_to_plan(row: Any) -> Dict[str, Any]:
    return {
        "id": row["id"],
        "roadmap_id": row["roadmap_id"],
        "vision_id": row["vision_id"],
        "title": row["title"],
        "goal": row["goal"],
        "status": row["status"],
        "priority": row["priority"],
        "scope": _loads(row["scope_json"], []),
        "risks": _loads(row["risks_json"], []),
        "assumptions": _loads(row["assumptions_json"], []),
        "task_ids": _loads(row["task_ids_json"], []),
        "source": row["source"],
        "created_at": row["created_at"],
        "updated_at": row["updated_at"],
    }
