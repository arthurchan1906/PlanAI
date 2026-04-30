from __future__ import annotations

import argparse
import json
from typing import Any, Dict, List

from ...store import advance_plan, create_plan, generate_plan, get_plan, list_plans, update_plan
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
    goal: str,
    roadmap_id: str | None,
    vision_id: str | None,
    priority: str,
) -> Dict[str, Any] | None:
    normalized_title = normalize_text(title)
    title_terms = text_terms(title)
    goal_terms = text_terms(goal)
    if not normalized_title or not title_terms:
        return None

    escaped_title = escape_cli_text(title)
    candidates: List[Dict[str, Any]] = []
    for plan in list_plans():
        existing_title = normalize_text(plan.get("title", ""))
        if not existing_title:
            continue

        score = 0
        reasons: List[str] = []
        if existing_title == normalized_title:
            score += 100
            reasons.append("exact_title_match")
        title_overlap = len(set(title_terms) & set(text_terms(plan.get("title", ""))))
        if title_overlap:
            score += title_overlap * 10
            reasons.append(f"title_term_overlap:{title_overlap}")
        if goal_terms:
            goal_overlap = len(set(goal_terms) & set(text_terms(plan.get("goal", ""))))
            if goal_overlap:
                score += goal_overlap * 4
                reasons.append(f"goal_term_overlap:{goal_overlap}")
        if roadmap_id and plan.get("roadmap_id") == roadmap_id:
            score += 5
            reasons.append("same_roadmap")
        if vision_id and plan.get("vision_id") == vision_id:
            score += 3
            reasons.append("same_vision")
        if priority and plan.get("priority") == priority:
            score += 1
            reasons.append("same_priority")

        if score < 10:
            continue
        candidates.append(
            {
                "type": "plan",
                "id": plan["id"],
                "title": plan["title"],
                "status": plan.get("status"),
                "priority": plan.get("priority"),
                "roadmap_id": plan.get("roadmap_id"),
                "vision_id": plan.get("vision_id"),
                "score": score,
                "reasons": reasons,
                "command": f"aipmc plan show --id {plan['id']}",
            }
        )

    if not candidates:
        return None

    candidates.sort(key=lambda item: (-item["score"], item["status"] != "active", item["title"]))
    primary = candidates[0]
    return build_possible_duplicate_response(
        message="Found similar existing plan(s). Reuse or inspect them before creating a new one.",
        input_payload={
            "title": title,
            "goal": goal,
            "roadmap_id": roadmap_id,
            "vision_id": vision_id,
            "priority": priority,
        },
        candidates=candidates,
        next_steps=[
            {
                "command": f"aipmc plan show --id {primary['id']}",
                "reason": "Inspect the closest existing plan first.",
            },
            {
                "command": f"aipmc plan update --id {primary['id']} --title \"{escaped_title}\"",
                "reason": "If this is the same workstream, refine the existing plan instead of creating a duplicate.",
            },
            {
                "command": f"aipmc plan add --title \"{escaped_title}\" --priority {priority} --force-create",
                "reason": "Only create a new plan if these candidates do not fit.",
            },
        ],
    )


def _build_created_payload(plan: Dict[str, Any]) -> Dict[str, Any]:
    return build_created_response(
        kind="plan",
        payload=plan,
        next_steps=[
            {
                "command": f"aipmc plan show --id {plan['id']}",
                "reason": "Inspect the created plan and confirm scope, risks, and linked tasks.",
            },
            {
                "command": f"aipmc link create --source plan --source-id {plan['id']} --target doc --target-id <doc-path> --relation references",
                "reason": "If this plan is driven by a design doc, link it for traceability.",
            },
            {
                "command": f"aipmc plan update --id {plan['id']} --status active",
                "reason": "Promote it to active when it becomes the current mainline plan.",
            },
        ],
    )


def _build_updated_payload(plan: Dict[str, Any]) -> Dict[str, Any]:
    plan_id = plan["id"]
    task_ids = plan.get("task_ids", [])
    context_updates: List[Dict[str, Any]] = [
        {
            "type": "plan_focus",
            "plan_id": plan_id,
            "status": plan.get("status"),
            "summary": f"Plan `{plan.get('title')}` is now `{plan.get('status')}`.",
        }
    ]
    if plan.get("roadmap_id"):
        context_updates.append(
            {
                "type": "roadmap_alignment",
                "plan_id": plan_id,
                "roadmap_id": plan["roadmap_id"],
                "summary": f"Plan stays aligned to roadmap `{plan['roadmap_id']}`.",
            }
        )
    if task_ids:
        context_updates.append(
            {
                "type": "task_sync",
                "plan_id": plan_id,
                "task_count": len(task_ids),
                "summary": f"{len(task_ids)} task(s) are now synced to this plan.",
            }
        )

    next_steps: List[Dict[str, Any]] = [
        {
            "command": f"aipmc plan show --id {plan_id}",
            "reason": "Inspect the updated plan before changing related tasks.",
        }
    ]
    if task_ids:
        next_steps.append(
            {
                "command": f"aipmc task show --id {task_ids[0]}",
                "reason": "Verify one linked task to confirm the plan-task sync landed as expected.",
            }
        )
    else:
        next_steps.append(
            {
                "command": f"aipmc task add --title \"Implement {escape_cli_text(plan['title'])}\" --plan-id {plan_id}",
                "reason": "Create or attach execution work only after re-checking this plan's scope.",
            }
        )

    return build_guided_response(
        message="Plan updated and related execution context refreshed.",
        payload={"plan": plan},
        context_updates=context_updates,
        next_steps=next_steps,
    )


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
        def _add_plan() -> Dict[str, Any]:
            if not args.force_create:
                duplicate_payload = _build_duplicate_payload(
                    title=args.title,
                    goal=args.goal,
                    roadmap_id=args.roadmap_id,
                    vision_id=args.vision_id,
                    priority=args.priority,
                )
                if duplicate_payload is not None:
                    return duplicate_payload
            plan = create_plan(
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
                    "task_ids": args.task_ids,
                }
            )
            return _build_created_payload(plan)

        run_local_command(
            _add_plan
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
        if args.task_ids is not None:
            payload["task_ids"] = args.task_ids
        run_local_command(lambda: _build_updated_payload(update_plan(args.id, payload)))
