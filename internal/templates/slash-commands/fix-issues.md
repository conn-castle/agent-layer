---
description: Issue-driven maintenance loop: triage docs/agent-layer/ISSUES.md, produce an explicit implementation plan, pause for human approval, then execute, audit, and verify—updating ISSUES.md throughout.
---

# Issue-driven maintenance loop (plan → execute → audit → verify)

## Applicability
This workflow is designed to work in **any repository**.

- It prefers **repo-defined commands** (Make/Task/Just/Turbo/package scripts/custom CI scripts).
- It supports a **human approval gate** to prevent unintended changes.
- It assumes an issues ledger file exists at `docs/agent-layer/ISSUES.md`, but includes fallbacks if it does not.

Treat this as a starting point. Adjust scope limits and verification rigor based on project maturity and risk.

---

## Optional guidance from the user
If the user provides extra direction, interpret it as:

- Whether they want planning only or to proceed to execution after approval (default: plan).
- Alternate paths for the issues ledger or README (defaults: `docs/agent-layer/ISSUES.md`, `README.md`). The plan file is always stored at `.agent-layer/tmp/implementation_plan.md`.
- Maximum number of issues to fix in one run (default: 3).
- Maximum number of files to touch (default: 12).
- Desired risk level and verification depth (default: medium risk and automatic verification; skip only if explicitly requested).
- Scope preference (default: targeted; the user may ask for touched-only or all).

**Human approval rule**
- If the user asks to execute, execution is allowed **only if** explicit approval is provided by the user (human).

---

## Roles and handoffs (multi-agent)
1. **Issue Triage Lead**: parse ISSUES, cluster themes, select a coherent subset.
2. **Architect / Standards Reviewer**: extract project standards from README and define acceptance criteria.
3. **Planner**: write `.agent-layer/tmp/implementation_plan.md` (explicit steps, files, tests, risks).
4. **Implementer**: execute the plan, keeping diffs tight and behavior-preserving unless an issue explicitly requires behavior change.
5. **Auditor**: review touched code for maintainability, standards alignment, and hidden regressions.
6. **Verifier**: run the fastest credible checks available; escalate to broader checks if risk warrants.
7. **Reporter**: summarize what changed, why, and how it was verified; update `ISSUES.md`.

If only one agent is available, execute phases in this order and clearly label each phase.

---

## Non-negotiable constraints
- Do not exceed reasonable scope:
  - fix a **logical subset** of issues (default cap: 3)
  - avoid touching more than 12 files unless required for correctness or explicitly requested
- Follow the repo’s architectural and style standards (from `README.md` and existing patterns).
- Keep changes **reviewable**:
  - avoid opportunistic refactors not tied to an issue
  - if you discover additional problems, log them to `ISSUES.md` rather than expanding scope
- Prefer backwards-compatible changes unless an issue explicitly calls for a breaking change.

---

# Phase 0 — Preflight (Issue Triage Lead)

1. Confirm baseline:
   - `git status --porcelain`
   - `git diff --stat`

2. Verify documentation files exist:
   - Open the README (default: `README.md`).
   - Open the issues ledger (default: `docs/agent-layer/ISSUES.md`).

**If the issues ledger file does not exist**
- Search for an issue ledger file (examples: `ISSUES.md`, `docs/issues.md`, `docs/TODO.md`, `TODO.md`).
- If none exists:
  - create a minimal `docs/agent-layer/ISSUES.md` with a header + “Known Issues” section
  - populate it with any obvious issues discovered during triage (keep brief)

**Deliverable**
- Paths used (README, issues ledger, plan file at `.agent-layer/tmp/implementation_plan.md`)
- Repo status summary (clean/dirty)
- Any missing-docs remediation performed

---

# Phase 1 — Review standards and issues (Architect + Issue Triage Lead)

## 1A) Read standards
Read the README and extract:
- architecture boundaries and layering
- naming conventions
- dependency rules
- testing expectations
- lint/format expectations
- any “do not do” rules

Record these in the plan as “Standards to obey”.

## 1B) Triage the issues ledger
Parse the issues ledger and build a structured shortlist:
- issue title / identifier (if present)
- category: bug | tech debt | perf | docs | tests | build/CI | security | DX
- impact: high | medium | low
- effort guess: small | medium | large
- risk: low | medium | high
- dependencies (if any)

## 1C) Select a logical subset
- If the user specifies a number of issues, select that many in order (treat that number as the cap).
- If the user asks for all issues, select all open issues.
- Otherwise select the smallest coherent set:
  - usually more than 1 issue,
  - select as many issues as possible when they are tightly coupled, clearly parallelizable, or needed to reach a clean testing stopping point,
  - prioritize maximizing the batch size without blurring review scope or spanning unrelated areas.

**Deliverable**
- Selected issues (with rationale)
- Deferred issues (with rationale)

---

# Phase 2 — Write the plan and stop for approval (Planner)

Create the plan file at `.agent-layer/tmp/implementation_plan.md` (create the directory if needed) with:

## Required sections in `.agent-layer/tmp/implementation_plan.md`
1. **Objective**
   - what will be fixed and why (tie directly to issues)
2. **Scope**
   - included issues (explicit)
   - excluded issues (explicit)
3. **Standards to obey**
   - bullet list derived from README and existing patterns
4. **Approach**
   - design notes, constraints, and invariants
5. **Step-by-step tasks**
   - ordered checklist
   - name target files/modules
6. **Verification plan**
   - what commands will be run
   - what success looks like
7. **Risk + rollback**
   - risk areas, how to detect problems, how to revert safely

## Approval gate (mandatory)
After creating `.agent-layer/tmp/implementation_plan.md`:
- Summarize the plan in chat (brief, structured)
- **Stop** and request explicit approval.

**Do not execute** unless the human responds with approval.

### How the human approves
The user must respond with a clear explicit message (for example: “Approved”, “Continue”, or “Agreed”).

If approval is not given, end after the plan.

---

# Phase 3 — Execute the plan (Implementer)

**Entry condition**
- Proceed only when the user has asked to execute and explicit approval is provided.

## 3A) Execute step-by-step
- Implement tasks in the order listed.
- Keep diffs narrow and explainable.
- When in doubt, follow existing code patterns in the repo.

## 3B) Track out-of-scope findings
While working:
- If you find out-of-scope issues, deviations from standards, or poor abstractions:
  - add them to the issues ledger immediately under a dated “Discovered During Execution” section
  - keep each entry concise and actionable

Do not expand the implementation plan scope without human approval.

---

# Phase 4 — Audit and consolidate (Auditor)

## 4A) Audit touched areas
Review code you changed (and any nearby code you relied on):
- standards compliance (README + local conventions)
- error handling
- edge cases
- logging/telemetry patterns (if applicable)
- consistency and naming
- potential regressions
- accidental behavior changes

## 4B) Fix vs log
- If an issue is **in-scope and small**, fix it now.
- If it is **out-of-scope** or would expand the diff materially:
  - add it to the issues ledger with enough context to reproduce/understand
  - do not fix it in this run

## 4C) Consolidate the issues ledger
- remove duplicates
- merge near-duplicates
- ensure each issue is actionable and not ambiguous

---

# Phase 5 — Double-check correctness (Implementer + Auditor)

Perform a deliberate review pass:
- Re-read the original selected issues and confirm each is actually resolved.
- Validate acceptance criteria from the plan.
- Confirm no new warnings/errors are introduced.
- Ask: “What could break in production because of this change?” and address or document it.

---

# Phase 6 — Verify and finalize (Verifier + Reporter)

## 6A) Choose verification level
Use the user’s verification preference if provided; otherwise default to automatic:

- If the user requests fast verification:
  - run the repo’s quickest test/check target
- If the user requests full verification:
  - run the repo’s full test suite and/or build checks
- Otherwise:
  - default to fast checks
  - escalate to fuller checks if:
    - risk is high, or
    - changes touch core infrastructure, build/CI, or public APIs
- If the user explicitly requests no verification:
  - only if explicitly requested; note limitations clearly in the report

## 6B) Run repo-defined commands first
Preferred sources for commands (in order):
- `make test-fast` / `make test` (if present)
- `task test` / `just test`
- `turbo run test`
- `npm/pnpm/yarn test`
- documented commands in README/CONTRIBUTING

If no commands exist:
- run the most basic available checks (e.g., compile/typecheck/syntax check) only if the repo clearly supports them
- otherwise state that verification could not be performed

## 6C) Update the issues ledger
- Remove issues that are now fixed.
- Add any new out-of-scope issues discovered during verification.
- Ensure the ledger remains clean and deduplicated.

## 6D) Handle the plan artifact
- If repo conventions prefer deleting: delete `.agent-layer/tmp/implementation_plan.md`
- Otherwise: mark it “Completed” with a short completion note and keep it in `.agent-layer/tmp/implementation_plan.md` for traceability

## 6E) Final report
Return:
- issues fixed (with references/titles)
- key code changes (high-level)
- verification commands run + outcomes
- issues added to the issues ledger (out-of-scope)
- any limitations (e.g., no tests available)

---

## Output expectations (what “done” looks like)
- `.agent-layer/tmp/implementation_plan.md` exists (plan mode) OR is completed/removed (execute mode).
- Selected issues are fixed and removed/marked resolved in `docs/agent-layer/ISSUES.md`.
- Any discovered out-of-scope issues are captured in `docs/agent-layer/ISSUES.md`.
- Verification was performed at the appropriate level (or explicitly skipped with documented limitation).
