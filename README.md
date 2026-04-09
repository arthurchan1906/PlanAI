# AIPM CLI

`aipm-cli` is a local project management tool for AI coding workflows.

AI-first product baseline:
- `doc/ai-first-product-baseline-v1.md`

It installs these command-line entry points:

- `aipmc`: CLI management commands
- `aipmv`: local web viewer

## Install

```bash
python -m pip install aipm-cli
```

Recommended for command-line usage:

```bash
python -m pip install --user pipx
python -m pipx ensurepath
pipx install aipm-cli
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
aipmc doctor
aipmc inbox
aipmc canon show
aipmc docs list
aipmc docs audit
aipmc feedback list
aipmc decision review --id <decision-id> --status accepted
aipmc feedback add --label bug --content "登录页面验证码不显示"
aipmc task list --status in_progress
aipmc commit list --task-id <task-id>
aipmc commit add --title "Implement task" --summary "..." --auto-git
aipmc commit update --id <commit-id> --status committed --review-status approved
aipmc task update --id <task-id> --status done
aipmc daily replace --completed "..." --problems "..." --risks "..." --next "..."
aipmv
```

`aipmc inbox` aggregates the current items that usually still need human attention:
- proposed decisions
- accepted decisions that still imply canon follow-up
- committed changes waiting for review
- blocking doc-governance issues

Remote feedback commands:

```bash
aipmc feedback list
aipmc feedback add --label bug --content "登录页面验证码不显示"
aipmc feedback add --label suggestion --content "建议任务列表支持按负责人筛选"
```

Optional override:
- `--base-url http://43.167.206.218:8080`
- `PMAI_FEEDBACK_BASE_URL=http://43.167.206.218:8080`
- `label` only supports `bug` and `suggestion`
- Remote request failures return a JSON error and exit quickly instead of hanging

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
