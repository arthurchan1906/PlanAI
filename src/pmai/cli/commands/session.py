from __future__ import annotations

import argparse

from ...store import (
    append_daily_note,
    append_task_note,
    build_daily_summary_from_activity,
    build_handoff_packet,
)
from .daily import _normalize_items
from .project import run_local_command


def _close_session(args: argparse.Namespace) -> dict:
    manual = {
        "completed": _normalize_items(args.completed),
        "problems": _normalize_items(args.problems),
        "risks": _normalize_items(args.risks),
        "next": _normalize_items(args.next),
    }
    generated = build_daily_summary_from_activity(
        include_commits=args.from_commits,
        include_tasks=args.from_tasks,
    )
    daily = append_daily_note(
        {
            key: manual[key] + generated[key]
            for key in ("completed", "problems", "risks", "next")
        }
    )

    task_note = None
    if args.task_id and args.note:
        task_note = append_task_note(args.task_id, args.note)

    return {
        "daily": daily,
        "task_note": task_note,
        "handoff": build_handoff_packet(),
    }


def handle_session(args: argparse.Namespace) -> None:
    if args.session_command == "close":
        run_local_command(lambda: _close_session(args))
