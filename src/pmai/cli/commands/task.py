from __future__ import annotations

import argparse

from ...store import (
    create_task,
    get_task,
    list_tasks,
    update_task,
)
from .project import run_local_command


def handle_task(args: argparse.Namespace) -> None:
    if args.task_command == "list":
        run_local_command(lambda: {"tasks": list_tasks(args.status or None)})
    elif args.task_command == "show":
        run_local_command(lambda: get_task(args.id))
    elif args.task_command == "add":
        run_local_command(
            lambda: create_task(
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
        )
    elif args.task_command == "update":
        run_local_command(
            lambda: update_task(
                    args.id,
                    args.status,
                    args.note,
                    allow_without_commit=args.allow_without_commit,
                    roadmap_id=args.roadmap_id,
                    plan_id=args.plan_id,
                )
        )
    elif args.task_command == "plan":
        from ...store import plan_task
        run_local_command(lambda: plan_task(args.id, args.steps))
    elif args.task_command == "checkpoint":
        from ...store import update_task_checkpoint
        run_local_command(lambda: update_task_checkpoint(args.id, args.index, args.done))
