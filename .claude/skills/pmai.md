---
name: pmai
description: AIPM project management — use MCP tools for all operations. Triggers: before coding, after commits, bug discovery, creating tasks/plans.
---

# AIPM — AI Project Manager

AIPM is the project's knowledge base. Every task, commit, plan, bug, decision lives here.
**Use MCP tools — they are always visible in your tool list. No CLI memorization needed.**

## MCP Workflow (PRIMARY)

### Before coding — ALWAYS
→ **aipm_get_briefing** — read project state, PM alerts, risks, duplicates

### When creating a task
→ **aipm_create_task** — auto-checks duplicates, backfills parent, validates plan status

### After a commit
→ **aipm_record_commit** — records commit, detects scope drift, cross-task file conflicts

### When unsure
→ **aipm_search_context** — search all entities, returns related context
→ **aipm_analyze** — full project health check

### Done reading PM alerts
→ **aipm_mark_consumed** — confirms you've seen PM's changes

## Parent-Child Hierarchy (STRICT)

commit → task → plan → roadmap
Every record must have a parent. No orphans.

## CLI (debug/fallback only)

`aipmc init` — one-time project setup
`aipmc web` — PM dashboard server
`aipmc mcp` — start MCP server (usually auto-launched)

## NEVER

- Create orphan records
- Skip aipm_get_briefing before coding
- Ignore reflection prompts from MCP tools
- Record bugs without error and root-cause

*Installed by aipmc. To update: aipmc init*
