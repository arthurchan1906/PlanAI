from __future__ import annotations

from pathlib import Path
from typing import Dict, List

from .store.config import get_project_root

AGENT_GUIDE = """Agent instruction file for AI tooling. This is not a product or project document.

AI startup flow:
1. Run `aipmc start`
2. Run `aipmc search "<topic>"`
3. If related work exists, use `aipmc task show`, `aipmc plan show`, `aipmc decision show`, or `aipmc task note` first
4. Only use `add` when existing context clearly does not fit
5. At the end, run `aipmc session close --from-commits --from-tasks`
"""


def build_agent_instruction_packet() -> Dict[str, object]:
    return {
        "role": "ai_instructions",
        "message": "Direct startup instructions for AI tooling. Use this instead of relying on a generated AGENTS.md file.",
        "startup_flow": [
            {
                "step": 1,
                "command": "aipmc start",
                "reason": "Load the current PMAI mainline, active context, and recommended next action.",
            },
            {
                "step": 2,
                "command": "aipmc search \"<topic>\"",
                "reason": "Check whether related task/plan/decision/doc context already exists.",
            },
            {
                "step": 3,
                "command": "aipmc next",
                "reason": "Refresh the currently recommended action after reading the context.",
            },
        ],
        "rules": [
            "If related work already exists, prefer `show`, `update`, or `task note` before using any `add` command.",
            "Only create a new task, plan, or decision when the existing context clearly does not fit.",
            "Close the working session with `aipmc session close --from-commits --from-tasks`.",
        ],
        "notes": [
            "AGENTS.md is optional helper text for local AI tooling; it is not the primary user-facing interface.",
            "This command always reflects the currently installed aipmc version.",
            "If `aipmc` is not on PATH, use `python -m pmai agent instructions`.",
        ],
    }


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
