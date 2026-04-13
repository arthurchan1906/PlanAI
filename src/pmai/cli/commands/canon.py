from __future__ import annotations

import argparse

from ...store import fetch_canon, update_canon
from .project import run_local_command


def handle_canon(args: argparse.Namespace) -> None:
    if args.canon_command == "show":
        run_local_command(fetch_canon)
    elif args.canon_command == "update":
        run_local_command(
            lambda: update_canon(
                {
                    "decision_id": args.decision_id,
                    "product_goal": args.product_goal,
                    "engineering_focus": args.engineering_focus,
                    "architecture": args.architecture,
                    "add_scope": args.add_scope,
                    "add_avoid": args.add_avoid,
                }
            )
        )
