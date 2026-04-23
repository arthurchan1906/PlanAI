from __future__ import annotations

from pathlib import Path
from typing import Dict, List

from .store.config import get_project_root

AGENT_GUIDE = """Before coding, run `aipmc start`.
Use the current PMAI task/doc context before editing files.
Before creating new docs/tasks, run `aipmc search "<topic>"`.
At the end, run `aipmc session close --from-commits --from-tasks`.
"""


def write_agent_guide(*, force: bool = False, start: Path | None = None) -> Dict[str, List[str]]:
    project_root = get_project_root(start)
    targets = [project_root / "AGENTS.md"]
    written: List[str] = []
    skipped: List[str] = []
    for path in targets:
        if path.exists() and not force:
            skipped.append(str(path))
            continue
        path.write_text(AGENT_GUIDE, encoding="utf-8")
        written.append(str(path))
    return {"written": written, "skipped": skipped}
