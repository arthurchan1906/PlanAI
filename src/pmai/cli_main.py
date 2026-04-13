"""Backward compatibility layer for the pre-refactor CLI module path."""
from __future__ import annotations

from .cli import build_parser, main

__all__ = ["main", "build_parser"]


if __name__ == "__main__":
    main()
