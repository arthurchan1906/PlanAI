from __future__ import annotations

import argparse

from ...store import build_brief
from .project import run_local_command


def handle_brief(args: argparse.Namespace) -> None:
    run_local_command(lambda: build_brief(args.brief_command))
