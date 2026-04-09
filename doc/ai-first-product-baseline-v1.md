# PlanAI AI-First Product Baseline V1

## Product Position

PlanAI is not primarily a human-facing project management tool.
Its first job is to help an AI coding agent keep the engineering mainline stable during long, multi-step implementation work.

The human role is:
- approve high-impact state transitions
- inspect drift and conflicts
- correct direction when the AI is wrong

The AI role is:
- keep work moving
- write drafts and evidence
- update task progress
- propose decisions and changes

The system role is:
- preserve mainline context
- separate draft from truth
- prevent silent self-approval
- retain verification evidence
- surface pending next actions

## The Real Pain Points

### 1. Context Drift

During extended coding sessions, AI tends to lose:
- the current main objective
- active constraints
- what was already rejected
- what still needs approval

PlanAI must continuously compress the project state into a short actionable context, not just store records.

### 2. Draft Becomes Truth Too Easily

AI naturally treats recent output as if it were canonical.
Without strong state boundaries, the following get mixed together:
- idea
- draft
- proposed
- accepted
- canon
- obsolete

PlanAI must make these boundaries explicit and operational.

### 3. Missing Next Action

AI often records many objects but still lacks the immediate next move.
Tasks alone are too coarse.

The tool must answer:
- what should happen next
- what is blocked on approval
- what can proceed automatically

### 4. Silent Self-Approval

If the system only records updates, AI will often move from:
- proposed -> accepted
- draft -> done
- note -> truth

PlanAI must be opinionated about which transitions require explicit human confirmation.

### 5. Code / Docs / Decisions Drift Apart

AI can modify code, describe a plan, and update records at different times.
Soon they disagree.

PlanAI must actively detect:
- accepted decisions not reflected in canon
- active docs in conflict with current truth
- tasks marked done without review evidence
- commits that remain unreviewed

### 6. Verification Is Too Weak

AI frequently says something is implemented without preserving enough verification detail.
Simple `test_status` is not sufficient for high-trust workflows.

## Product Design Principles

### Principle 1. Mainline First

The most important output is not a table of all objects.
It is a compressed answer to:
- what is the current mainline
- what is pending
- what is the next action

### Principle 2. State Separation Over Convenience

The product should prefer explicit state boundaries over easy but ambiguous shortcuts.

### Principle 3. AI Writes Drafts, Humans Confirm Truth

The default assumption should be:
- AI may draft
- AI may append evidence
- AI may update in-progress work
- AI may not silently finalize high-impact states

### Principle 4. Workflow Over Display

Web and CLI should primarily expose:
- pending actions
- blockers
- approvals
- drift

not only entity lists.

### Principle 5. Evidence Over Narrative

The system should prefer verified, linked evidence over broad narrative summaries.

## Core Object Model

The current object set is directionally correct:
- canon
- tasks
- commits
- decisions
- ideas
- docs
- daily notes

But the product should treat them as workflow states, not isolated records.

The important chains are:
- `idea -> decision -> canon`
- `task -> commit -> review -> done`
- `doc draft -> active -> source_of_truth / obsolete`
- `daily note -> current risk / next action`

## AI-First P0 Capabilities

These should be the first-class product surface.

### 1. Mainline Summary

A short machine-readable context containing:
- current objective
- active constraints
- in-progress tasks
- pending approvals
- next recommended actions

### 2. Inbox / Pending Actions

This is already the right direction.
Inbox should be the default answer to:
- what needs human attention now
- what the AI should wait for
- what the human should review first

### 3. Approval Boundaries

At minimum, the following should require explicit review:
- decision accepted / rejected / superseded
- canon update
- task done
- source_of_truth assignment

### 4. Drift Audit

The system should proactively surface:
- proposed decisions not reviewed
- accepted canon-impacting decisions not absorbed into canon
- committed changes without review
- doc governance exceptions

### 5. Verification Evidence

Commits should eventually carry more than `test_status`.
The workflow needs structured verification traces.

## AI-First P1 Capabilities

### 1. Work Session Layer

Add a short-lived session/worklog object for:
- current attempt
- blocker
- assumption
- validation result

This is better suited to AI iteration than overloading daily notes.

### 2. Rich Verification Records

Add explicit records for:
- command run
- test scope
- result
- residual risk

### 3. Stronger Drift Detection

Add topic-aware checks such as:
- multiple active truth sources in the same topic
- stale active docs
- canon/doc mismatch
- task/commit mismatch

## Human-Facing UI Guidance

The Web UI matters, but it is secondary.
It should support AI-first workflows rather than redefine them.

The homepage should answer:
- what is pending
- what should happen next
- what is blocked

The CLI should remain the primary operational path.

## Current Priority Order

### P0

- strengthen inbox and next-action quality
- strengthen approval boundaries
- strengthen drift detection
- improve verification evidence

### P1

- refine workflow-oriented web pages
- improve installation and direct command availability
- improve human audit views

### P2

- richer dashboards
- more UI affordances
- more presentation polish

## What PlanAI Should Avoid

- behaving like a generic PM dashboard first
- allowing AI to silently convert drafts into truth
- treating entity counts as more important than next actions
- using web configuration as the primary governance path
- relying on humans to manually remember state relationships

## One-Sentence Definition

PlanAI is a local governance layer for AI coding work that preserves mainline context, enforces state boundaries, exposes pending actions, and reduces silent drift between code, decisions, documents, and approvals.
