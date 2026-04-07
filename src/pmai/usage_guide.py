from __future__ import annotations

from pathlib import Path

from .store import get_runtime_dir


USAGE_FILENAME = "USAGE.md"


def build_usage_markdown() -> str:
    return """# AIPM CLI Usage

AIPM CLI is for local AI coding projects.
Read these first:
- `.pmai/USAGE.md`
- `.pmai/data/pmai.db`
- `README.md`

Common commands:

```bash
aipmc help
aipmc info
aipmc canon show
aipmc task list
aipmc decision list
aipmc commit list
aipmc task list --status in_progress
aipmc commit list --task-id <task-id>
aipmc idea list
aipmc daily show
aipmv
```

Initialize:

```bash
aipmc init
```

This creates:
- `.pmai/config.json`
- `.pmai/data/pmai.db`
- `.pmai/USAGE.md`

Write commands:

```bash
aipmc task add --title "Implement xxx" --acceptance "Complete yyy"
aipmc task update --id <task-id> --status in_progress
aipmc decision add --title "Choose approach A" --background "..." --decision "..."
aipmc commit add --title "Implement task" --summary "..." --auto-git
aipmc commit update --id <commit-id> --status committed --review-status approved
aipmc task update --id <task-id> --status done
aipmc idea capture --title "Optimization idea" --summary "..."
aipmc daily close --completed "..." --problems "..." --risks "..." --next "..."
```

Status commands:

```bash
aipmc info
aipmc canon show
aipmc task list
aipmc commit list
aipmc task list --status done
aipmc commit list --status committed --task-id <task-id>
aipmc decision list
aipmc idea list
aipmc daily show
```

Web:

```bash
aipmv
```

Default address:
- `http://127.0.0.1:8011/`

Notes:
- Runtime data is stored under `.pmai/` in the project.
- After installing from PyPI, use `aipmc` and `aipmv` directly.
- If the shell cannot find those commands, use `python -m pmai` and `python -m pmai.run`.
- Marking a task `done` requires at least one linked approved commit (`status=committed|merged` and `review_status=approved`). Use `--allow-without-commit` only for emergency override.
"""


def get_usage_path(start: Path | None = None) -> Path:
    return get_runtime_dir(start, create=True) / USAGE_FILENAME


def write_usage_file(start: Path | None = None) -> Path:
    path = get_usage_path(start)
    path.write_text(build_usage_markdown(), encoding="utf-8")
    return path
