"""Backward compatibility layer for the pre-refactor web runner."""
from __future__ import annotations

from .run_server import main

__all__ = ["main"]


if __name__ == "__main__":
    main()
