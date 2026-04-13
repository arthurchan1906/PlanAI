from __future__ import annotations

import argparse

from ...feedback_api import add_feedback, list_feedback
from .project import run_remote_command


def handle_feedback(args: argparse.Namespace) -> None:
    if args.feedback_command == "list":
        run_remote_command(lambda: list_feedback(args.base_url))
    elif args.feedback_command == "add":
        run_remote_command(lambda: add_feedback(args.label, args.content, args.base_url))
