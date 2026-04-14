from __future__ import annotations

from pathlib import Path


def resolve_web_dir() -> Path:
    package_dist = Path(__file__).resolve().parents[1] / "ui" / "dist"
    repo_dist = Path(__file__).resolve().parents[3] / "ui" / "dist"

    if repo_dist.exists():
        return repo_dist
    return package_dist
