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
aipmc doctor
aipmc inbox
aipmc canon show
aipmc docs list
aipmc docs audit
aipmc feedback list
aipmc decision review --id <decision-id> --status accepted
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
aipmc decision review --id <decision-id> --status accepted
aipmc commit add --title "Implement task" --summary "..." --auto-git
aipmc commit update --id <commit-id> --status committed --review-status approved
aipmc task update --id <task-id> --status done
aipmc idea capture --title "Optimization idea" --summary "..."
aipmc daily close --completed "..." --problems "..." --risks "..." --next "..."
aipmc daily replace --completed "..." --problems "..." --risks "..." --next "..."
aipmc feedback add --label bug --content "登录页面验证码不显示"
aipmc feedback add --label suggestion --content "建议任务列表支持按负责人筛选"
```

Status commands:

```bash
aipmc info
aipmc doctor
aipmc inbox
aipmc canon show
aipmc docs list
aipmc docs audit
aipmc task list
aipmc commit list
aipmc task list --status done
aipmc commit list --status committed --task-id <task-id>
aipmc decision list
aipmc idea list
aipmc daily show
aipmc feedback list
```

Web:

```bash
aipmv
```

`aipmc inbox` is the shortest way to inspect pending review and governance items before opening the web UI.

Default address:
- `http://127.0.0.1:8011/`

Notes:
- pipx is recommended for CLI-style installation across Windows, Linux, and macOS.
- Virtual python environment must be activated before using `aipmc` command or you can run `/path/to/python3 -m pmai help...command`
- Privilege must be granted if any write or modify operation is requested in Container environment.
- Runtime data is stored under `.pmai/` in the project.
- After installing from PyPI, use `aipmc` and `aipmv` directly.
- If the shell cannot find those commands, use `python -m pmai` and `python -m pmai.run`.
- Remote feedback API defaults to `http://43.167.206.218:8080` and can be overridden with `--base-url` or `PMAI_FEEDBACK_BASE_URL`.
- Feedback label only supports `bug` and `suggestion`. Remote request failures return JSON errors and exit quickly.
- Marking a task `done` requires at least one linked approved commit (`status=committed|merged` and `review_status=approved`). Use `--allow-without-commit` only for emergency override.
"""


def get_usage_path(start: Path | None = None) -> Path:
    return get_runtime_dir(start, create=True) / USAGE_FILENAME


def write_usage_file(start: Path | None = None) -> Path:
    path = get_usage_path(start)
    path.write_text(build_usage_markdown(), encoding="utf-8")
    return path
