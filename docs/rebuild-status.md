# PlanAI Rebuild Status

This document captures the current rebuild direction for PlanAI, what is already done, and what remains for the next round.

## Overall Goal

PlanAI is being reshaped from a broad object-heavy project tracker into a local `context engineering + governance shell` for AI software delivery.

The target product shape is:

- `aipmc` gives coding AI a compact runtime context instead of forcing it to reconstruct project state from scattered records.
- `aipmv` gives a project manager a control console instead of a passive entity browser.
- `idea -> decision/task -> evidence/review -> done` becomes the main operational chain.
- governance stays lightweight, but it must clearly expose review points, blockers, and handoff state.

## What Is Already Done

### 1. Context shell is in place

Implemented:

- `aipmc context`
- `aipmc next`
- `aipmc handoff`

Current effect:

- AI can load a compressed project context before starting work.
- AI can inspect the current mainline and recommended next step.
- AI can hand work off with a stable packet instead of relying on chat history alone.

Main code:

- `src/pmai/store/context_runtime.py`
- `src/pmai/cli/parser.py`
- `src/pmai/cli/__init__.py`
- `src/pmai/web/handlers/bootstrap.py`

### 2. Idea thread model is working

Implemented:

- idea summary fields:
  - `current_summary`
  - `main_question`
  - `recommended_next_action`
  - `updated_at`
- idea comments table and APIs
- CLI support for:
  - `idea update`
  - `idea comment`
  - `idea convert`

Current effect:

- ideas are no longer static capture records
- ideas can be discussed, summarized, and gradually converged
- idea threads now act as the pre-mainline discussion layer

Main code:

- `src/pmai/store/ideas.py`
- `src/pmai/store/idea_comments.py`
- `src/pmai/store/idea_conversion.py`
- `src/pmai/web/handlers/ideas.py`

### 3. Idea conversion chain is working

Implemented:

- `idea -> task`
- `idea -> decision`
- conversion links via `converted_to`
- generated task acceptance and decision draft content from idea thread summary

Current effect:

- ideas can now formally enter the execution mainline
- conversion remains traceable instead of losing origin context

### 4. Web console is now closer to a PM dashboard

Implemented in `aipmv`:

- `AI Context`
- `Next / Handoff`
- `Ready Ideas`
- `Handoff Risks`
- `Evidence Review`
- `Closure Blockers`

Current effect:

- the dashboard now answers more of the key management questions:
  - what is the current mainline
  - what should happen next
  - which ideas are ready to enter the mainline
  - which evidence items need review
  - which tasks are blocked from closure

Main code:

- `ui/src/views/DashboardView.jsx`
- `src/pmai/store/summary.py`

### 5. Traceability between ideas, tasks, and decisions is visible

Implemented:

- task cards show source idea
- decision cards show source idea
- task/decision can jump back to idea thread
- ideas display conversion target

Current effect:

- PM can understand where execution items came from
- idea discussion is no longer isolated from the mainline

Main code:

- `ui/src/views/TasksView.jsx`
- `ui/src/views/DecisionsView.jsx`
- `ui/src/views/IdeasView.jsx`
- `ui/src/components/IdeaDetailPanel.jsx`

### 6. Evidence and review chain is working

Implemented:

- commits now support:
  - `evidence_summary`
  - `review_notes`
- task cards show:
  - linked evidence count
  - reviewed evidence count
  - verified evidence count
  - latest evidence summary
  - closure blocker reasons
- commits view supports:
  - evidence fields in form
  - review/verification quick actions
  - task-focused filtering
  - attention filtering

Current effect:

- a task is no longer just “done because status changed”
- evidence and review state now participate in closure logic and PM visibility

Main code:

- `src/pmai/store/commits.py`
- `src/pmai/store/tasks.py`
- `src/pmai/web/handlers/bootstrap.py`
- `ui/src/views/CommitsView.jsx`

### 7. Task closure governance was tightened

Current rule:

- a task can only be marked `done` when it has at least one linked commit with:
  - `status in (committed, merged)`
  - `review_status = approved`
  - `test_status = passed`

Current effect:

- backend rule now matches the evidence/review semantics shown in the UI
- “done” better reflects verified delivery, not only review completion

## Current Product Shape

The current working chain is:

`idea thread -> task/decision -> evidence/review -> done`

And the current shell around that chain is:

- `context`
- `next`
- `handoff`
- dashboard governance cards

This is enough to treat the current state as a real `v0.1` directionally valid version.

## Still To Do

### High priority

1. Build a stronger review workspace

Needed:

- a more focused review view or inbox lane for:
  - pending decision review
  - pending evidence review
  - task closure blockers

Why:

- review signals exist now, but they are still split across dashboard cards and generic lists

2. Strengthen context packaging further

Needed:

- richer `context pack` summaries for:
  - why current roadmap/plan is the mainline
  - what is explicitly not in scope
  - what questions AI must not silently decide

Why:

- current context is much better than before, but it still emphasizes “what exists” more than “what must be preserved”

3. Improve execution-facing PM navigation

Needed:

- stronger linking between:
  - dashboard cards
  - filtered commit review queue
  - blocked task queue
  - idea threads

Why:

- the core data is present, but the PM path can still be tighter

### Medium priority

4. Rename or evolve `commit` toward broader `evidence`

Needed:

- consider whether `commit` should remain the main evidence object
- potentially generalize toward evidence records that can represent:
  - code change evidence
  - verification evidence
  - manual validation evidence

Why:

- not all proof of delivery is a git commit

5. Improve inbox and governance unification

Needed:

- bring decisions, evidence review, canon follow-up, and closure blockers into one explicit governance queue

Why:

- this would better match the target role of PlanAI as a governance shell

6. Reduce front-end bundle size

Known issue:

- `npm run build` still succeeds with a large chunk warning

Needed:

- code splitting
- route/view splitting

Why:

- current build is usable but not yet optimized

## Suggested Next Start Point

When work resumes, the recommended next step is:

1. add a dedicated review workspace
2. unify review queue + closure blockers + pending decisions into a stronger governance console
3. only after that, further polish context pack depth and terminology

## Validation Already Completed

The following have already been validated during the current rebuild round:

- Python compile checks via `python3 -m compileall`
- repeated `npm run build` success
- temporary runtime validation for:
  - `context / next / handoff`
  - idea capture / update / comment / convert
  - idea -> decision
  - task -> evidence -> review -> done

## Notes

- The current state is good enough to stop and resume later without losing the mainline.
- The next round should continue deepening the existing shell, not adding more disconnected concepts.
