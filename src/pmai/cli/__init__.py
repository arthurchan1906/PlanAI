from __future__ import annotations

from .parser import build_parser
from .commands import (
    handle_brief,
    handle_canon,
    handle_code,
    handle_commit,
    handle_daily,
    handle_decision,
    handle_doc,
    handle_feedback,
    handle_idea,
    handle_link,
    handle_principle,
    handle_task,
    handle_vision,
    init_project,
    run_local_command,
    run_remote_command,
    show_doctor,
    show_help_text,
    show_info,
)


def main() -> None:
    parser = build_parser()
    args = parser.parse_args()
    if args.command == "init":
        init_project(args)
    elif args.command == "help":
        show_help_text()
    elif args.command == "info":
        from pmai.store import describe_runtime
        run_local_command(lambda: describe_runtime())
    elif args.command == "doctor":
        show_doctor()
    elif args.command == "status":
        from pmai.store import get_status_snapshot
        run_local_command(get_status_snapshot)
    elif args.command == "inbox":
        from pmai.store import get_inbox_summary
        run_local_command(get_inbox_summary)
    elif args.command == "code":
        handle_code(args)
    elif args.command == "brief":
        handle_brief(args)
    elif args.command == "canon":
        handle_canon(args)
    elif args.command == "vision":
        handle_vision(args)
    elif args.command == "roadmap":
        handle_roadmap(args)
    elif args.command == "principle":
        handle_principle(args)
    elif args.command == "link":
        handle_link(args)
    elif args.command == "decision":
        handle_decision(args)
    elif args.command == "idea":
        handle_idea(args)
    elif args.command == "task":
        handle_task(args)
    elif args.command == "commit":
        handle_commit(args)
    elif args.command == "daily":
        handle_daily(args)
    elif args.command == "docs":
        handle_doc(args)
    elif args.command == "feedback":
        handle_feedback(args)


def handle_roadmap(args) -> None:
    if args.roadmap_command == "list":
        from pmai.store import list_roadmaps
        run_local_command(lambda: {"roadmaps": list_roadmaps(args.vision_id or None)})
    elif args.roadmap_command == "show":
        from pmai.store import get_roadmap
        run_local_command(lambda: get_roadmap(args.id))
    elif args.roadmap_command == "add":
        from pmai.store import create_roadmap
        run_local_command(lambda: create_roadmap({
            "title": args.title,
            "target_date": args.target_date,
            "vision_id": args.vision_id,
            "status": args.status,
            "priority": args.priority,
        }))
    elif args.roadmap_command == "update":
        from pmai.store import update_roadmap
        run_local_command(lambda: update_roadmap(args.id, {
            "title": args.title,
            "status": args.status,
            "priority": args.priority,
        }))
