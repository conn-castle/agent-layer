---
description: Continue executing the roadmap by selecting the next actionable task(s) in the active incomplete phase, producing an approval-gated implementation plan, then implementing, verifying, and updating ROADMAP/ISSUES/DECISIONS/COMMANDS.
---

# Continue roadmap work (plan → approve → execute)

## Intent
Pick up where the project left off by:
1) finding the **active phase** in `ROADMAP.md`,  
2) selecting the **next unchecked task(s)**,  
3) creating an approval-gated `.agent-layer/tmp/implementation_plan.md`,  
4) implementing + verifying, and  
5) updating `ROADMAP.md` and project memory files.

This workflow is **roadmap-driven**. It should not “freestyle” features from `FEATURES.md` unless they are already scheduled into the roadmap.

---

## Optional guidance from the user
If the user provides extra direction, interpret it as:

- Whether they want planning only or to proceed to execution after approval (default: plan).
- A specific roadmap phase number to use; otherwise select the first incomplete phase.
- How many tasks to bundle (default: the smallest coherent set, usually one task).
- Alternate path for the checklist file (default: `.agent-layer/tmp/task.md`). The plan file is always stored at `.agent-layer/tmp/implementation_plan.md`.
- Desired risk level and verification depth (defaults: medium risk and automatic verification).
- Whether to update `COMMANDS.md` when new repeatable commands are discovered (default: yes).

**Approval gate**
- Code changes are allowed only when the user has asked to execute **and** explicit approval is given.

---

## Roles and handoffs (multi-agent)
1. **Phase Scout**: identifies the active phase and next task(s); assembles required context.
2. **Planner**: produces `.agent-layer/tmp/implementation_plan.md` + checklist; integrates relevant ISSUES; flags ambiguities.
3. **Implementer**: executes the plan with tight scope and root-cause fixes.
4. **Verifier**: runs the most credible verification commands for the touched areas.
5. **Memory Curator**: updates ROADMAP/ISSUES/DECISIONS/COMMANDS; removes resolved entries.
6. **Reporter**: produces a final structured summary.

If only one agent is available, execute phases in this order with explicit headings.

---

## Global constraints
- **No silent assumptions.** If a requirement or expected behavior is unclear, stop and ask questions before planning.
- **No code changes before approval.**
- Keep scope tight: implement only the selected roadmap task(s) and necessary prerequisites.
- Follow repo standards (README + DECISIONS). Avoid band-aids; prefer root-cause fixes (ask for confirmation if refactor scope is large).
- Never claim verification was run unless it was actually run and observed.

---

# Phase 0 — Preflight (Phase Scout)

1. Confirm baseline:
   - `git status --porcelain`
2. Ensure `COMMANDS.md` exists.
   - If missing, ask the user before creating it. If approved, copy `.agent-layer/templates/docs/COMMANDS.md` into `COMMANDS.md` when available; otherwise ask before creating a minimal structured file.
3. Read (in this order):
   - `ROADMAP.md`
   - `DECISIONS.md` (if present)
   - `README.md`
   - `ISSUES.md`
   - `FEATURES.md` (for awareness only; do not schedule from it here)

---

# Phase 1 — Identify active phase and next tasks (Phase Scout)

## 1A) Determine the active phase
- If the user specifies a phase number, use that phase.
- Otherwise choose the **first incomplete** roadmap phase (the first phase heading that is not marked with ✅).

If no incomplete phase exists:
- Stop and ask the user what to do next (roadmap may be complete).

## 1B) Select the next task(s)
Within the active phase’s **Tasks** checkbox list:
- Identify unchecked items (`- [ ] ...`).
- If none remain, check the phase **Exit criteria**:
  - If exit criteria are satisfied, the plan should complete the phase (mark ✅ and summarize).
  - If exit criteria are not satisfied, stop and ask what is missing (roadmap may be out of sync).

Task selection rules:
- If the user specifies a number of tasks, select that many unchecked tasks (in order).
- If the user asks for all tasks, select all unchecked tasks.
- Otherwise select the **smallest coherent set**:
  - usually more than 1 task,
  - select as many tasks as possible when they are tightly coupled, clearly parallelizable, or needed to reach a clean testing stopping point,
  - prioritize maximizing the batch size without blurring review scope or spanning unrelated areas.

**Deliverable (Phase Scout → Planner)**
- Active phase title and number
- Selected task text (verbatim)
- Relevant files/modules likely involved (initial guess)
- Any immediate ambiguities found in task wording

---

# Phase 2 — Context and scope audit (Planner)

## 2A) Roadmap/standards alignment
- Re-read the active phase Goal, Tasks, Exit criteria.
- Re-check README/DECISIONS for constraints that affect the selected tasks (layering, data flow, error handling, time handling, “no silent fallbacks”).

## 2B) Code and dependency audit (focused)
Inspect only what is necessary:
- modules directly referenced by the selected tasks
- call-sites and boundary interfaces for those modules
- existing tests in the area
- relevant docs referenced by README/ROADMAP (for example schema docs if present)

## 2C) Issues intersection
Scan `ISSUES.md` for:
- issues that block the selected tasks (treat as prerequisites)
- issues likely to be fixed “for free” by this work
- issues that would become obsolete once the task is done

Rules:
- If an issue is a prerequisite and is small, include it in plan scope.
- If it is large, document it as a prerequisite and ask the user whether to expand scope.

## 2D) Feature intersection (awareness only)
- Do not pull new work from `FEATURES.md`.
- If you notice a feature that is already effectively scheduled in the roadmap (duplicated wording), note it for cleanup (FEATURES should remain unscheduled only).

## 2E) Ambiguity gate
If anything is ambiguous or contradictory (task definition, acceptance criteria, dependency behavior):
- Stop and ask the user clarifying questions.
- Do not write the plan until ambiguity is resolved.

---

# Phase 3 — Create plan artifacts (Planner)

Create:

## 3A) `.agent-layer/tmp/implementation_plan.md`
Required sections:
1. **Objective**
   - active phase + selected task(s)
2. **Scope**
   - in-scope tasks
   - out-of-scope items (explicit)
3. **Proposed changes**
   - grouped by component/module/file
4. **Tests**
   - what new/updated tests will be added per task
5. **Documentation**
   - which docs might need updates (README, COMMANDS, schema docs)
6. **Verification plan**
   - commands to run (prefer `COMMANDS.md`)
   - success criteria
7. **Risks and rollback**
   - what could break and how to revert safely

## 3B) `.agent-layer/tmp/task.md` checklist
A granular, ordered checklist aligned to the plan and the roadmap task(s).
- Keep steps small and verifiable.
- Include checkpoints for tests and docs updates.

## 3C) Plan quality audit
Before asking for approval, critically review the plan:
- does it match the roadmap task(s) exactly?
- is each task paired with tests?
- does it avoid band-aids?
- is the verification plan realistic and aligned with repo commands?

---

# Phase 4 — Approval gate (Reporter)

1. Provide a concise summary of:
   - active phase
   - selected task(s)
   - the proposed approach
   - verification commands to be run
2. Ask the user for explicit approval.

**To proceed, the user must provide explicit approval** (for example: “Approved”, “Continue”, or “Agreed”).

If approval is not granted, stop here.

---

# Phase 5 — Execute the plan (Implementer)

**Entry condition:** explicit approval from the user.

Execution rules:
- Implement changes in the order in `task.md`.
- Keep diffs limited to what the plan requires.
- Prefer root-cause fixes; if a large refactor becomes necessary, pause and ask for confirmation.

While executing:
- If you discover out-of-scope problems:
  - add them to `ISSUES.md` (compact entry)
  - do not expand scope
- If you discover new reusable commands:
  - update `COMMANDS.md` unless the user asked not to update commands

---

# Phase 6 — Verify (Verifier)

## 6A) Choose verification level
- If the user explicitly requests no verification, document the limitation.
- If the user requests fast checks, run the repo’s fast checks.
- If the user requests full verification, run the repo’s full checks.
- Otherwise choose fast by default and escalate to full when risk is high or changes touch core interfaces.

## 6B) Use `COMMANDS.md` first
Run the most relevant commands documented there (tests, typecheck, lint, build), prioritizing the smallest set that credibly verifies the change.

If the required command is missing from COMMANDS:
- attempt discovery (Makefile/scripts/README)
- if still unclear, ask the user
- then add the final command to `COMMANDS.md` if it will be reused

If verification fails:
- fix failures that are directly caused by the change
- log broader issues to `ISSUES.md` if out-of-scope

---

# Phase 7 — Update roadmap and memory files (Memory Curator)

## 7A) Update `ROADMAP.md`
- For each selected roadmap task completed:
  - change `- [ ]` to `- [x]`
- If the phase Exit criteria are satisfied:
  - mark phase heading with ✅
  - replace phase content with a bullet summary of accomplishments (no checkbox list)

Do not renumber completed phases. Only renumber incomplete phases if necessary for consistency and only when explicitly editing sequencing (rare for this workflow).

## 7B) Update `ISSUES.md`
- Remove issues that were fixed by this work.
- Add any newly discovered out-of-scope issues (compact, deduplicated).

## 7C) Update `DECISIONS.md` (if needed)
If a significant decision was made, log it (briefly).

## 7D) Update `FEATURES.md` (if needed)
If a feature was implemented as part of roadmap work and it still exists in FEATURES:
- remove it (FEATURES should only contain unscheduled/unfinished user-visible requests)

---

# Phase 8 — Final report (Reporter)

Return:
- Active phase and selected task(s)
- What was implemented (high-level)
- Tests added/updated
- Verification commands run and outcomes
- ROADMAP updates (tasks checked; phase completed or not)
- Issues removed/added
- Any decisions logged
