from __future__ import annotations

import argparse
import json
import os
import platform
import shutil
import sys
from typing import Any

from .bootstrap import bootstrap_project_db
from .feedback_api import add_feedback, list_feedback
from .store import (
    append_daily_note,
    audit_docs,
    create_commit,
    create_decision,
    create_idea,
    create_task,
    describe_runtime,
    fetch_canon,
    get_config_path,
    get_inbox_summary,
    get_runtime_dir,
    get_db_path,
    list_commits,
    list_decisions,
    list_doc_records,
    list_ideas,
    list_tasks,
    load_runtime_config,
    replace_daily_note,
    review_idea,
    save_runtime_config,
    update_decision_status,
    update_doc_record,
    update_canon,
    update_commit,
    update_task,
)
from .feedback_api import get_feedback_base_url
from .usage_guide import build_usage_markdown, write_usage_file


def init_project(args: argparse.Namespace) -> None:
    get_runtime_dir(create=True)
    if args.port is not None or args.host:
        payload: dict[str, Any] = {}
        if args.host:
            payload["web_host"] = args.host
        if args.port is not None:
            payload["web_port"] = args.port
        save_runtime_config(payload)
    elif not get_config_path().exists():
        save_runtime_config({})
    bootstrap_project_db()
    write_usage_file()
    print("Initialized .pmai")


def show_info() -> None:
    print(json.dumps(describe_runtime(), ensure_ascii=False, indent=2))


def show_canon() -> None:
    print(json.dumps(fetch_canon(), ensure_ascii=False, indent=2))


def show_help_text() -> None:
    print(build_usage_markdown())


def show_doctor() -> None:
    runtime = describe_runtime()
    config = load_runtime_config()
    db_path = get_db_path()
    runtime_dir = get_runtime_dir()
    print(
        json.dumps(
            {
                "python_executable": sys.executable,
                "python_version": sys.version.split()[0],
                "platform": platform.platform(),
                "cwd": os.getcwd(),
                "command_resolution": {
                    "aipmc": shutil.which("aipmc"),
                    "aipmv": shutil.which("aipmv"),
                    "python": shutil.which("python"),
                },
                "runtime": runtime,
                "config": config,
                "feedback_base_url": get_feedback_base_url(),
                "filesystem": {
                    "runtime_dir_exists": runtime_dir.exists(),
                    "runtime_dir_writable": runtime_dir.exists() and os.access(runtime_dir, os.W_OK),
                    "db_parent_exists": db_path.parent.exists(),
                    "db_parent_writable": db_path.parent.exists() and os.access(db_path.parent, os.W_OK),
                },
                "hints": [
                    "Use pipx for CLI-style installation if commands are not stable across Python environments.",
                    "If aipmc is not found, fallback to `python -m pmai ...`.",
                    "If runtime_dir_writable is false, writes to .pmai may fail in container or sandbox environments.",
                ],
            },
            ensure_ascii=False,
            indent=2,
        )
    )


def run_remote_command(fn) -> None:
    try:
        print(json.dumps(fn(), ensure_ascii=False, indent=2))
    except Exception as exc:
        print(
            json.dumps(
                {
                    "ok": False,
                    "detail": str(exc),
                },
                ensure_ascii=False,
                indent=2,
            ),
            file=sys.stderr,
        )
        raise SystemExit(2) from None


def run_local_command(fn, *, init_hint: str = "Run `aipmc init` in the project root first.") -> None:
    try:
        print(json.dumps(fn(), ensure_ascii=False, indent=2))
    except FileNotFoundError as exc:
        print(
            json.dumps(
                {
                    "ok": False,
                    "error": "runtime_not_initialized",
                    "detail": str(exc),
                    "hint": init_hint,
                },
                ensure_ascii=False,
                indent=2,
            ),
            file=sys.stderr,
        )
        raise SystemExit(2) from None
    except KeyError as exc:
        print(
            json.dumps(
                {
                    "ok": False,
                    "error": "not_found",
                    "detail": str(exc),
                },
                ensure_ascii=False,
                indent=2,
            ),
            file=sys.stderr,
        )
        raise SystemExit(2) from None
    except Exception as exc:
        print(
            json.dumps(
                {
                    "ok": False,
                    "error": "command_failed",
                    "detail": str(exc),
                },
                ensure_ascii=False,
                indent=2,
            ),
            file=sys.stderr,
        )
        raise SystemExit(2) from None


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="AIPM CLI")
    subparsers = parser.add_subparsers(dest="command", required=True)
    init_parser = subparsers.add_parser("init")
    init_parser.add_argument("--host", default="")
    init_parser.add_argument("--port", type=int, default=None)
    subparsers.add_parser("help")
    subparsers.add_parser("info")
    subparsers.add_parser("doctor")
    subparsers.add_parser("inbox")
    canon = subparsers.add_parser("canon").add_subparsers(dest="canon_command", required=True)
    canon.add_parser("show")
    canon_update = canon.add_parser("update")
    canon_update.add_argument("--decision-id", required=True)
    canon_update.add_argument("--product-goal", default="")
    canon_update.add_argument("--engineering-focus", default="")
    canon_update.add_argument("--architecture", default="")
    canon_update.add_argument("--add-scope", nargs="*", default=[])
    canon_update.add_argument("--add-avoid", nargs="*", default=[])
    decision = subparsers.add_parser("decision").add_subparsers(dest="decision_command", required=True)
    decision.add_parser("list")
    decision_add = decision.add_parser("add")
    decision_add.add_argument("--title", required=True)
    decision_add.add_argument("--background", required=True)
    decision_add.add_argument("--decision", required=True)
    decision_add.add_argument("--status", default="proposed")
    decision_review = decision.add_parser("review")
    decision_review.add_argument("--id", required=True)
    decision_review.add_argument("--status", required=True)
    idea = subparsers.add_parser("idea").add_subparsers(dest="idea_command", required=True)
    idea_list = idea.add_parser("list")
    idea_list.add_argument("--status", default="")
    idea_capture = idea.add_parser("capture")
    idea_capture.add_argument("--title", required=True)
    idea_capture.add_argument("--summary", required=True)
    idea_capture.add_argument("--impact", default="")
    idea_capture.add_argument("--source", default="manual")
    idea_capture.add_argument("--canon-conflict", action="store_true", dest="canon_conflict")
    idea_review = idea.add_parser("review")
    idea_review.add_argument("--id", required=True)
    idea_review.add_argument("--status", required=True)
    idea_review.add_argument("--note", default="")
    task = subparsers.add_parser("task").add_subparsers(dest="task_command", required=True)
    task_list = task.add_parser("list")
    task_list.add_argument("--status", default="")
    task_add = task.add_parser("add")
    task_add.add_argument("--title", required=True)
    task_add.add_argument("--priority", default="P1")
    task_add.add_argument("--status", default="todo")
    task_add.add_argument("--phase", default="general")
    task_add.add_argument("--acceptance", nargs="*", default=[])
    task_update = task.add_parser("update")
    task_update.add_argument("--id", required=True)
    task_update.add_argument("--status", required=True)
    task_update.add_argument("--note", default="")
    task_update.add_argument("--allow-without-commit", action="store_true", dest="allow_without_commit")
    commit = subparsers.add_parser("commit").add_subparsers(dest="commit_command", required=True)
    commit_list = commit.add_parser("list")
    commit_list.add_argument("--status", default="")
    commit_list.add_argument("--task-id", default="", dest="task_id")
    commit_list.add_argument("--decision-id", default="", dest="decision_id")
    commit_add = commit.add_parser("add")
    commit_add.add_argument("--title", required=True)
    commit_add.add_argument("--summary", default="")
    commit_add.add_argument("--branch", default="")
    commit_add.add_argument("--commit-hash", default="", dest="commit_hash")
    commit_add.add_argument("--task-id", default="", dest="task_id")
    commit_add.add_argument("--decision-id", default="", dest="decision_id")
    commit_add.add_argument("--status", default="draft")
    commit_add.add_argument("--test-status", default="not_run", dest="test_status")
    commit_add.add_argument("--review-status", default="pending", dest="review_status")
    commit_add.add_argument("--files", nargs="*", default=[])
    commit_add.add_argument("--auto-git", action="store_true", dest="auto_git")
    commit_update = commit.add_parser("update")
    commit_update.add_argument("--id", required=True)
    commit_update.add_argument("--title", default=None)
    commit_update.add_argument("--summary", default=None)
    commit_update.add_argument("--branch", default=None)
    commit_update.add_argument("--commit-hash", default=None, dest="commit_hash")
    commit_update.add_argument("--task-id", default=None, dest="task_id")
    commit_update.add_argument("--decision-id", default=None, dest="decision_id")
    commit_update.add_argument("--status", default=None)
    commit_update.add_argument("--test-status", default=None, dest="test_status")
    commit_update.add_argument("--review-status", default=None, dest="review_status")
    commit_update.add_argument("--files", nargs="*", default=None)
    commit_update.add_argument("--clear-task-id", action="store_true", dest="clear_task_id")
    commit_update.add_argument("--clear-decision-id", action="store_true", dest="clear_decision_id")
    daily = subparsers.add_parser("daily").add_subparsers(dest="daily_command", required=True)
    daily_show = daily.add_parser("show")
    daily_show.add_argument("--date", default=None)
    daily_close = daily.add_parser("close")
    daily_close.add_argument("--completed", nargs="*", default=[])
    daily_close.add_argument("--problems", nargs="*", default=[])
    daily_close.add_argument("--risks", nargs="*", default=[])
    daily_close.add_argument("--next", nargs="*", default=[])
    daily_replace = daily.add_parser("replace")
    daily_replace.add_argument("--date", default=None)
    daily_replace.add_argument("--completed", nargs="*", default=[])
    daily_replace.add_argument("--problems", nargs="*", default=[])
    daily_replace.add_argument("--risks", nargs="*", default=[])
    daily_replace.add_argument("--next", nargs="*", default=[])
    docs = subparsers.add_parser("docs").add_subparsers(dest="docs_command", required=True)
    docs_list = docs.add_parser("list")
    docs_list.add_argument("--status", default="")
    docs_list.add_argument("--layer", default="")
    docs_update = docs.add_parser("update")
    docs_update.add_argument("--path", required=True)
    docs_update.add_argument("--type", default="")
    docs_update.add_argument("--status", default="")
    docs_update.add_argument("--layer", default="")
    docs_update.add_argument("--source-of-truth", action="store_true", dest="source_of_truth")
    docs_update.add_argument("--clear-source-of-truth", action="store_true", dest="clear_source_of_truth")
    docs_update.add_argument("--superseded-by", default=None, dest="superseded_by")
    docs_update.add_argument("--create", action="store_true", dest="create")
    docs_audit = docs.add_parser("audit")
    feedback = subparsers.add_parser("feedback").add_subparsers(dest="feedback_command", required=True)
    feedback_list = feedback.add_parser("list")
    feedback_list.add_argument("--base-url", default=None, dest="base_url")
    feedback_add = feedback.add_parser("add")
    feedback_add.add_argument("--label", required=True, choices=["bug", "suggestion"])
    feedback_add.add_argument("--content", required=True)
    feedback_add.add_argument("--base-url", default=None, dest="base_url")
    return parser


def main() -> None:
    parser = build_parser()
    args = parser.parse_args()
    if args.command == "init":
        init_project(args)
    elif args.command == "help":
        show_help_text()
    elif args.command == "info":
        run_local_command(lambda: describe_runtime())
    elif args.command == "doctor":
        show_doctor()
    elif args.command == "inbox":
        run_local_command(get_inbox_summary)
    elif args.command == "canon" and args.canon_command == "show":
        run_local_command(fetch_canon)
    elif args.command == "canon" and args.canon_command == "update":
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
    elif args.command == "decision" and args.decision_command == "list":
        run_local_command(lambda: {"decisions": list_decisions()})
    elif args.command == "decision" and args.decision_command == "add":
        run_local_command(
            lambda: create_decision(
                {
                    "title": args.title,
                    "background": args.background,
                    "decision": args.decision,
                    "status": args.status,
                }
            )
        )
    elif args.command == "decision" and args.decision_command == "review":
        run_local_command(lambda: update_decision_status(args.id, args.status))
    elif args.command == "idea" and args.idea_command == "list":
        run_local_command(lambda: {"ideas": list_ideas(args.status or None)})
    elif args.command == "idea" and args.idea_command == "capture":
        run_local_command(
            lambda: create_idea(
                {
                    "title": args.title,
                    "summary": args.summary,
                    "impact": args.impact,
                    "source": args.source,
                    "canon_conflict": args.canon_conflict,
                }
            )
        )
    elif args.command == "idea" and args.idea_command == "review":
        run_local_command(lambda: review_idea(args.id, args.status, args.note))
    elif args.command == "task" and args.task_command == "list":
        run_local_command(lambda: {"tasks": list_tasks(args.status or None)})
    elif args.command == "task" and args.task_command == "add":
        run_local_command(
            lambda: create_task(
                {
                    "title": args.title,
                    "priority": args.priority,
                    "status": args.status,
                    "phase": args.phase,
                    "acceptance": args.acceptance,
                }
            )
        )
    elif args.command == "task" and args.task_command == "update":
        run_local_command(
            lambda: update_task(
                    args.id,
                    args.status,
                    args.note,
                    allow_without_commit=args.allow_without_commit,
                )
        )
    elif args.command == "commit" and args.commit_command == "list":
        run_local_command(
            lambda: {
                    "commits": list_commits(
                        args.status or None,
                        args.task_id or None,
                        args.decision_id or None,
                    )
                },
        )
    elif args.command == "commit" and args.commit_command == "add":
        run_local_command(
            lambda: create_commit(
                {
                    "title": args.title,
                    "summary": args.summary,
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
    elif args.command == "commit" and args.commit_command == "update":
        run_local_command(
            lambda: update_commit(
                args.id,
                {
                    "title": args.title,
                    "summary": args.summary,
                    "branch": args.branch,
                    "commit_hash": args.commit_hash,
                    "task_id": args.task_id,
                    "decision_id": args.decision_id,
                    "status": args.status,
                    "test_status": args.test_status,
                    "review_status": args.review_status,
                    "files": args.files,
                    "clear_task_id": args.clear_task_id,
                    "clear_decision_id": args.clear_decision_id,
                },
            )
        )
    elif args.command == "daily" and args.daily_command == "show":
        from .store import get_daily_note
        run_local_command(lambda: get_daily_note(args.date))
    elif args.command == "daily" and args.daily_command == "close":
        run_local_command(
            lambda: append_daily_note(
                {
                    "completed": args.completed,
                    "problems": args.problems,
                    "risks": args.risks,
                    "next": args.next,
                }
            )
        )
    elif args.command == "daily" and args.daily_command == "replace":
        run_local_command(
            lambda: replace_daily_note(
                {
                    "completed": args.completed,
                    "problems": args.problems,
                    "risks": args.risks,
                    "next": args.next,
                },
                args.date,
            )
        )
    elif args.command == "docs" and args.docs_command == "list":
        run_local_command(lambda: {"records": list_doc_records(args.status or None, args.layer or None)})
    elif args.command == "docs" and args.docs_command == "update":
        payload = {
            "path": args.path,
            "create": args.create,
            "source_of_truth": args.source_of_truth,
            "clear_source_of_truth": args.clear_source_of_truth,
        }
        if args.type:
            payload["type"] = args.type
        if args.status:
            payload["status"] = args.status
        if args.layer:
            payload["layer"] = args.layer
        if args.superseded_by is not None:
            payload["superseded_by"] = args.superseded_by
        run_local_command(lambda: update_doc_record(payload))
    elif args.command == "docs" and args.docs_command == "audit":
        run_local_command(audit_docs)
    elif args.command == "feedback" and args.feedback_command == "list":
        run_remote_command(lambda: list_feedback(args.base_url))
    elif args.command == "feedback" and args.feedback_command == "add":
        run_remote_command(lambda: add_feedback(args.label, args.content, args.base_url))


if __name__ == "__main__":
    main()
