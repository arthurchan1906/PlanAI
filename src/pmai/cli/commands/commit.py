from __future__ import annotations

import argparse

from ...store import (
    create_commit,
    get_commit,
    list_commits,
    update_commit,
)
from .creation_guidance import escape_cli_text
from .followup_guidance import build_guided_response
from .project import run_local_command


def _compact_commit(commit: dict) -> dict:
    files = commit.get("files", [])
    return {
        "id": commit["id"],
        "title": commit["title"],
        "status": commit["status"],
        "test_status": commit["test_status"],
        "review_status": commit["review_status"],
        "task_id": commit["task_id"],
        "decision_id": commit["decision_id"],
        "file_count": len(files),
        "files": files,
        "created_at": commit["created_at"],
        "updated_at": commit["updated_at"],
    }


def _build_commit_payload(commit: dict, *, message: str) -> dict:
    commit_id = commit["id"]
    context_updates = [
        {
            "type": "commit_record",
            "commit_id": commit_id,
            "status": commit.get("status"),
            "review_status": commit.get("review_status"),
            "test_status": commit.get("test_status"),
            "summary": f"Commit `{commit.get('title')}` is now tracked as `{commit.get('status')}`.",
        }
    ]
    if commit.get("task_id"):
        context_updates.append(
            {
                "type": "task_link",
                "commit_id": commit_id,
                "task_id": commit["task_id"],
                "summary": f"Commit is linked to task `{commit['task_id']}`.",
            }
        )
    if commit.get("decision_id"):
        context_updates.append(
            {
                "type": "decision_link",
                "commit_id": commit_id,
                "decision_id": commit["decision_id"],
                "summary": f"Commit is linked to decision `{commit['decision_id']}`.",
            }
        )

    next_steps = [
        {
            "command": f"aipmc commit show --id {commit_id}",
            "reason": "Inspect the tracked commit and confirm linked task/decision context.",
        }
    ]
    if commit.get("status") == "draft":
        next_steps.append(
            {
                "command": f"aipmc commit update --id {commit_id} --status committed --review-status approved",
                "reason": "Promote the commit after review when the code slice is ready.",
            }
        )
    elif commit.get("task_id") and commit.get("review_status") == "approved" and commit.get("test_status") == "passed":
        next_steps.append(
            {
                "command": f"aipmc task update --id {commit['task_id']} --status done",
                "reason": "The linked task can now move toward closure.",
            }
        )
    elif commit.get("task_id"):
        next_steps.append(
            {
                "command": f"aipmc commit list --task-id {commit['task_id']}",
                "reason": "Check the full commit set linked to the task before closing work.",
            }
        )
    else:
        next_steps.append(
            {
                "command": "aipmc next",
                "reason": "Refresh the current recommendation after recording the commit.",
            }
        )

    return build_guided_response(
        message=message,
        payload={"commit": commit},
        context_updates=context_updates,
        next_steps=next_steps,
    )


def handle_commit(args: argparse.Namespace) -> None:
    if args.commit_command == "list":
        run_local_command(
            lambda: (lambda commits: {
                    "commits": [_compact_commit(item) for item in commits] if args.compact else commits,
                    "count": len(commits),
                })(
                    list_commits(
                        args.status or None,
                        args.task_id or None,
                        args.decision_id or None,
                        args.since or None,
                        args.limit,
                    )
                ),
        )
    elif args.commit_command == "show":
        run_local_command(lambda: get_commit(args.id))
    elif args.commit_command == "add":
        run_local_command(
            lambda: _build_commit_payload(create_commit(
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
            ), message="Commit recorded and execution context updated.")
        )
    elif args.commit_command == "update":
        run_local_command(
            lambda: _build_commit_payload(update_commit(
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
            ), message="Commit updated and follow-up context refreshed.")
        )
