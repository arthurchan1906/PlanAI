package main

const skillMD = `---
name: pmai
description: AIPM project management — use MCP tools for all operations. Triggers: before coding, after commits, bug discovery, creating tasks/plans.
---

# AIPM — AI Project Manager

AIPM is the project's knowledge base. Every task, commit, plan, bug, decision lives here.
**Use MCP tools — they are always visible in your tool list. No CLI memorization needed.**

## Daily Workflow (PRIMARY)

### Before coding — ALWAYS
→ **aipm_get_briefing** — read project state, 线索 (threads), PM alerts, risks, duplicates

### When creating a task
→ **aipm_create_task** — auto-checks duplicates, backfills parent, validates plan status

### After a commit
→ **aipm_record_commit** — records commit, detects scope drift, cross-task file conflicts

### End of session — DAILY
→ **aipm_suggest_threads** — analyze today's commits, suggest 线索 that group related work. The agent should:
  1. Read the suggestions
  2. For each suggestion: if it matches an existing thread, use **aipm_add_to_thread**
  3. For new patterns, use **aipm_create_thread** to establish a new 线索

### When unsure
→ **aipm_search_context** — search all entities (tasks, plans, threads, decisions, bugs, ideas)

### Done reading PM alerts
→ **aipm_mark_consumed** — confirms you've seen PM's changes

## 线索 (Threads) — NEW

Threads are retrospective groupings of related work across plans/tasks/commits.
They help track work that spans multiple plans or evolved non-linearly.
Threads are identified AFTER work is done — the agent presents suggestions, the PM confirms.

## Parent-Child Hierarchy (STRICT)

commit → task → plan → roadmap
Every record must have a parent. No orphans.

## Link Relations

Use **aipm_link_entities** to connect entities. Recommended causal relations:
- causes — work on A directly caused need for B
- enables — completing A unlocked B
- blocks — A prevents progress on B
- supersedes — B replaces A (but A's lessons may still be relevant)

## NEVER

- Create orphan records
- Skip aipm_get_briefing before coding
- Skip aipm_suggest_threads at end of session
- Ignore reflection prompts from MCP tools
- Record bugs without error and root-cause

*Installed by aipmc. To update: aipmc init*
`
