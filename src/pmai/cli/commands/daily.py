from __future__ import annotations

import argparse

from ...store import (
    append_daily_note,
    build_daily_summary_from_activity,
    get_daily_note,
    replace_daily_note,
)
from .project import run_local_command


def _normalize_items(value: list) -> list[str]:
    items: list[str] = []
    for group in value or []:
        values = group if isinstance(group, list) else [group]
        for raw in values:
            # Handle split by newline, semicolon, or comma
            content = str(raw).replace("\n", ";").replace(",", ";")
            for part in content.split(";"):
                item = part.strip()
                if item:
                    items.append(item)
    return items


def _daily_payload(args: argparse.Namespace, *, date: str | None = None) -> dict:
    payload = {
        "completed": _normalize_items(args.completed),
        "problems": _normalize_items(args.problems),
        "risks": _normalize_items(args.risks),
        "next": _normalize_items(args.next),
    }
    generated = build_daily_summary_from_activity(
        include_commits=getattr(args, "from_commits", False),
        include_tasks=getattr(args, "from_tasks", False),
        note_date=date,
    )
    return {
        key: payload[key] + generated[key]
        for key in ("completed", "problems", "risks", "next")
    }


def handle_daily(args: argparse.Namespace) -> None:
    if args.daily_command == "show":
        run_local_command(lambda: get_daily_note(args.date))
    elif args.daily_command == "close":
        run_local_command(
            lambda: append_daily_note(
                _daily_payload(args)
            )
        )
    elif args.daily_command == "replace":
        run_local_command(
            lambda: replace_daily_note(
                _daily_payload(args, date=args.date),
                args.date,
            )
        )
