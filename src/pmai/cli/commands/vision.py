from __future__ import annotations

import argparse

from ...store import create_vision, get_vision, list_visions, update_vision
from .project import run_local_command


def handle_vision(args: argparse.Namespace) -> None:
    if args.vision_command == "list":
        run_local_command(lambda: {"visions": list_visions(args.status or None)})
    elif args.vision_command == "show":
        run_local_command(lambda: get_vision(args.id))
    elif args.vision_command == "add":
        run_local_command(lambda: create_vision({
            "title": args.title,
            "summary": args.summary,
            "status": args.status,
            "horizon": args.horizon,
        }))
    elif args.vision_command == "update":
        run_local_command(lambda: update_vision(args.id, {
            "title": args.title,
            "summary": args.summary,
            "status": args.status,
            "horizon": args.horizon,
        }))
