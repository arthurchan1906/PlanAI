from __future__ import annotations

import argparse

from ...store import (
    create_link,
    list_links,
)
from .project import run_local_command


def handle_link(args: argparse.Namespace) -> None:
    if args.link_command == "list":
        run_local_command(lambda: {"links": list_links(args.source_id or None, args.target_id or None, args.relation or None)})
    elif args.link_command == "add":
        run_local_command(lambda: create_link({
            "source_type": args.source_type,
            "source_id": args.source_id,
            "relation": args.relation,
            "target_type": args.target_type,
            "target_id": args.target_id,
            "note": args.note,
        }))
    elif args.link_command == "delete":
        from ...store import delete_link
        run_local_command(lambda: {"ok": delete_link(args.id)})
