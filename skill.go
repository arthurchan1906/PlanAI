package main

const skillMD = `---
name: pmai
description: Use when recording bugs, searching project history, or linking commits to tasks. Triggers: bug discovery, fix completion, before modifying code.
---

# AIPM — AI Project Manager

AIPM is the project's knowledge base. Every bug, task, commit, decision, and principle lives here.
**Use it to LOOK UP existing information before acting. Record new information when done.**

## Parent-Child Hierarchy (STRICT)

commit -> task -> plan -> roadmap
Every record must have a parent at creation time. No orphans, no back-fill.

## Search — ALWAYS DO THIS FIRST

` + "`" + `` + "`" + `
aipmc search "<keyword>"
aipmc bug list
aipmc decision list
aipmc principle list
` + "`" + `` + "`" + `

## Record a bug — MUST include all metadata

` + "`" + `` + "`" + `
aipmc bug add \
  --title "One-line description" \
  --error "Full error message verbatim" \
  --files "File.swift:funcName" \
  --root-cause "Root cause, not symptom" \
  --fix "How it was fixed" \
  --tags "comma,separated,keywords"
` + "`" + `` + "`" + `

## Record a commit (task-id required)

` + "`" + `` + "`" + `
aipmc commit add \
  --task-id <task-id> \
  --title "commit message title" \
  --summary "Brief description of changes"
` + "`" + `` + "`" + `

## Before creating records — find or create the parent first

Before ` + "`commit add`" + `: run ` + "`aipmc task list --status in_progress`" + ` to find a task. If none, create one.
Before ` + "`task add --plan-id <plan-id>`" + `: run ` + "`aipmc plan list`" + ` to find a plan. If none, create one.
Before ` + "`plan add --roadmap-id <roadmap-id>`" + `: run ` + "`aipmc roadmap list`" + ` to find a roadmap. If none, create one.

## NEVER

- Create an orphan record (commit without task, task without plan, plan without roadmap)
- Back-fill parent associations — find or create the parent FIRST
- Skip recording a found bug (future you will hit it again)
- Record a bug without --error and --root-cause (they are what search finds)

*Installed by aipmc. To update: aipmc init*
`
