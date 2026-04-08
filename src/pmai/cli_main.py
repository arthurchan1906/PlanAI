from __future__ import annotations

import argparse
import json
import sys
from typing import Any

from .bootstrap import bootstrap_project_db
from .feedback_api import add_feedback, list_feedback
from .store import (
    append_daily_note,
    create_commit,
    create_decision,
    create_idea,
    create_task,
    describe_runtime,
    fetch_canon,
    get_config_path,
    get_runtime_dir,
    list_commits,
    list_decisions,
    list_ideas,
    list_tasks,
    review_idea,
    save_runtime_config,
    update_canon,
    update_commit,
    update_task,
)
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


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="AIPM CLI")
    subparsers = parser.add_subparsers(dest="command", required=True)
    init_parser = subparsers.add_parser("init")
    init_parser.add_argument("--host", default="")
    init_parser.add_argument("--port", type=int, default=None)
    subparsers.add_parser("help")
    subparsers.add_parser("info")
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
        show_info()
    elif args.command == "canon" and args.canon_command == "show":
        show_canon()
    elif args.command == "canon" and args.canon_command == "update":
        print(json.dumps(update_canon({"decision_id": args.decision_id, "product_goal": args.product_goal, "engineering_focus": args.engineering_focus, "architecture": args.architecture, "add_scope": args.add_scope, "add_avoid": args.add_avoid}), ensure_ascii=False, indent=2))
    elif args.command == "decision" and args.decision_command == "list":
        print(json.dumps({"decisions": list_decisions()}, ensure_ascii=False, indent=2))
    elif args.command == "decision" and args.decision_command == "add":
        print(json.dumps(create_decision({"title": args.title, "background": args.background, "decision": args.decision, "status": args.status}), ensure_ascii=False, indent=2))
    elif args.command == "idea" and args.idea_command == "list":
        print(json.dumps({"ideas": list_ideas(args.status or None)}, ensure_ascii=False, indent=2))
    elif args.command == "idea" and args.idea_command == "capture":
        print(json.dumps(create_idea({"title": args.title, "summary": args.summary, "impact": args.impact, "source": args.source, "canon_conflict": args.canon_conflict}), ensure_ascii=False, indent=2))
    elif args.command == "idea" and args.idea_command == "review":
        print(json.dumps(review_idea(args.id, args.status, args.note), ensure_ascii=False, indent=2))
    elif args.command == "task" and args.task_command == "list":
        print(json.dumps({"tasks": list_tasks(args.status or None)}, ensure_ascii=False, indent=2))
    elif args.command == "task" and args.task_command == "add":
        print(json.dumps(create_task({"title": args.title, "priority": args.priority, "status": args.status, "phase": args.phase, "acceptance": args.acceptance}), ensure_ascii=False, indent=2))
    elif args.command == "task" and args.task_command == "update":
        print(
            json.dumps(
                update_task(
                    args.id,
                    args.status,
                    args.note,
                    allow_without_commit=args.allow_without_commit,
                ),
                ensure_ascii=False,
                indent=2,
            )
        )
    elif args.command == "commit" and args.commit_command == "list":
        print(
            json.dumps(
                {
                    "commits": list_commits(
                        args.status or None,
                        args.task_id or None,
                        args.decision_id or None,
                    )
                },
                ensure_ascii=False,
                indent=2,
            )
        )
    elif args.command == "commit" and args.commit_command == "add":
        print(json.dumps(create_commit({"title": args.title, "summary": args.summary, "branch": args.branch, "commit_hash": args.commit_hash, "task_id": args.task_id or None, "decision_id": args.decision_id or None, "status": args.status, "test_status": args.test_status, "review_status": args.review_status, "files": args.files, "auto_git": args.auto_git}), ensure_ascii=False, indent=2))
    elif args.command == "commit" and args.commit_command == "update":
        print(json.dumps(update_commit(args.id, {"title": args.title, "summary": args.summary, "branch": args.branch, "commit_hash": args.commit_hash, "task_id": args.task_id, "decision_id": args.decision_id, "status": args.status, "test_status": args.test_status, "review_status": args.review_status, "files": args.files, "clear_task_id": args.clear_task_id, "clear_decision_id": args.clear_decision_id}), ensure_ascii=False, indent=2))
    elif args.command == "daily" and args.daily_command == "show":
        from .store import get_daily_note
        print(json.dumps(get_daily_note(args.date), ensure_ascii=False, indent=2))
    elif args.command == "daily" and args.daily_command == "close":
        print(json.dumps(append_daily_note({"completed": args.completed, "problems": args.problems, "risks": args.risks, "next": args.next}), ensure_ascii=False, indent=2))
    elif args.command == "feedback" and args.feedback_command == "list":
        run_remote_command(lambda: list_feedback(args.base_url))
    elif args.command == "feedback" and args.feedback_command == "add":
        run_remote_command(lambda: add_feedback(args.label, args.content, args.base_url))


if __name__ == "__main__":
    main()
