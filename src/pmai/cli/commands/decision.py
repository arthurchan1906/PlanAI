from __future__ import annotations

import argparse

from ...store import (
    create_decision,
    get_decision,
    list_decisions,
    update_decision_status,
)
from .project import run_local_command


def handle_decision(args: argparse.Namespace) -> None:
    if args.decision_command == "list":
        run_local_command(lambda: {"decisions": list_decisions()})
    elif args.decision_command == "show":
        run_local_command(lambda: get_decision(args.id))
    elif args.decision_command == "add":
        run_local_command(
            lambda: create_decision(
                {
                    "title": args.title,
                    "background": args.background,
                    "decision": args.decision,
                    "status": args.status,
                }
            )
        )
    elif args.decision_command == "review":
        run_local_command(lambda: update_decision_status(args.id, args.status))
