from __future__ import annotations

from pathlib import Path

from .store import get_runtime_dir


USAGE_FILENAME = "USAGE.md"


def build_usage_markdown() -> str:
    return """# AIPM - AI Project Manager

## Overview

AIPM is a local project management tool for AI coding workflows. It stores project context, decisions, tasks, and code commits in a local SQLite database (`.pmai/data/pmai.db`).

**Key concepts:**
- **Vision**: Long-term project goals and direction
- **Principle**: Governance rules (technical, governance, quality)
- **Task**: Work items with acceptance criteria
- **Decision**: Recorded choices with context and rationale
- **Commit**: Code changes linked to tasks/decisions
- **Idea**: Captured suggestions for future consideration
- **Canon**: Living document of product goals, engineering focus, and architecture
- **Daily Note**: End-of-day progress summary

---

## For AI Coders: Workflow Guide

### Phase 1: Understand Context (Read-Only)

Before writing code, understand the project state:

```bash
# 1. Check project overview and runtime status
aipmc info
aipmc status

# 2. Read project canon (product goals, engineering focus, architecture)
aipmc canon show

# 3. Review active tasks and pending decisions
aipmc task list --status todo
aipmc task list --status in_progress
aipmc decision list --status proposed

# 4. Check inbox for items needing attention
aipmc inbox

# 5. Review technical principles and constraints
aipmc principle list
aipmc link list
```

### Phase 2: Plan Work (Write)

When starting a new task:

```bash
# 1. Create a task with clear acceptance criteria
aipmc task add --title "Implement user authentication" --acceptance "User can login with email/password"

# 2. Record any decisions needed
aipmc decision add --title "Use JWT for auth tokens" --background "Need stateless auth" --decision "JWT with 24h expiry"

# 3. Link task to relevant decision
aipmc link add --source-type decision --source-id <decision-id> --relation implies --target-type task --target-id <task-id>

# 4. Update task status to in_progress
aipmc task update --id <task-id> --status in_progress
```

### Phase 3: Implement Code (Write)

During implementation:

```bash
# 1. Check current git status
aipmc code status
aipmc code diff

# 2. When ready, record commit with auto-git integration
aipmc commit add --title "Add user login endpoint" --summary "Implemented POST /api/login with JWT token generation" --auto-git

# 3. Update commit with review status
aipmc commit update --id <commit-id> --status committed --review-status approved
```

### Phase 4: Complete Work (Write)

When task is done:

```bash
# 1. Verify commit is linked and approved
aipmc commit list --task-id <task-id>

# 2. Mark task as done (requires approved commit)
aipmc task update --id <task-id> --status done

# 3. If emergency override needed (no approved commit)
aipmc task update --id <task-id> --status done --allow-without-commit
```

### Phase 5: Daily Close (Write)

End of work session:

```bash
# Record daily progress
aipmc daily close --completed "Implemented user login" --problems "None" --risks "Need to add password reset" --next "Start password reset flow"
```

---

## Command Reference

### Initialization

```bash
aipmc init                    # Create .pmai/ directory with config.json, pmai.db, USAGE.md
```

### Read Commands (Safe to Run Anytime)

```bash
# Project overview
aipmc info                    # Show runtime info, db path, config
aipmc status                  # Snapshot of tasks, decisions, commits
aipmc doctor                  # Check project health

# Inbox (items needing attention)
aipmc inbox                   # Proposed decisions, pending reviews, blocking issues

# Canon (living project document)
aipmc canon show              # Show current product goals, engineering focus, architecture

# Visions (long-term goals)
aipmc vision list             # List all visions
aipmc vision list --status active
aipmc vision show --id <vision-id>

# Principles (governance rules)
aipmc principle list
aipmc principle list --status active
aipmc principle show --id <principle-id>

# Links (relationships between entities)
aipmc link list

# Tasks (work items)
aipmc task list
aipmc task list --status todo
aipmc task list --status in_progress
aipmc task list --status done
aipmc task show --id <task-id>

# Decisions (recorded choices)
aipmc decision list
aipmc decision list --status proposed
aipmc decision list --status accepted
aipmc decision show --id <decision-id>

# Commits (code changes)
aipmc commit list
aipmc commit list --task-id <task-id>
aipmc commit list --status committed
aipmc commit show --id <commit-id>

# Ideas (suggestions for future)
aipmc idea list
aipmc idea list --status proposed
aipmc idea show --id <idea-id>

# Daily notes
aipmc daily show

# Docs audit
aipmc docs list
aipmc docs audit

# Git commands
aipmc code status             # Git worktree status
aipmc code diff               # Show diff (add --staged for staged changes)
aipmc code recent             # Recent commits (add --limit N, default 10)

# Feedback (remote)
aipmc feedback list
aipmc feedback add --label bug --content "Description of bug"
aipmc feedback add --label suggestion --content "Suggestion text"
```

### Write Commands (Modify State)

```bash
# Vision management
aipmc vision add --title "Vision title" --summary "Description" --status active
aipmc vision update --id <vision-id> --title "New title" --summary "Updated" --status active

# Principle management
aipmc principle add --title "Principle title" --summary "Description" --kind technical
aipmc principle update --id <principle-id> --title "New title" --status archived

# Link management
aipmc link add --source-type vision --source-id <id> --relation supports --target-type task --target-id <id>
aipmc link delete --id <link-id>

# Task management
aipmc task add --title "Task title" --acceptance "Acceptance criteria" --priority high
aipmc task update --id <task-id> --status in_progress
aipmc task update --id <task-id> --status done                    # Requires approved commit
aipmc task update --id <task-id> --status done --allow-without-commit  # Emergency override

# Decision management
aipmc decision add --title "Decision title" --background "Context" --decision "Chosen approach"
aipmc decision review --id <decision-id> --status accepted
aipmc decision review --id <decision-id> --status rejected

# Commit management
aipmc commit add --title "Commit title" --summary "Description" --auto-git
aipmc commit update --id <commit-id> --status committed --review-status approved
aipmc commit update --id <commit-id> --auto-git  # Auto-fill from git

# Idea management
aipmc idea capture --title "Idea title" --summary "Description"
aipmc idea review --id <idea-id> --status accepted

# Daily notes
aipmc daily close --completed "Done today" --problems "Issues" --risks "Risks" --next "Next steps"
aipmc daily replace --completed "..." --problems "..." --risks "..." --next "..."

# Canon update
aipmc canon update --decision-id <decision-id> --product-goal "..." --engineering-focus "..." --architecture "..."
aipmc canon update --decision-id <decision-id> --add-scope "scope1" --add-avoid "avoid1"

# Brief generation
aipmc brief product
aipmc brief architecture
aipmc brief modules
```

### Web UI

```bash
aipmv                         # Start web viewer at http://127.0.0.1:8011/
```

---

## Important Rules for AI Coders

### Task Completion Rule
- **Must have**: At least one linked commit with `status=committed|merged` AND `review_status=approved`
- **Emergency override**: Use `--allow-without-commit` only when absolutely necessary

### Decision Review Rule
- Decisions start as `proposed`
- Must be reviewed to `accepted` or `rejected`
- Accepted decisions may imply canon follow-up

### Canon Governance
- Canon reflects accepted decisions
- Update canon after decisions that change product goals, engineering focus, or architecture
- Use `aipmc docs audit` to check for doc-governance issues

### Feedback API
- Default base URL: `http://43.167.206.218:8080`
- Override with `--base-url <url>` or env `PMAI_FEEDBACK_BASE_URL`
- Labels only support: `bug`, `suggestion`
- Remote failures return JSON error and exit quickly (no hanging)

---

## Runtime Files

- `.pmai/config.json` - Web server configuration (host, port)
- `.pmai/data/pmai.db` - SQLite database with all project data
- `.pmai/USAGE.md` - This file

---

## Installation Notes

**Recommended (pipx):**
```bash
python -m pip install --user pipx
python -m pipx ensurepath
pipx install aipm-cli
```

**Direct pip install:**
```bash
python -m pip install aipm-cli
```

**Run without installation:**
```bash
# From project root
python -m pmai help
python -m pmai.run           # Start web server
```

**If commands not found:**
- Use `python -m pmai` instead of `aipmc`
- Use `python -m pmai.run` instead of `aipmv`
- On macOS/Windows, Python scripts directory may not be in PATH
"""


def get_usage_path(start: Path | None = None) -> Path:
    return get_runtime_dir(start, create=True) / USAGE_FILENAME


def write_usage_file(start: Path | None = None) -> Path:
    path = get_usage_path(start)
    path.write_text(build_usage_markdown(), encoding="utf-8")
    return path
