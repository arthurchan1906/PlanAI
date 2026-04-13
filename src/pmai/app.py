"""Backward compatibility layer for the pre-refactor web app module."""
from __future__ import annotations

from .web_server import PMAIRequestHandler, create_server

__all__ = ["PMAIRequestHandler", "create_server"]
