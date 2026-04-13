from __future__ import annotations

import argparse

from ...store import (
    get_git_diff_summary,
    get_git_worktree_status,
    list_recent_git_commits,
)
from .project import run_local_command


def handle_code(args: argparse.Namespace) -> None:
    if args.code_command == "status":
        run_local_command(get_git_worktree_status)
    elif args.code_command == "diff":
        run_local_command(lambda: get_git_diff_summary(staged=args.staged))
    elif args.code_command == "recent":
        run_local_command(lambda: {"items": list_recent_git_commits(args.limit)})
