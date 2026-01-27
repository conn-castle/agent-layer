---
description: Post-task wrap-up: reflect on recent changes, update project memory files with compact deduplicated entries, run fast verification, and summarize.
---

# Post-task cleanup and project memory update

## Intent
After completing a task, perform a focused wrap-up that:
- validates what was changed (and what was not),
- captures **deferred work** and **risks** in the project memory files,
- removes items that are now fixed,
- runs the fastest credible verification,
- produces a clear summary for a human reviewer.

This is **not** a full codebase audit. Only document what you touched or passively observed during the task.

---

## Project memory files (authoritative)
Canonical list:
- `ISSUES.md`
- `BACKLOG.md`
- `ROADMAP.md`
- `DECISIONS.md`

Formatting: follow the entry formats defined in each file. If any required files are missing, ask the user before creating them.

---

## Optional guidance from the user
If the user provides extra direction, interpret it as:

- Scope: default to uncommitted changes; the user may request since last commit, a specific git range, or explicit paths.
- Plan file path: use the plan/task path from the current run (printed earlier); no overrides.
- Verification depth and risk level: default to automatic verification with medium risk.
- Roadmap updates: default to automatic updates when the work maps to roadmap tasks; skip if the user asks to avoid updates.
- Maximum new entries across memory files: default to 10.

---

## Artifact handling (standard)
- Use the plan/task path from the current run (printed earlier in the workflow).
- Do **not** create new artifacts or invent a path.
- If no plan path is available, skip plan alignment and cleanup.

---

## Roles and handoffs (multi-agent)
1. **Change Reviewer**: enumerates what changed, checks plan alignment, and notes passive observations.
2. **Memory Curator**: updates project memory files with compact deduplicated entries; removes resolved items.
3. **Verifier**: runs the best available fast checks; escalates when risk warrants.
4. **Reporter**: summarizes outcomes (what changed, what was logged/removed, what was verified).

If only one agent is available, execute phases in this order with explicit headings.

---

## Global constraints
- Keep scope tight. No opportunistic refactors.
- Do not start a broad audit; focus on touched files and nearby context only.

---

# Phase 0 — Preflight (Change Reviewer)

1. Establish baseline:
   - `git status --porcelain`
   - `git diff --stat`
2. Determine a reference identifier for entries:
   - Preferred: `git rev-parse --short HEAD` (use as `abcdef`)
   - If git is unavailable: use `nogit` as the identifier.

3. Build the review file list based on `scope`:

- Default to uncommitted changes:
  - staged: `git diff --name-only --staged`
  - unstaged: `git diff --name-only`
- If the user requests since last commit:
  - `git show --name-only --pretty="" HEAD`
- If the user provides a git range:
  - `git diff --name-only <range>`
- If the user provides explicit paths:
  - use those paths directly

If the file list is empty:
- state “No changed files detected” and proceed with memory cleanup only (dedup/removal).

**Deliverable**
- List of files reviewed
- Short SHA used for ledger entries (or `nogit`)
- Whether the working tree is clean/dirty

---

# Phase 1 — Reflect on recent work (Change Reviewer)

## 1A) Plan alignment (if a plan exists)
If a plan file path is available from the current run:
- read it
- compare planned tasks vs actual changes
- list:
  - completed items
  - omissions
  - deviations (and why)

If no plan file exists:
- state that no plan artifact was found and skip plan alignment.

## 1B) Passive best-practice check (no broad audit)
Review only:
- the files in scope,
- any directly related modules you opened during the task,
- relevant standards in `README.md` (only as needed to judge compliance).

Capture:
- over-complication / unnecessary abstraction noticed while implementing
- best-practice violations observed (naming, layering, error handling, boundary validation)
- band-aids / workaround patterns you encountered
- risky fallbacks/defaults that may hide failures

**Output of this phase**
- A concise bullet list of “Findings to log” grouped by:
  - Bugs / correctness risks
  - Maintainability / technical debt
  - Reliability / security / performance
  - User-visible feature ideas (only if truly user-facing)
  - Decisions with long-term impact that are not obvious from code/config (deferrals, constraints, irreversible tradeoffs)

---

# Phase 2 — Update project memory files (Memory Curator)

## 2A) Ensure memory files exist
Ensure the project memory files listed above exist.

If missing:
- ask the user before creating it. If approved, copy `.agent-layer/templates/docs/<NAME>.md` into `<NAME>.md` (preserve headings and markers).

## 2B) Decide where each finding belongs
- Add to **`ISSUES.md`** if it is:
  - a bug, correctness defect, maintainability refactor, technical debt, reliability gap, security concern, test gap, performance concern, or engineering risk.
- Add to **`BACKLOG.md`** only if it is a **new user-visible capability** request.
- Add to **`DECISIONS.md`** if the task required a significant decision:
  - record decision, reason, and tradeoffs
  - consolidate or replace near-duplicate decisions instead of adding a new entry
  - log only decisions future developers/agents would not infer from the code or config
  - keep it brief and add new entries at the bottom so the oldest decisions remain at the top
- Update **`ROADMAP.md`** only if the user asks for roadmap updates, or if automatic updates are appropriate and:
  - the completed work clearly maps to existing roadmap tasks, or
  - the roadmap is now stale/contradicted by what was implemented.

## 2C) Prevent duplicates before writing
Before adding a new entry:
- search the target file for key terms and nearby titles
- if a near-duplicate exists:
  - merge the new information into the existing entry
  - keep the final entry within 3–5 lines

## 2D) Remove resolved items
- For each issue that is now fixed by the recent work:
  - remove it from `ISSUES.md` completely
- For each backlog item that is now implemented:
  - remove it from `BACKLOG.md`

## 2E) Consolidate and keep files readable
- Merge duplicates.
- Ensure entries remain compact (3–5 lines).
- Ensure no abbreviations.
- Keep the file easy to scan (follow the existing ordering convention for that file).

## 2F) Respect entry limits
Do not add more than the entry cap across all memory files in a single run.
If more exist:
- add the most impactful first
- summarize the remainder in the final report as “not logged due to limit”

---

# Phase 3 — Regression test and verification (Verifier)

## 3A) Choose verification level
- If the user explicitly requests no verification, skip it and clearly document the limitation.
- If the user requests fast verification, run the repo’s fast checks.
- If the user requests full verification, run the repo’s full checks.
- Otherwise:
  - default to fast checks
  - escalate toward full checks when risk is high or changes touch core infrastructure, build pipelines, or public interfaces.

## 3B) Prefer fix-tests when available
- If a `fix-tests` workflow exists, run it to reach a commit-ready state.
- If it does not exist, proceed with repo-defined commands below.

## 3C) Prefer repo-defined commands
Attempt, in order, depending on what exists in the repository:
- `make test-fast` (preferred when available)
- `task test-fast` / `just test-fast`
- `turbo run test --filter=...` (if turbo is present and configured)
- `npm/pnpm/yarn test` (if package scripts define a fast lane)
- any documented “quick checks” in `README.md` / `CONTRIBUTING.md`

If no credible commands exist:
- run the smallest applicable sanity check (compile/typecheck/syntax) only if the repo clearly supports it
- otherwise record “No verification command available” in the report

## 3D) If verification fails
- Fix failures only if the fix is directly connected to the recent work and remains in-scope.
- If the failure indicates a broader problem:
  - log it to `ISSUES.md`
  - stop further scope expansion.

---

# Phase 4 — Final report (Reporter)

Provide a structured summary:

## Summary
- Files reviewed (count + list or top-level directories)
- Plan alignment (if applicable): omissions/deviations
  - Memory updates:
    - issues added (titles only)
    - issues removed (titles only)
    - backlog items added/removed (titles only)
  - decisions logged (titles only)
  - roadmap updates (if any)
- Verification:
  - commands run
  - pass/fail
  - limitations (if any)

## Out-of-scope discoveries
List any out-of-scope items that were observed and where they were logged (ISSUES/BACKLOG), or note if they were not logged due to the entry cap.

---

# Phase 5 — Cleanup (Reporter)

- If a plan file was used and the run completed successfully:
  - delete the plan file only if it exists
  - delete any other workflow-generated files explicitly listed for that workflow **only** if they share the same run-id and live under `.agent-layer/tmp/`
  - do not delete any other files
- If no plan file exists, state that cleanup was not needed.

---

## Definition of done
- Recent work has been reviewed for plan alignment and passive best-practice concerns.
- Project memory files are up to date, deduplicated, and compact.
- Fixed issues/features have been removed from their ledgers.
- Plan file cleanup is complete (deleted if it existed).
- Fast verification has been run (or explicitly skipped with a clear limitation note).
