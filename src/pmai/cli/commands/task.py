from __future__ import annotations

import argparse
from typing import Any, Dict, List

from ...store import (
    append_task_note,
    create_task,
    get_task,
    list_task_notes,
    list_tasks,
    update_task,
)
from .creation_guidance import (
    build_created_response,
    build_possible_duplicate_response,
    escape_cli_text,
    normalize_text,
    text_terms,
)
from .followup_guidance import build_guided_response
from .project import run_local_command


def _build_duplicate_payload(
    *,
    title: str,
    priority: str,
    phase: str,
    roadmap_id: str | None,
    plan_id: str | None,
) -> Dict[str, Any] | None:
    normalized_title = normalize_text(title)
    terms = text_terms(title)
    if not normalized_title or not terms:
        return None
    escaped_title = escape_cli_text(title)

    candidates: List[Dict[str, Any]] = []
    for task in list_tasks():
        existing_normalized = normalize_text(task.get("title", ""))
        if not existing_normalized:
            continue

        score = 0
        reasons: List[str] = []
        if existing_normalized == normalized_title:
            score += 100
            reasons.append("exact_title_match")
        overlap = len(set(terms) & set(text_terms(task.get("title", ""))))
        if overlap:
            score += overlap * 10
            reasons.append(f"title_term_overlap:{overlap}")
        if roadmap_id and task.get("roadmap_id") == roadmap_id:
            score += 3
            reasons.append("same_roadmap")
        if plan_id and task.get("plan_id") == plan_id:
            score += 4
            reasons.append("same_plan")
        if phase and task.get("phase") == phase:
            score += 1
            reasons.append("same_phase")
        if priority and task.get("priority") == priority:
            score += 1
            reasons.append("same_priority")

        if score < 10:
            continue
        candidates.append(
            {
                "type": "task",
                "id": task["id"],
                "title": task["title"],
                "status": task["status"],
                "priority": task.get("priority"),
                "phase": task.get("phase"),
                "roadmap_id": task.get("roadmap_id"),
                "plan_id": task.get("plan_id"),
                "score": score,
                "reasons": reasons,
                "command": f"aipmc task show --id {task['id']}",
            }
        )

    if not candidates:
        return None

    candidates.sort(key=lambda item: (-item["score"], item["status"] != "in_progress", item["title"]))
    top = candidates[:5]
    primary = top[0]
    return build_possible_duplicate_response(
        message="Found similar existing task(s). Reuse or inspect them before creating a new one.",
        input_payload={
            "title": title,
            "priority": priority,
            "phase": phase,
            "roadmap_id": roadmap_id,
            "plan_id": plan_id,
        },
        candidates=top,
        next_steps=[
            {
                "command": f"aipmc task show --id {primary['id']}",
                "reason": "Inspect the closest existing task first.",
            },
            {
                "command": f"aipmc task note --id {primary['id']} --content \"continue work on {escaped_title}\"",
                "reason": "If this is the same workstream, continue the existing task instead of creating a duplicate.",
            },
            {
                "command": f"aipmc task add --title \"{escaped_title}\" --priority {priority} --phase {phase} --force-create",
                "reason": "Only create a new task if these candidates do not fit.",
            },
        ],
    )


def _build_created_payload(task: Dict[str, Any]) -> Dict[str, Any]:
    return build_created_response(
        kind="task",
        payload=task,
        next_steps=[
            {
                "command": f"aipmc task show --id {task['id']}",
                "reason": "Inspect the created task and confirm acceptance/progress context.",
            },
            {
                "command": f"aipmc link create --source task --source-id {task['id']} --target doc --target-id <doc-path> --relation references",
                "reason": "If this task follows a design doc, link it for traceability.",
            },
            {
                "command": f"aipmc task update --id {task['id']} --status in_progress",
                "reason": "Move it to active work when you start implementing.",
            },
        ],
    )


def _build_updated_payload(task: Dict[str, Any]) -> Dict[str, Any]:
    task_id = task["id"]
    context_updates: List[Dict[str, Any]] = [
        {
            "type": "task_focus",
            "task_id": task_id,
            "status": task.get("status"),
            "summary": f"Task `{task.get('title')}` is now `{task.get('status')}`.",
        }
    ]
    if task.get("plan_id"):
        context_updates.append(
            {
                "type": "plan_membership",
                "task_id": task_id,
                "plan_id": task["plan_id"],
                "summary": f"Task stays attached to plan `{task['plan_id']}`.",
            }
        )
    if task.get("roadmap_id"):
        context_updates.append(
            {
                "type": "roadmap_alignment",
                "task_id": task_id,
                "roadmap_id": task["roadmap_id"],
                "summary": f"Task stays aligned to roadmap `{task['roadmap_id']}`.",
            }
        )

    next_steps: List[Dict[str, Any]] = [
        {
            "command": f"aipmc task show --id {task_id}",
            "reason": "Re-read the full task context before the next edit or commit.",
        }
    ]
    if task.get("status") == "in_progress":
        next_steps.append(
            {
                "command": f"aipmc commit add --task-id {task_id} --title \"{escape_cli_text(task['title'])}\"",
                "reason": "Record the implementation work against the active task.",
            }
        )
    elif task.get("status") == "done":
        next_steps.append(
            {
                "command": "aipmc session close --from-commits --from-tasks",
                "reason": "Close the working session after task completion and refresh the daily record.",
            }
        )
    else:
        next_steps.append(
            {
                "command": f"aipmc task update --id {task_id} --status in_progress",
                "reason": "Promote the task when execution really starts.",
            }
        )

    return build_guided_response(
        message="Task updated and project context refreshed.",
        payload={"task": task},
        context_updates=context_updates,
        next_steps=next_steps,
    )


def handle_task(args: argparse.Namespace) -> None:
    if args.task_command == "list":
        run_local_command(lambda: {"tasks": list_tasks(args.status or None)})
    elif args.task_command == "show":
        run_local_command(lambda: get_task(args.id))
    elif args.task_command == "add":
        def _add_task() -> Dict[str, Any]:
            if not args.force_create:
                duplicate_payload = _build_duplicate_payload(
                    title=args.title,
                    priority=args.priority,
                    phase=args.phase,
                    roadmap_id=args.roadmap_id,
                    plan_id=args.plan_id,
                )
                if duplicate_payload is not None:
                    return duplicate_payload
            task = create_task(
                {
                    "title": args.title,
                    "priority": args.priority,
                    "status": args.status,
                    "phase": args.phase,
                    "roadmap_id": args.roadmap_id,
                    "plan_id": args.plan_id,
                    "acceptance": args.acceptance,
                }
            )
            return _build_created_payload(task)

        run_local_command(
            _add_task
        )
    elif args.task_command == "update":
        run_local_command(
            lambda: _build_updated_payload(update_task(
                    args.id,
                    args.status,
                    args.note,
                    allow_without_commit=args.allow_without_commit,
                    roadmap_id=args.roadmap_id,
                    plan_id=args.plan_id,
                    append_note=args.append_note,
                ))
        )
    elif args.task_command == "note":
        run_local_command(lambda: append_task_note(args.id, args.content))
    elif args.task_command == "notes":
        run_local_command(lambda: list_task_notes(args.id, args.limit))
    elif args.task_command == "plan":
        from ...store import plan_task
        run_local_command(lambda: plan_task(args.id, args.steps))
    elif args.task_command == "checkpoint":
        from ...store import update_task_checkpoint
        run_local_command(lambda: update_task_checkpoint(args.id, args.index, args.done))
