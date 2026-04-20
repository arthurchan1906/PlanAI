from __future__ import annotations

from datetime import datetime
from typing import Any, Dict, List

from .canon import fetch_canon
from .commits import list_commits
from .daily import get_daily_note
from .decisions import list_decisions
from .ideas import list_ideas
from .plans import list_plans
from .principles import list_active_principles
from .roadmaps import list_roadmaps
from .summary import get_inbox_summary, get_status_snapshot
from .tasks import list_tasks
from .visions import get_active_vision
from .docs import list_doc_records


def build_context_pack() -> Dict[str, Any]:
    canon = fetch_canon()
    vision = get_active_vision()
    roadmaps = _list_roadmaps_for_context()
    plans = list_plans()
    tasks = list_tasks()
    decisions = list_decisions()
    ideas = list_ideas()
    inbox = get_inbox_summary()
    daily = get_daily_note()
    all_docs = list_doc_records()

    active_roadmap = next(
        (item for item in roadmaps if item.get("status") in ("active", "planned")),
        roadmaps[0] if roadmaps else None,
    )
    active_plan = next((item for item in plans if item.get("status") == "active"), None)
    in_progress_tasks = [task for task in tasks if task["status"] == "in_progress"]
    active_task = in_progress_tasks[0] if in_progress_tasks else next(
        (task for task in tasks if task["status"] == "todo"),
        None,
    )
    accepted_decisions = [item for item in decisions if item["status"] == "accepted"][:5]
    
    # Filter active source of truth docs
    sot_docs = [
        {"path": doc["path"], "layer": doc["layer"]}
        for doc in all_docs
        if doc["status"] == "active" and doc["source_of_truth"]
    ]

    pending_questions = [item for item in decisions if item["status"] == "proposed"][:5]
    ready_ideas = [
        {
            "id": item["id"],
            "title": item["title"],
            "recommended_next_action": item.get("recommended_next_action", ""),
            "current_summary": item.get("current_summary", "") or item.get("summary", ""),
        }
        for item in ideas
        if item.get("recommended_next_action") in {"ready_for_decision", "ready_for_task"}
    ][:5]
    narrative = _build_context_narrative(
        vision=vision,
        canon=canon,
        active_roadmap=active_roadmap,
        active_plan=active_plan,
        active_task=active_task,
        pending_questions=pending_questions,
        ready_ideas=ready_ideas,
        daily=daily,
    )

    return {
        "project": {
            "vision": {
                "id": vision.get("id"),
                "title": vision.get("title"),
                "summary": vision.get("summary"),
            } if vision else None,
            "canon": {
                "product_goal": canon.get("product_goal", ""),
                "engineering_focus": canon.get("engineering_focus", ""),
                "architecture": canon.get("architecture", ""),
            },
            "source_of_truth_docs": sot_docs,
        },
        "mainline": {
            "roadmap": _roadmap_summary(active_roadmap),
            "plan": _plan_summary(active_plan),
            "task": _task_summary(active_task),
            "in_progress_tasks": in_progress_tasks[:5],
        },
        "constraints": {
            "accepted_decisions": [
                {
                    "id": item["id"],
                    "title": item["title"],
                    "date": item["date"],
                }
                for item in accepted_decisions
            ],
            "active_principles": [
                {
                    "id": item["id"],
                    "title": item["title"],
                    "kind": item["kind"],
                }
                for item in list_active_principles(limit=5)
            ],
        },
        "pending_questions": [
            {
                "id": item["id"],
                "title": item["title"],
                "status": item["status"],
            }
            for item in pending_questions
        ],
        "ready_ideas": ready_ideas,
        "risks": daily.get("risks", [])[:5],
        "recommended_actions": inbox.get("recommended_actions", [])[:5],
        "narrative": narrative,
    }


def build_next_action_packet() -> Dict[str, Any]:
    context = build_context_pack()
    mainline_task = context.get("mainline", {}).get("task")
    recommended_actions = context.get("recommended_actions", [])
    next_action = recommended_actions[0] if recommended_actions else None

    if not next_action and mainline_task:
        next_action = {
            "kind": "continue_task",
            "title": mainline_task["title"],
            "reason": "This is the current mainline task and there is no higher-priority governance blocker.",
            "command": f"aipmc task show --id {mainline_task['id']}",
        }

    return {
        "mainline": context.get("mainline"),
        "next_action": next_action,
        "backup_actions": recommended_actions[1:4] if next_action else recommended_actions[:3],
        "pending_questions": context.get("pending_questions", [])[:3],
    }


def build_handoff_packet() -> Dict[str, Any]:
    context = build_context_pack()
    daily = get_daily_note()
    commits = list_commits()[:5]

    return {
        "mainline": context.get("mainline"),
        "completed": daily.get("completed", [])[:5],
        "risks": daily.get("risks", [])[:5],
        "next": daily.get("next", [])[:5],
        "recent_commits": [
            {
                "id": item["id"],
                "title": item["title"],
                "status": item["status"],
                "review_status": item["review_status"],
                "task_id": item.get("task_id"),
            }
            for item in commits
        ],
        "recommended_actions": context.get("recommended_actions", [])[:5],
    }


def build_progress_packet() -> Dict[str, Any]:
    status = get_status_snapshot()
    next_packet = build_next_action_packet()
    inbox = status.get("inbox", {}).get("counts", {})
    mainline = next_packet.get("mainline", {})
    active_task = mainline.get("task") or {}
    active_plan = mainline.get("plan") or {}
    in_progress_tasks = status.get("tasks", {}).get("in_progress", [])
    recent_commits = status.get("recent_commits", {}).get("pmai", [])
    recommended_actions = status.get("inbox", {}).get("recommended_actions", [])
    git_status = status.get("git", {})

    quick_commands: List[str] = []
    if active_task.get("id"):
        quick_commands.append(f"aipmc task show --id {active_task['id']}")
    next_action = next_packet.get("next_action") or {}
    if next_action.get("command"):
        quick_commands.append(next_action["command"])
    quick_commands.extend(
        [
            "aipmc status",
            "aipmc next",
            "aipmc inbox",
        ]
    )

    return {
        "as_of": datetime.now().isoformat(timespec="seconds"),
        "health": {
            "inbox_total": inbox.get("total", 0),
            "git_dirty": bool(git_status.get("dirty")),
            "in_progress_count": len(in_progress_tasks),
            "has_next_action": bool(next_action),
        },
        "current_focus": {
            "roadmap": mainline.get("roadmap"),
            "plan": {
                "id": active_plan.get("id"),
                "title": active_plan.get("title"),
                "status": active_plan.get("status"),
                "next_manager_checkpoint": active_plan.get("next_manager_checkpoint", ""),
            }
            if active_plan
            else None,
            "task": active_task or None,
            "next_action": next_action or None,
        },
        "execution": {
            "in_progress_tasks": in_progress_tasks[:5],
            "recent_commits": recent_commits[:5],
        },
        "manager_review": {
            "counts": inbox,
            "recommended_actions": recommended_actions[:5],
        },
        "quick_commands": list(dict.fromkeys(quick_commands)),
    }


def _list_roadmaps_for_context() -> List[Dict[str, Any]]:
    try:
        return list_roadmaps()
    except Exception:
        return []


def _roadmap_summary(roadmap: Dict[str, Any] | None) -> Dict[str, Any] | None:
    if not roadmap:
        return None
    return {
        "id": roadmap.get("id"),
        "title": roadmap.get("title"),
        "status": roadmap.get("status"),
        "target_date": roadmap.get("target_date"),
    }


def _plan_summary(plan: Dict[str, Any] | None) -> Dict[str, Any] | None:
    if not plan:
        return None
    return {
        "id": plan.get("id"),
        "title": plan.get("title"),
        "goal": plan.get("goal"),
        "status": plan.get("status"),
        "next_manager_checkpoint": plan.get("manager_summary", {}).get("next_manager_checkpoint", ""),
    }


def _task_summary(task: Dict[str, Any] | None) -> Dict[str, Any] | None:
    if not task:
        return None
    return {
        "id": task.get("id"),
        "title": task.get("title"),
        "status": task.get("status"),
        "priority": task.get("priority"),
        "phase": task.get("phase"),
        "last_note": task.get("last_note", ""),
    }


def _build_context_narrative(
    *,
    vision: Dict[str, Any] | None,
    canon: Dict[str, Any],
    active_roadmap: Dict[str, Any] | None,
    active_plan: Dict[str, Any] | None,
    active_task: Dict[str, Any] | None,
    pending_questions: List[Dict[str, Any]],
    ready_ideas: List[Dict[str, Any]],
    daily: Dict[str, Any],
) -> Dict[str, str]:
    focus = active_task.get("title") if active_task else active_plan.get("title") if active_plan else active_roadmap.get("title") if active_roadmap else ""
    stage = active_roadmap.get("title") if active_roadmap else vision.get("title") if vision else "current project stage"
    why_now = (
        f"Current mainline is centered on {focus} under {stage}."
        if focus
        else f"Current mainline is centered on {stage}."
    )
    if canon.get("engineering_focus"):
        why_now += f" Engineering focus: {canon['engineering_focus']}."
    if daily.get("next"):
        why_now += f" Immediate next emphasis: {daily['next'][0]}."

    constraints = []
    if canon.get("product_goal"):
        constraints.append(f"Product goal: {canon['product_goal']}")
    if canon.get("architecture"):
        constraints.append(f"Architecture: {canon['architecture']}")
    constraints_summary = " | ".join(constraints[:2])

    governance = []
    if pending_questions:
        governance.append(f"{len(pending_questions)} proposed decisions still need review")
    if ready_ideas:
        governance.append(f"{len(ready_ideas)} ideas are ready to enter the mainline")
    if daily.get("risks"):
        governance.append(f"top risk: {daily['risks'][0]}")

    return {
        "project_focus": focus or "No active task is selected yet.",
        "why_now": why_now,
        "constraints_summary": constraints_summary or "No canon constraints captured yet.",
        "governance_focus": "; ".join(governance) if governance else "No urgent governance blockers right now.",
    }
