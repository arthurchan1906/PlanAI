# PlanAI AI-First Implementation Order V1

## Goal

Keep implementation order aligned with AI coding pain points rather than generic project-management features.

## Step 1. Improve Inbox Quality

Focus:
- better `recommended_actions`
- more precise pending categories
- stronger canon follow-up detection
- task closure blockers surfaced in inbox

Why first:
- AI needs the next action more than it needs more records

## Step 2. Strengthen Approval Boundaries

Focus:
- explicit approval commands for high-impact transitions
- clearer distinction between draft/proposed/accepted/truth
- fewer silent state jumps

Why second:
- without this, AI can still over-finalize work

## Step 3. Add Verification Evidence

Focus:
- preserve what was run
- preserve what passed or failed
- preserve residual risk

Why third:
- AI coding quality depends on evidence, not just optimistic state updates

## Step 4. Add Drift / Conflict Detection

Focus:
- accepted decisions not in canon
- stale or conflicting docs
- task / commit / review mismatch
- active truth ambiguity

Why fourth:
- once inbox and approval flow exist, drift detection becomes actionable

## Step 5. Add Session / Worklog Layer

Focus:
- current attempt
- blocker
- local hypothesis
- short-lived execution trace

Why fifth:
- this helps AI during long iterative coding loops

## Step 6. Refine Web as a Secondary Workflow Surface

Focus:
- homepage driven by inbox
- exceptions first
- approvals first
- governance workflows before presentation

Why later:
- web is useful, but not the primary operating path for the AI
