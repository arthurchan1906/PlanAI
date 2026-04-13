from __future__ import annotations

import argparse

from ...store import (
    append_daily_note,
    get_daily_note,
    replace_daily_note,
)
from .project import run_local_command


def handle_daily(args: argparse.Namespace) -> None:
    if args.daily_command == "show":
        run_local_command(lambda: get_daily_note(args.date))
    elif args.daily_command == "close":
        run_local_command(
            lambda: append_daily_note(
                {
                    "completed": args.completed,
                    "problems": args.problems,
                    "risks": args.risks,
                    "next": args.next,
                }
            )
        )
    elif args.daily_command == "replace":
        run_local_command(
            lambda: replace_daily_note(
                {
                    "completed": args.completed,
                    "problems": args.problems,
                    "risks": args.risks,
                    "next": args.next,
                },
                args.date,
            )
        )
