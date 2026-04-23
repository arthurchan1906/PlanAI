# AIPM CLI

`aipm-cli` is a local project management tool for AI coding workflows.

AI-first product baseline:
- `doc/ai-first-product-baseline-v1.md`

Current rebuild status:
- [docs/rebuild-status.md](docs/rebuild-status.md)

It installs these entry points:

- `aipmc`: CLI for AI coders and automation
- `aipmv`: local web UI for human review and project management

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

## Role split

- `aipmc` is for AI coders. Use it inside agent workflows, scripts, and terminal-driven implementation.
- `aipmv` is for humans. Use it for review, inspection, and project-manager-facing decisions.
- Do not treat `aipmc` and `aipmv` as interchangeable entry points. They serve different users on purpose.
- AI coders should run `aipmc start` before implementation.

## Common AI coder commands

```bash
aipmc init
aipmc start
aipmc agent guide
aipmc search "topic or feature name"
aipmc help
aipmc info
aipmc context
aipmc next
aipmc handoff
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
aipmc commit list --since today --limit 10 --compact
aipmc commit add --title "Implement task" --summary "..." --auto-git
aipmc commit update --id <commit-id> --status committed --review-status approved
aipmc task update --id <task-id> --status done
aipmc task note --id <task-id> --content "Design note"
aipmc task notes --id <task-id>
aipmc daily replace --completed "..." --problems "..." --risks "..." --next "..."
aipmc daily close --from-commits --from-tasks
aipmc session close --completed "..." --from-commits --from-tasks
```

## Human review entry

```bash
aipmv
```

Use `aipmv` when a human needs to:
- inspect the current mainline
- review decisions, commits, and blockers
- manage canon, docs, and daily notes through the web UI

`aipmc inbox` is still a CLI aggregation for the coding agent, not the primary PM interface:
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
- Marking a task `done` requires at least one linked verified approved commit (`status=committed|merged`, `review_status=approved`, and `test_status=passed`).
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

## Current Direction

PlanAI is currently being rebuilt toward a local `context engineering + governance shell` for AI software delivery.

The current mainline is:

- compressed AI runtime context via `context / next / handoff`
- idea threads as pre-mainline convergence objects
- traceable `idea -> task / decision` conversion
- evidence / review / closure governance around task completion

See [docs/rebuild-status.md](docs/rebuild-status.md) for completed work, remaining work, and the recommended next continuation point.
