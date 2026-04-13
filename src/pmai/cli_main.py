"""Backward compatibility layer - redirects to .cli package."""
from __future__ import annotations

from .cli import main
from .cli.parser import build_parser

__all__ = ["main", "build_parser"]

if __name__ == "__main__":
    main()
