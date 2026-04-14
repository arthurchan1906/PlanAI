from __future__ import annotations

import argparse

from ...store import (
    create_commit,
    get_commit,
    list_commits,
    update_commit,
)
from .project import run_local_command


def handle_commit(args: argparse.Namespace) -> None:
    if args.commit_command == "list":
        run_local_command(
            lambda: {
                    "commits": list_commits(
                        args.status or None,
                        args.task_id or None,
                        args.decision_id or None,
                    )
                },
        )
    elif args.commit_command == "show":
        run_local_command(lambda: get_commit(args.id))
    elif args.commit_command == "add":
        run_local_command(
            lambda: create_commit(
                {
                    "title": args.title,
                    "summary": args.summary,
                    "evidence_summary": args.evidence_summary,
                    "review_notes": args.review_notes,
                    "branch": args.branch,
                    "commit_hash": args.commit_hash,
                    "task_id": args.task_id or None,
                    "decision_id": args.decision_id or None,
                    "status": args.status,
                    "test_status": args.test_status,
                    "review_status": args.review_status,
                    "files": args.files,
                    "auto_git": args.auto_git,
                }
            )
        )
    elif args.commit_command == "update":
        run_local_command(
            lambda: update_commit(
                args.id,
                {
                    "title": args.title,
                    "summary": args.summary,
                    "evidence_summary": args.evidence_summary,
                    "review_notes": args.review_notes,
                    "branch": args.branch,
                    "commit_hash": args.commit_hash,
                    "task_id": args.task_id,
                    "decision_id": args.decision_id,
                    "status": args.status,
                    "test_status": args.test_status,
                    "review_status": args.review_status,
                    "files": args.files,
                    "auto_git": args.auto_git,
                    "clear_task_id": args.clear_task_id,
                    "clear_decision_id": args.clear_decision_id,
                },
            )
        )
