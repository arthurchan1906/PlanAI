from __future__ import annotations

import argparse
import json

from ...store import advance_plan, create_plan, generate_plan, get_plan, list_plans, update_plan
from .project import run_local_command


def handle_plan(args: argparse.Namespace) -> None:
    if args.plan_command == "list":
        run_local_command(lambda: {"plans": list_plans(args.roadmap_id or None, args.status or None)})
    elif args.plan_command == "show":
        run_local_command(lambda: get_plan(args.id))
    elif args.plan_command == "packet":
        plan = get_plan(args.id)
        packet = plan.get("execution_packet", {})
        if args.format == "json":
            print(json.dumps(packet, ensure_ascii=False, indent=2))
        else:
            print(packet.get("prompt", ""))
    elif args.plan_command == "advance":
        run_local_command(lambda: advance_plan(args.id))
    elif args.plan_command == "add":
        run_local_command(
            lambda: create_plan(
                {
                    "title": args.title,
                    "goal": args.goal,
                    "roadmap_id": args.roadmap_id,
                    "vision_id": args.vision_id,
                    "priority": args.priority,
                    "status": args.status,
                    "scope": args.scope,
                    "risks": args.risks,
                    "assumptions": args.assumptions,
                }
            )
        )
    elif args.plan_command == "generate":
        run_local_command(
            lambda: generate_plan(
                roadmap_id=args.roadmap_id,
                vision_id=args.vision_id,
                title=args.title,
                create_tasks_for_plan=args.create_tasks,
                task_limit=args.task_limit,
            )
        )
    elif args.plan_command == "update":
        payload = {}
        for field in ("title", "goal", "status", "priority"):
            value = getattr(args, field)
            if value is not None:
                payload[field] = value
        run_local_command(lambda: update_plan(args.id, payload))
