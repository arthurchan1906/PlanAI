# AIPM CLI

`aipm-cli` is a local project management tool for AI coding workflows.

It installs these command-line entry points:

- `aipmc`: CLI management commands
- `aipmv`: local web viewer

## Install

```bash
python -m pip install aipm-cli
```

If your shell cannot find `aipmc` or `aipmv` after installation, use the module form instead:

```bash
python -m pmai help
python -m pmai info
python -m pmai canon show
python -m pmai.run
```

This is especially useful on macOS when the Python scripts directory is not in `PATH`.

## Common commands

```bash
aipmc init
aipmc help
aipmc info
aipmc canon show
aipmc task list --status in_progress
aipmc commit list --task-id <task-id>
aipmc commit add --title "Implement task" --summary "..." --auto-git
aipmc commit update --id <commit-id> --status committed --review-status approved
aipmc task update --id <task-id> --status done
aipmv
```

Task governance rule:
- Marking a task `done` requires at least one linked approved commit (`status=committed|merged` and `review_status=approved`).
- Emergency override is available with `aipmc task update --id <task-id> --status done --allow-without-commit`.

## Runtime files

- `.pmai/data/pmai.db`
- `.pmai/config.json`
- `.pmai/USAGE.md`

## Main modules

- `src/pmai/bootstrap.py`
- `src/pmai/cli_main.py`
- `src/pmai/usage_guide.py`
- `src/pmai/store.py`
- `src/pmai/web_server.py`
- `src/pmai/run_server.py`
