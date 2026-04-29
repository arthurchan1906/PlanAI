from __future__ import annotations

import argparse

from ...store import (
    create_link,
    get_plan,
    get_task,
    list_links,
)
from .followup_guidance import build_guided_response
from .project import run_local_command


def _build_link_payload(link: dict) -> dict:
    pair = {link["source_type"], link["target_type"]}
    context_updates = [
        {
            "type": "link_created",
            "link_id": link["id"],
            "summary": (
                f"Created `{link['relation']}` link from "
                f"{link['source_type']} `{link['source_id']}` to {link['target_type']} `{link['target_id']}`."
            ),
        }
    ]
    next_steps = []

    if pair == {"plan", "task"}:
        plan_id = link["source_id"] if link["source_type"] == "plan" else link["target_id"]
        task_id = link["source_id"] if link["source_type"] == "task" else link["target_id"]
        task = get_task(task_id)["task"]
        context_updates.append(
            {
                "type": "task_sync",
                "task_id": task_id,
                "plan_id": task.get("plan_id"),
                "roadmap_id": task.get("roadmap_id"),
                "summary": "Link creation also synced the task's plan/roadmap fields.",
            }
        )
        next_steps.extend(
            [
                {
                    "command": f"aipmc task show --id {task_id}",
                    "reason": "Confirm the task now points at the intended plan context.",
                },
                {
                    "command": f"aipmc plan show --id {plan_id}",
                    "reason": "Check the plan now lists the task in its execution set.",
                },
            ]
        )
    elif pair == {"roadmap", "task"}:
        task_id = link["source_id"] if link["source_type"] == "task" else link["target_id"]
        task = get_task(task_id)["task"]
        context_updates.append(
            {
                "type": "roadmap_sync",
                "task_id": task_id,
                "roadmap_id": task.get("roadmap_id"),
                "summary": "Link creation also synced the task's roadmap field.",
            }
        )
        next_steps.append(
            {
                "command": f"aipmc task show --id {task_id}",
                "reason": "Confirm the task now carries the intended roadmap alignment.",
            }
        )
    elif pair == {"roadmap", "plan"}:
        plan_id = link["source_id"] if link["source_type"] == "plan" else link["target_id"]
        plan = get_plan(plan_id)
        context_updates.append(
            {
                "type": "plan_sync",
                "plan_id": plan_id,
                "roadmap_id": plan.get("roadmap_id"),
                "task_count": len(plan.get("task_ids", [])),
                "summary": "Link creation also synced the plan roadmap and inherited roadmap context to linked tasks.",
            }
        )
        next_steps.extend(
            [
                {
                    "command": f"aipmc plan show --id {plan_id}",
                    "reason": "Verify the plan now points at the intended roadmap.",
                },
                {
                    "command": f"aipmc next",
                    "reason": "Refresh the mainline recommendation after changing roadmap structure.",
                },
            ]
        )
    else:
        next_steps.append(
            {
                "command": "aipmc next",
                "reason": "Refresh the current recommendation after adding a new relationship.",
            }
        )

    return build_guided_response(
        message="Link created and related context updated.",
        payload={"link": link},
        context_updates=context_updates,
        next_steps=next_steps,
    )


def handle_link(args: argparse.Namespace) -> None:
    if args.link_command == "list":
        run_local_command(lambda: {"links": list_links(args.source_id or None, args.target_id or None, args.relation or None)})
    elif args.link_command == "add":
        run_local_command(lambda: _build_link_payload(create_link({
            "source_type": args.source_type,
            "source_id": args.source_id,
            "relation": args.relation,
            "target_type": args.target_type,
            "target_id": args.target_id,
            "note": args.note,
        })))
    elif args.link_command == "delete":
        from ...store import delete_link
        run_local_command(lambda: {"ok": delete_link(args.id)})
