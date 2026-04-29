from __future__ import annotations

import argparse

from ...store import (
    create_idea_comment,
    create_idea,
    convert_idea,
    get_idea,
    list_ideas,
    review_idea,
    update_idea,
)
from .creation_guidance import escape_cli_text
from .followup_guidance import build_guided_response
from .project import run_local_command


def _build_idea_payload(idea: dict, *, message: str) -> dict:
    idea_id = idea["id"]
    context_updates = [
        {
            "type": "idea_state",
            "idea_id": idea_id,
            "status": idea.get("status"),
            "recommended_next_action": idea.get("recommended_next_action"),
            "summary": f"Idea `{idea.get('title')}` is now `{idea.get('status')}`.",
        }
    ]
    if idea.get("converted_to"):
        context_updates.append(
            {
                "type": "idea_conversion",
                "idea_id": idea_id,
                "target_type": idea.get("converted_to_type"),
                "target_id": idea.get("converted_to"),
                "summary": (
                    f"Idea is already converted to {idea.get('converted_to_type')} "
                    f"`{idea.get('converted_to')}`."
                ),
            }
        )

    next_steps = [
        {
            "command": f"aipmc idea show --id {idea_id}",
            "reason": "Re-read the idea thread before deciding whether to convert or continue discussion.",
        }
    ]
    action = idea.get("recommended_next_action") or ""
    title = escape_cli_text(idea.get("title", ""))
    if action == "ready_for_task":
        next_steps.append(
            {
                "command": f"aipmc idea convert --id {idea_id} --to task",
                "reason": "This idea is ready to move into execution.",
            }
        )
    elif action == "ready_for_decision":
        next_steps.append(
            {
                "command": f"aipmc idea convert --id {idea_id} --to decision",
                "reason": "This idea is ready to become an explicit decision thread.",
            }
        )
    elif idea.get("converted_to_type") == "task":
        next_steps.append(
            {
                "command": f"aipmc task show --id {idea['converted_to']}",
                "reason": "Inspect the execution task created from this idea.",
            }
        )
    elif idea.get("converted_to_type") == "decision":
        next_steps.append(
            {
                "command": f"aipmc decision show --id {idea['converted_to']}",
                "reason": "Inspect the decision thread created from this idea.",
            }
        )
    else:
        next_steps.append(
            {
                "command": f"aipmc search \"{title}\"",
                "reason": "Check whether this idea overlaps with existing work before creating new records from it.",
            }
        )

    return build_guided_response(
        message=message,
        payload={"idea": idea},
        context_updates=context_updates,
        next_steps=next_steps,
    )


def _build_conversion_payload(result: dict) -> dict:
    converted = result.get("converted", {})
    idea = result.get("idea", {})
    target_type = converted.get("type")
    target_id = converted.get("id")
    next_steps = [
        {
            "command": f"aipmc idea show --id {idea['id']}",
            "reason": "Confirm the idea thread now points at the converted execution/governance record.",
        }
    ]
    if target_type == "task":
        next_steps.append(
            {
                "command": f"aipmc task show --id {target_id}",
                "reason": "Continue work from the created task instead of re-capturing the idea.",
            }
        )
    elif target_type == "decision":
        next_steps.append(
            {
                "command": f"aipmc decision show --id {target_id}",
                "reason": "Continue review from the created decision instead of keeping discussion in the idea thread.",
            }
        )
    return build_guided_response(
        message="Idea conversion completed and the next working record is ready.",
        payload=result,
        context_updates=[
            {
                "type": "idea_conversion",
                "idea_id": idea.get("id"),
                "target_type": target_type,
                "target_id": target_id,
                "summary": f"Idea converted to {target_type} `{target_id}`.",
            }
        ],
        next_steps=next_steps,
    )


def handle_idea(args: argparse.Namespace) -> None:
    if args.idea_command == "list":
        run_local_command(lambda: {"ideas": list_ideas(args.status or None)})
    elif args.idea_command == "show":
        run_local_command(lambda: get_idea(args.id))
    elif args.idea_command == "capture":
        run_local_command(
            lambda: _build_idea_payload(create_idea(
                {
                    "title": args.title,
                    "summary": args.summary,
                    "impact": args.impact,
                    "source": args.source,
                    "canon_conflict": args.canon_conflict,
                    "current_summary": args.current_summary,
                    "main_question": args.main_question,
                    "recommended_next_action": args.recommended_next_action,
                }
            ), message="Idea captured and kept in the project context.")
        )
    elif args.idea_command == "review":
        run_local_command(lambda: _build_idea_payload(review_idea(args.id, args.status, args.note), message="Idea review updated and follow-up intent refreshed."))
    elif args.idea_command == "update":
        run_local_command(
            lambda: _build_idea_payload(update_idea(
                args.id,
                {
                    "title": args.title,
                    "summary": args.summary,
                    "impact": args.impact,
                    "source": args.source,
                    "status": args.status,
                    "current_summary": args.current_summary,
                    "main_question": args.main_question,
                    "recommended_next_action": args.recommended_next_action,
                },
            ), message="Idea updated and next action refreshed.")
        )
    elif args.idea_command == "comment":
        run_local_command(
            lambda: create_idea_comment(
                args.id,
                content=args.content,
                kind=args.kind,
                author_type=args.author_type,
                author_name=args.author_name,
            )
        )
    elif args.idea_command == "convert":
        run_local_command(lambda: _build_conversion_payload(convert_idea(args.id, args.target_type)))
