from __future__ import annotations

import argparse

from ...store import (
    create_idea,
    get_idea,
    list_ideas,
    review_idea,
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
                }
            )
        )
    elif args.idea_command == "review":
        run_local_command(lambda: review_idea(args.id, args.status, args.note))
