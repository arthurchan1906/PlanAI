"""Backward compatibility layer - re-exports from .web package."""
from __future__ import annotations

from .web import PMAIRequestHandler, create_server
from .web.handlers.bootstrap import build_web_bootstrap
from .web.handlers.commits import build_web_commit_detail
from .web.handlers.decisions import build_web_decision_detail
from .web.handlers.tasks import build_web_task_detail

__all__ = [
    "PMAIRequestHandler",
    "create_server",
    "build_web_bootstrap",
    "build_web_task_detail",
    "build_web_commit_detail",
    "build_web_decision_detail",
]
