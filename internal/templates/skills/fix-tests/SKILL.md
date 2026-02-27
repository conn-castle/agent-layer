---
name: fix-tests
description: Run repo-defined checks (e.g., lint/format/pre-commit/tests) in a loop, fixing failures until all checks pass.
---

# Fix failing checks (repo-defined)

## Intent
Turn a failing repo into a **commit-ready** state by:
- discovering the repo’s preferred checks (e.g., lint/format/pre-commit/tests or a single “all checks” command),
- running them in the repo’s intended sequence,
- fixing failures, and
- repeating until all checks pass.

This workflow is **verification-first** and should fail loudly when required commands are missing or unclear.

---

## Optional guidance from the user
If the user provides extra direction, interpret it as:

- Verification depth: fast vs full tests (default: fast when available, else full).
- Which checks to include (default: whatever the repo documents as required).
- Whether to run pre-commit (default: yes if the repo uses it).
- Max iterations before stopping (default: 3).
- Scope for fixes (default: failures caused by the current changes only).
- Whether to update `COMMANDS.md` when new repeatable commands are discovered (default: yes).

---

## Roles and handoffs (multi-agent)
1. **Repo Scout**: identify required checks from documentation and repo tooling.
2. **Executor**: run the repo-defined checks and capture failures.
3. **Fixer**: apply targeted fixes for failed checks.
4. **Verifier**: rerun checks until all pass or max iterations reached.
5. **Reporter**: summarize commands run, failures fixed, and remaining issues.

If only one agent is available, execute phases in this order with explicit headings.

---

## Global constraints
- **Do not skip required checks.** Fix the underlying issue instead of disabling or bypassing tools.
- **Fail loudly.** If a required command is missing, unclear, or unavailable, stop and ask for guidance.
- **No silent fallbacks.** Do not invent commands that are not documented or discoverable.
- **Test integrity:** Do not lower coverage thresholds or weaken gates to make tests pass.
- **Scope discipline:** Fix only issues directly related to the failing checks; log unrelated problems to `ISSUES.md`.

---

# Phase 0 — Preflight (Repo Scout)

1. Establish baseline:
   - `git status --porcelain`
   - `git diff --stat`
2. Read `COMMANDS.md` first (if present) and identify the repo’s required checks.
   - prefer a single “all checks” command if the repo defines one
   - otherwise collect lint/format, pre-commit, and test commands
3. If required checks are missing or unclear:
   - try discovery (Makefile/Taskfile/justfile/package scripts)
   - if still unclear, **stop and ask**
4. If new repeatable commands are discovered, update `COMMANDS.md` based on your memory instructions.

**Deliverable**
- Selected checks and execution order
- Whether “fast” tests exist and which command will be used

---

# Phase 1 — Execute checks (Executor)

Run the checks **in the repo’s intended order**.
- If the repo defines a single “all checks” or “ci” command, run that.
- Otherwise, use a reasonable order that fits the repo (for example: format/lint → pre-commit → tests).
- Skip optional checks only when the repo does not use them and the user agrees.

If any step fails:
- capture the failure output
- proceed to Phase 2

---

# Phase 2 — Fix failures (Fixer)

For each failing check:
- fix the root cause (no bypasses)
- keep changes minimal and targeted
- if the failure is unrelated to current work, log it to `ISSUES.md` and stop

---

# Phase 3 — Rerun loop (Verifier)

- Rerun the same check sequence that failed.
- Repeat until all pass or the max iteration limit is reached.
- If the limit is reached, stop and report remaining failures.

---

# Phase 4 — Report (Reporter)

Return:
- Commands run (exact)
- Failures encountered and fixes applied
- Whether all checks passed
- Any out-of-scope issues logged
