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
from .project import run_local_command


def handle_idea(args: argparse.Namespace) -> None:
    if args.idea_command == "list":
        run_local_command(lambda: {"ideas": list_ideas(args.status or None)})
    elif args.idea_command == "show":
        run_local_command(lambda: get_idea(args.id))
    elif args.idea_command == "capture":
        run_local_command(
            lambda: create_idea(
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
            )
        )
    elif args.idea_command == "review":
        run_local_command(lambda: review_idea(args.id, args.status, args.note))
    elif args.idea_command == "update":
        run_local_command(
            lambda: update_idea(
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
            )
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
        run_local_command(lambda: convert_idea(args.id, args.target_type))
