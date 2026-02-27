---
name: boost-coverage
description: Iteratively raise coverage by selecting the lowest-covered eligible file, adding tests, and repeating until the repo-defined coverage threshold is met, using coverage commands documented in COMMANDS.md.
---

# Boost coverage until the target is met (repo-adaptive, monorepo-safe)

## Intent
Increase test coverage by iterating over eligible business-logic files until the repo-defined threshold is met:
- Build a per-file coverage table across coverage domains/components.
- Repeatedly select the eligible file with the lowest line coverage (or a user-specified target for the next iteration).
- Add or update tests to raise that file's coverage, then re-run coverage to refresh the table.
- If no threshold is documented, ask the user to provide one, log it in `DECISIONS.md`, and use it going forward.
- Stop when the repo-defined coverage threshold is met.
- If coverage stops improving, no eligible files remain, or verification fails, stop and report the shortfall.

This workflow is designed for agentic environments:
- It prevents "silent partial coverage" in monorepos by using a confidence gate and explicit stop conditions.
- It persists the definitive coverage commands in `COMMANDS.md` for repeated reuse.

---

## Optional guidance from the user
If the user provides extra direction, interpret it as:

- Coverage threshold: use the repo-defined threshold (CI requirements or `DECISIONS.md`). If none is documented, ask the user to provide one and log it in `DECISIONS.md`.
- A specific target file path; if provided, use it for the next iteration unless the user explicitly wants to stop after that file.
- A coverage domain/component to focus on (default: auto-detect).
- Iteration cap (default: none). If provided, stop after that many files and report whether the threshold is met.
- Scope preference (default: automatic, choose repo or domain as appropriate).
- Verification depth (default: automatic).
- Whether to install missing coverage tooling (default: ask; always requires approval).
- Whether to persist coverage commands to `COMMANDS.md` (default: yes).

---

## Roles and handoffs (multi-agent)
1. **Coverage Scout**: discover coverage domains and commands; compute confidence; propose the coverage plan.
2. **Coverage Runner**: execute coverage commands and produce a normalized per-file coverage table.
3. **Target Selector**: apply eligibility rules and choose the lowest-covered file (or validate a user-specified target) for each iteration.
4. **Test Designer**: derive behavior-driven test cases that cover branches and edge cases meaningfully.
5. **Test Implementer**: add/update tests to raise coverage without changing behavior.
6. **Verifier**: re-run the smallest credible coverage check to confirm the target file improves and the repo progresses toward the threshold.
7. **Reporter**: summarize before/after, commands, and changes; update `COMMANDS.md` if enabled.

---

## Global constraints
- **No behavior changes.** Minor refactors are allowed only to enable testability (e.g., dependency injection) and must preserve semantics.
- Improve coverage for the **selected file only per iteration** (avoid broad test sweeps unless required to execute the component's test runner).
- Keep tests deterministic: no real network/time/randomness without mocking.
- Exclude non-business-logic files from selection (config, generated, mocks, tests, build outputs, vendor).
- Do not stop early without reporting why the threshold could not be met.

---

# Phase 0 — Preflight (Coverage Scout)
1. Confirm baseline:
   - `git status --porcelain`
2. Ensure `COMMANDS.md` exists:
   - If missing, ask the user before creating it. If approved, copy `.agent-layer/templates/docs/COMMANDS.md` into `COMMANDS.md` when available; otherwise ask before creating a minimal structured file.

---

# Phase 1 — Discover coverage commands and domains (Coverage Scout)

## 1A) Primary source: COMMANDS.md
Search `COMMANDS.md` for coverage-related commands. Prefer a heading named **Coverage** if it exists, but do not require any specific sections. Extract:
- coverage domains/components (if monorepo)
- command(s) to generate coverage per domain
- working directory for each command
- expected output artifact(s) and format if documented (lcov/cobertura/json/text)
- any exclusions (include/exclude globs)

## 1B) Auto-discovery fallback (repo-normal places)
If Coverage is missing or incomplete, attempt discovery (do not guess silently):
- `README.md` / `CONTRIBUTING.md` / `docs/*`
- `Makefile`, `Taskfile.yml`, `justfile`, `turbo.json`
- `package.json` scripts (if present)
- CI workflows as hints (do not run full CI)

## 1C) Determine coverage domains (monorepo-safe)
If multiple independently testable units exist (apps/packages/services), treat them as separate coverage domains unless a single repo-wide coverage command is explicitly documented.

## 1D) Confidence scoring and gate
Assign confidence for the coverage plan:

- **High confidence** if:
  - commands are explicit and clearly mapped to domain(s),
  - coverage output is parsable (known artifact or stable textual table),
  - the plan is unlikely to miss major parts of the repo.

- **Medium confidence** if:
  - plausible commands exist but mapping or completeness is ambiguous,
  - multiple competing commands appear valid.

- **Low confidence** if:
  - no credible coverage commands are discoverable,
  - coverage output paths/formats are unknown.

### Gate behavior (mandatory)
- If **high confidence**: proceed without user confirmation.
- If **medium or low confidence**:
  1) Present the proposed plan (domains + commands + working directories + expected outputs).
  2) Ask the user to confirm or provide the definitive commands and where they should be documented.
  3) Do not proceed until confirmed.

## 1E) Persist commands (seamless)
If the user confirms/provides commands and wants them recorded:
- Update `COMMANDS.md` under **Coverage** (and **Test** if relevant).
- Only record commands expected to be reused.

## 1F) Determine the coverage threshold (repo-defined)
1. Check CI or test requirements for an explicit coverage gate (for example, coverage configuration or workflow checks).
2. Check `DECISIONS.md` for an existing coverage threshold decision.
3. If a threshold is found, use it and avoid adding a duplicate decision.
4. If no threshold is documented, ask the user to provide one before proceeding.
5. Once provided, add a `Decision YYYY-MM-DD abcdef` entry to `DECISIONS.md` and use that threshold for this run and future runs.

---

# Phase 2 — Install missing coverage tooling (optional, approval required)

If coverage execution fails due to missing tooling (plugin/runner):
1. Propose the smallest viable installation approach (exact command and why it is needed).
2. Ask the user for approval.
3. Only after approval, install and re-run coverage.
4. Update `COMMANDS.md` prerequisites if the user wants commands recorded.

---

# Phase 3 — Run coverage and build a per-file table (Coverage Runner)

For each coverage domain:
1. Run the documented coverage command.
2. Collect coverage results from:
   - artifacts when available (preferred): `lcov.info`, `cobertura.xml`, JSON reports
   - otherwise parse a stable textual coverage table

Normalize into a table:
- `domain`
- `file`
- `line_coverage_percent`
- optional: `lines_total`, `lines_missed` when available

---

# Phase 4 — Select the next target file (Target Selector)

## 4A) Eligibility filtering
Exclude files likely to be noise:
- tests, mocks, fixtures
- generated code
- configuration/build glue
- UI boilerplate directories (if repo-standard)
- entrypoints that are intentionally thin (barrels, index files) unless they contain logic

## 4B) Target selection
- If the user provides a target file:
  - validate it is eligible business logic
  - compute current coverage for it
- Otherwise:
  - select the eligible file with the **lowest** line coverage across domains
  - if coverage is not comparable across domains, choose:
    - the lowest file within the domain that has the clearest, most reliable coverage measurement, and report the limitation

Deliverables:
- chosen target file
- coverage before (with evidence from report/artifact)
- explanation of eligibility and selection

---

# Phase 5 — Design tests (Test Designer)

1. Read the target file and identify:
   - public interfaces
   - branches and guard clauses
   - error paths and boundary conditions
   - external dependencies and side effects
2. Produce a test plan that increases meaningful coverage:
   - happy path
   - edge cases
   - error handling
   - branching coverage
3. Propose minimal refactors only if required for testability.

---

# Phase 6 — Implement tests (Test Implementer)
- Add or update tests according to repo conventions.
- Keep changes local and behavior-preserving.
- Prefer stable unit tests; use mocks/fakes for IO and time.

---

# Phase 7 — Verify the target and refresh coverage (Verifier)

Verification preference order:
1. File-scoped coverage for the target (best).
2. Domain-scoped coverage re-run and confirm the target file's percent improves.

After verification:
- Re-run the coverage command to refresh the per-file table and repo totals.

If verification cannot be performed reliably:
- stop and report what is missing (commands/artifacts/tooling)
- propose the smallest change to make it verifiable (and record it as an Issue if deferred)

---

# Phase 8 — Iterate or stop (Coverage Scout + Target Selector)
- If the repo coverage threshold is met, proceed to the report.
- If coverage improved and eligible files remain, return to Phase 4.
- If coverage did not improve or no eligible files remain, stop and report why the threshold could not be met.

---

# Phase 9 — Report (Reporter)
Return:
- files targeted per iteration
- coverage before -> after per file
- repo coverage before -> after
- tests changed/added (files and scenarios)
- commands run (coverage + verification)
- any tool installation approved/performed
- `COMMANDS.md` updates (if any)
- stop condition when the threshold is not met
