from __future__ import annotations

from typing import Any, Dict, List, Optional

from .commits import list_commits
from .tasks import list_tasks


PHASE_ORDER = {
    "foundation": 0,
    "general": 1,
    "implementation": 2,
    "polish": 3,
}


def enrich_plan(plan: Dict[str, Any]) -> Dict[str, Any]:
    linked_tasks = list_linked_tasks(plan.get("task_ids", []))
    health = build_plan_health(linked_tasks)
    return {
        **plan,
        "task_count": len(linked_tasks),
        "linked_tasks": linked_tasks,
        "health": health,
        "recommendations": build_plan_recommendations(plan, linked_tasks, health),
        "manager_summary": build_manager_summary(plan, linked_tasks, health),
        "execution_packet": build_execution_packet(plan, linked_tasks),
    }


def list_linked_tasks(task_ids: List[str]) -> List[Dict[str, Any]]:
    if not task_ids:
        return []
    tasks_by_id = {task["id"]: task for task in list_tasks()}
    return [tasks_by_id[task_id] for task_id in task_ids if task_id in tasks_by_id]


def build_manager_summary(
    plan: Dict[str, Any],
    linked_tasks: List[Dict[str, Any]],
    health: Dict[str, Any],
) -> Dict[str, Any]:
    done_tasks = len([task for task in linked_tasks if task["status"] == "done"])
    open_tasks = len([task for task in linked_tasks if task["status"] != "done"])
    return {
        "goal": plan.get("goal", ""),
        "task_count": len(linked_tasks),
        "done_task_count": done_tasks,
        "open_task_count": open_tasks,
        "main_risk": (plan.get("risks") or ["No explicit risk recorded yet."])[0],
        "next_manager_checkpoint": next_manager_checkpoint(linked_tasks),
        "progress": health["progress"],
        "state": health["state"],
    }


def build_execution_packet(plan: Dict[str, Any], linked_tasks: List[Dict[str, Any]]) -> Dict[str, Any]:
    ordered_tasks = sorted(
        linked_tasks,
        key=lambda item: (
            PHASE_ORDER.get(item["phase"] or "general", 99),
            item["priority"],
            item["title"],
        ),
    )
    parallel_tracks: Dict[str, List[str]] = {}
    for task in ordered_tasks:
        parallel_tracks.setdefault(task["phase"] or "general", []).append(task["title"])
    return {
        "manager_goal": plan.get("goal", ""),
        "constraints": plan.get("scope", [])[:4],
        "assumptions": plan.get("assumptions", [])[:4],
        "ordered_tasks": [
            {
                "id": task["id"],
                "title": task["title"],
                "phase": task["phase"],
                "priority": task["priority"],
                "acceptance": [item.get("text", "") for item in task.get("acceptance", [])],
            }
            for task in ordered_tasks
        ],
        "parallel_tracks": [{"phase": phase, "tasks": titles} for phase, titles in parallel_tracks.items()],
        "prompt": build_agent_prompt(plan, ordered_tasks),
    }


def build_agent_prompt(plan: Dict[str, Any], ordered_tasks: List[Dict[str, Any]]) -> str:
    lines = [
        f"Goal: {plan.get('goal', '')}",
        "Constraints:",
        *[f"- {item}" for item in plan.get("scope", [])[:4]],
        "Tasks:",
    ]
    for task in ordered_tasks:
        lines.append(f"- {task['title']} [{task['phase']}, {task['priority']}]")
        for acceptance in task.get("acceptance", []):
            text = acceptance.get("text", "") if isinstance(acceptance, dict) else str(acceptance)
            if text:
                lines.append(f"  acceptance: {text}")
    if plan.get("risks"):
        lines.append("Risks:")
        lines.extend(f"- {risk}" for risk in plan["risks"][:3])
    return "\n".join(lines).strip()


def next_manager_checkpoint(linked_tasks: List[Dict[str, Any]]) -> str:
    for phase in PHASE_ORDER:
        for task in linked_tasks:
            if task["phase"] == phase and task["status"] != "done":
                return f"Review `{task['title']}` before moving the plan forward."
    if linked_tasks:
        return "Check whether remaining tasks still match the roadmap milestone."
    return "Generate tasks for this plan so execution can start."


def build_plan_health(linked_tasks: List[Dict[str, Any]]) -> Dict[str, Any]:
    task_count = len(linked_tasks)
    done_count = len([task for task in linked_tasks if task["status"] == "done"])
    blocked_count = len([task for task in linked_tasks if task["status"] == "blocked"])
    in_progress_count = len([task for task in linked_tasks if task["status"] == "in_progress"])
    todo_count = len([task for task in linked_tasks if task["status"] == "todo"])
    progress = int((done_count / task_count) * 100) if task_count else 0
    task_ids = [task["id"] for task in linked_tasks]
    linked_commits = [commit for commit in list_commits() if commit.get("task_id") in task_ids]
    ready_commits = [
        commit
        for commit in linked_commits
        if commit["status"] in ("committed", "merged") and commit["review_status"] == "approved"
    ]
    verification_gaps = [
        commit
        for commit in linked_commits
        if commit["status"] in ("committed", "merged")
        and (commit["review_status"] != "approved" or commit["test_status"] != "passed")
    ]

    issues: List[str] = []
    if not task_count:
        issues.append("no_tasks")
    if task_count and in_progress_count == 0 and done_count < task_count:
        issues.append("no_active_execution")
    if blocked_count:
        issues.append("blocked_tasks")
    if todo_count == task_count and task_count:
        issues.append("not_started")
    if done_count and done_count < task_count and not linked_commits:
        issues.append("no_delivery_evidence")
    if verification_gaps:
        issues.append("verification_pending")

    if done_count == task_count and task_count:
        state = "completed"
    elif blocked_count:
        state = "blocked"
    elif verification_gaps and in_progress_count == 0:
        state = "verification"
    elif in_progress_count:
        state = "active"
    elif task_count:
        state = "waiting"
    else:
        state = "draft"

    return {
        "state": state,
        "progress": progress,
        "task_count": task_count,
        "done_count": done_count,
        "blocked_count": blocked_count,
        "in_progress_count": in_progress_count,
        "todo_count": todo_count,
        "linked_commit_count": len(linked_commits),
        "ready_commit_count": len(ready_commits),
        "verification_gap_count": len(verification_gaps),
        "issues": issues,
        "needs_manager_attention": bool(issues and state != "completed"),
    }


def build_plan_recommendations(
    plan: Dict[str, Any],
    linked_tasks: List[Dict[str, Any]],
    health: Dict[str, Any],
) -> List[Dict[str, Any]]:
    recommendations: List[Dict[str, Any]] = []

    if "no_tasks" in health["issues"]:
        recommendations.append(
            {
                "kind": "generate_tasks",
                "auto_supported": False,
                "priority": "high",
                "title": "Generate execution tasks for this plan",
                "reason": "The plan has no linked tasks, so execution cannot start.",
                "command": (
                    f"aipmc plan generate --roadmap-id {plan.get('roadmap_id') or ''} "
                    f'--title "{escape_cli(plan["title"])}" --create-tasks'
                ),
            }
        )

    if "no_active_execution" in health["issues"]:
        next_task = first_open_task(linked_tasks)
        if next_task:
            recommendations.append(
                {
                    "kind": "start_task",
                    "auto_supported": True,
                    "priority": "high",
                    "title": f"Start `{next_task['title']}`",
                    "reason": "The plan has open tasks but no task is currently in progress.",
                    "command": f"aipmc task update --id {next_task['id']} --status in_progress",
                }
            )

    if "blocked_tasks" in health["issues"]:
        blocked_task = next((task for task in linked_tasks if task["status"] == "blocked"), None)
        if blocked_task:
            recommendations.append(
                {
                    "kind": "unblock_task",
                    "auto_supported": False,
                    "priority": "high",
                    "title": f"Unblock `{blocked_task['title']}`",
                    "reason": "A blocked task is stalling the plan.",
                    "command": f"aipmc task show --id {blocked_task['id']}",
                }
            )

    if "verification_pending" in health["issues"]:
        recommendations.append(
            {
                "kind": "review_commits",
                "auto_supported": False,
                "priority": "high",
                "title": "Close verification gaps on linked commits",
                "reason": "Plan execution has delivery evidence, but review or test verification is still incomplete.",
                "command": "aipmc commit list --status committed",
            }
        )

    if health["state"] == "active":
        active_task = next((task for task in linked_tasks if task["status"] == "in_progress"), None)
        if active_task:
            recommendations.append(
                {
                    "kind": "review_progress",
                    "auto_supported": False,
                    "priority": "medium",
                    "title": f"Review progress on `{active_task['title']}`",
                    "reason": "An active task should be checked against acceptance and evidence before more work is queued.",
                    "command": f"aipmc task show --id {active_task['id']}",
                }
            )

    if health["state"] == "completed":
        recommendations.append(
            {
                "kind": "close_plan",
                "auto_supported": True,
                "priority": "medium",
                "title": "Archive or update the completed plan",
                "reason": "All linked tasks are done; the plan should be closed or superseded explicitly.",
                "command": f"aipmc plan update --id {plan['id']} --status completed",
            }
        )

    return recommendations[:4]


def first_open_task(linked_tasks: List[Dict[str, Any]]) -> Optional[Dict[str, Any]]:
    for phase in PHASE_ORDER:
        for task in linked_tasks:
            if task["phase"] == phase and task["status"] != "done":
                return task
    return next((task for task in linked_tasks if task["status"] != "done"), None)


def escape_cli(value: str) -> str:
    return value.replace('"', '\\"')


def extract_id_from_command(command: str, flag: str) -> Optional[str]:
    parts = command.split()
    for index, part in enumerate(parts[:-1]):
        if part == flag:
            return parts[index + 1]
    return None
