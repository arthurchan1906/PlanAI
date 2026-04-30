from __future__ import annotations

import argparse

from ...store import create_bug, get_bug, list_bugs, update_bug
from .project import run_local_command


def handle_bug(args: argparse.Namespace) -> None:
    if args.bug_command == "list":
        run_local_command(
            lambda: {
                "bugs": list_bugs(
                    args.status or None,
                    args.severity or None,
                    args.commit_id or None,
                    args.limit,
                )
            }
        )
    elif args.bug_command == "show":
        run_local_command(lambda: get_bug(args.id))
    elif args.bug_command == "add":
        run_local_command(
            lambda: create_bug({
                "title": args.title,
                "description": args.description,
                "severity": args.severity,
                "status": args.status,
                "commit_id": args.commit_id or None,
            })
        )
    elif args.bug_command == "update":
        run_local_command(
            lambda: update_bug(
                args.id,
                {
                    "title": args.title,
                    "description": args.description,
                    "severity": args.severity,
                    "status": args.status,
                    "commit_id": args.commit_id,
                    "clear_commit_id": args.clear_commit_id,
                },
            )
        )
