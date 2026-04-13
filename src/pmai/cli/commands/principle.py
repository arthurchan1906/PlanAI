from __future__ import annotations

import argparse

from ...store import create_principle, get_principle, list_principles, update_principle
from .project import run_local_command


def handle_principle(args: argparse.Namespace) -> None:
    if args.principle_command == "list":
        run_local_command(lambda: {"principles": list_principles(args.status or None, args.kind or None)})
    elif args.principle_command == "show":
        run_local_command(lambda: get_principle(args.id))
    elif args.principle_command == "add":
        run_local_command(lambda: create_principle({
            "title": args.title,
            "summary": args.summary,
            "kind": args.kind,
            "status": args.status,
        }))
    elif args.principle_command == "update":
        run_local_command(lambda: update_principle(args.id, {
            "title": args.title,
            "summary": args.summary,
            "kind": args.kind,
            "status": args.status,
        }))
