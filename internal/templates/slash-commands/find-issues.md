---
description: Proactive, repo-adaptive code quality audit to surface over-engineering, bad practices, and “band-aids”, aligned with README and the project memory files. Report-first: no automatic issue logging unless the user explicitly opts in.
---

# Proactive code quality audit (report-first, memory-aware)

## Intent
Perform a structured audit to find:
- over-engineering / unnecessary complexity
- poor abstractions / architecture drift
- brittle fallbacks and defaults that mask failures
- “band-aids” / quick-and-dirty fixes that became permanent
- DRY violations and extensibility pain

This workflow is **report-first**:
- **Do not modify code**.
- **Do not write to** project memory files unless the user explicitly requests it.

---

## Project memory files (authoritative)
Canonical list (read order for this workflow):
1. `ROADMAP.md`
2. `DECISIONS.md`
3. `BACKLOG.md`
4. `ISSUES.md`

Formatting: follow the entry formats defined in each file. If any required files are missing, ask the user before creating them.

---

## Optional guidance from the user
If the user provides extra direction, interpret it as:

- Scope: default to uncommitted changes; the user may request the last commit, a specific git range, specific paths, or the full repository.
- Depth: default to a full pass (narrow → deep → broad); the user may request narrow, deep, or broad only.
- Risk level: default to medium; use higher risk to prioritize more severe findings.
- Report path: always use the standard artifact naming rule (no overrides).
- Maximum findings: default to 20.
- Snippet length: default to short; omit snippets if the user requests none.
- Write mode: default to report-only; provide proposals if requested; apply changes only if the user explicitly requests it.
- Entry identifier: default to the git short SHA when available; use a user-provided token if given.

---

## Artifact naming (standard)
Artifacts are agent-only and always live under `.agent-layer/tmp/`.

Use the naming rule:
- `.agent-layer/tmp/<workflow>.<run-id>.<type>.md`
- `run-id = YYYYMMDD-HHMMSS-<short-rand>`
- Reuse the same `run-id` for multi-file workflows.
- Use `touch` to create the file before writing.
- **No overrides**: do not accept custom paths.

For this workflow:
- Report path: `.agent-layer/tmp/find-issues.<run-id>.report.md`
- Echo the report path in the chat output.

Example (shell):
```bash
run_id="$(date +%Y%m%d-%H%M%S)-$RANDOM"
report=".agent-layer/tmp/find-issues.$run_id.report.md"
touch "$report"
```

---

## Roles and handoffs (multi-agent)
1. **Standards Extractor**: read the project memory files in the order listed above, then read `README.md` to derive the “audit lens”.
2. **Diff Auditor**: narrow audit on changed files per `scope`.
3. **Deep-Dive Investigator**: expand one layer outward from the highest-impact narrow findings.
4. **Broad Scanner**: lightweight smell scanning + hotspot spot-checking.
5. **Synthesizer**: deduplicate, prioritize, and map findings to roadmap/decisions/standards.
6. **Reporter**: write the report file and present a review summary to the user.

If only one agent is available, execute phases in this order with explicit headings.

---

## Guardrails
- Avoid speculation: each finding must include **evidence** (file + symbol/section + reasoning; optional short snippet).
- Avoid nitpicks: focus on issues that impact correctness, delivery speed, operability, or extensibility.
- No broad rewrites: this is an audit, not a refactor plan.
- Use the repo’s terminology. Avoid abbreviations in memory entries.

---

# Phase 0 — Preflight (Diff Auditor)

1. Confirm repo state:
   - `git status --porcelain`

2. Determine the narrow file set:
- Default to uncommitted changes:
  - staged: `git diff --name-only --staged`
  - unstaged: `git diff --name-only`
- If the user requests the last commit: `git show --name-only --pretty="" HEAD`
- If the user provides a git range: `git diff --name-only <range>`
- If the user provides specific paths: use those paths directly.
- If the user requests a repo-wide scan: the narrow set is empty; proceed directly to the broad scan.

If uncommitted changes yield no files, fall back to the last commit and note it in the report.

3. Establish an entry identifier:
- if the user did not supply an identifier and git is available: use `git rev-parse --short HEAD`
- otherwise: use `nogit`

**Deliverable**
- Narrow file list (or explicit “none”)
- Actual scope used (including fallback)
- Entry identifier token

---

# Phase 1 — Standards and roadmap lens (Standards Extractor)

Extract:
- architectural boundaries and layering rules
- dependency rules and patterns to avoid
- module ownership expectations
- planned future direction (roadmap phases)
- existing decisions that constrain changes

Produce an “audit lens” checklist:
- **Must align with** (roadmap/decisions)
- **Must avoid** (anti-patterns called out by repo standards)
- **Design priorities** (simplicity, explicitness, reliability, performance, etc.)

---

# Phase 2 — Narrow audit (Diff Auditor)

Trigger when the chosen depth includes narrow.

For each file in the narrow set:

## 2A) Over-engineering / complexity signals
Examples (language-agnostic):
- multiple abstraction layers for simple operations
- indirection that obscures behavior (factories/wrappers without clear payoff)
- configuration/flags controlling many unrelated behaviors
- “framework inside the codebase” patterns (custom DSL/IOC) without strong justification

## 2B) Bad practices / brittleness
- swallowed errors / overly broad exception handling
- side effects hidden inside helpers with unclear ownership
- hidden global state, implicit environment assumptions
- confusing naming, unclear invariants, missing validation at boundaries
- unclear separation of concerns vs the README’s layering model

## 2C) Fallbacks, defaults, and band-aids
- magic defaults that mask missing configuration
- “temporary” workarounds that became permanent
- retries/timeouts/backoffs missing or inconsistent
- suppression markers (lint/type ignores) concentrated in one area

## 2D) Record evidence-backed findings
Each finding should include:
- **Title**
- **Location**: file + symbol/section
- **Evidence**: what you observed (optional short snippet)
- **Impact**: correctness | operability | maintainability | extensibility | performance | security
- **Severity**: High | Medium | Low
- **Recommendation**: concrete next step and smallest viable improvement
- **Alignment**: how it matches/conflicts with ROADMAP/DECISIONS/README

---

# Phase 3 — Deep dive (Deep-Dive Investigator)

Trigger when the chosen depth includes deep.

1. Select up to 5 highest-impact narrow findings.
2. For each, go one layer outward:
   - callers/call-sites
   - direct dependencies
   - adjacent modules in the same feature area
   - tests that cover the area (if present)
3. Determine whether the issue is localized or systemic:
   - repeated patterns across modules
   - inconsistent abstractions
   - duplicated logic or inconsistent defaults

Refine recommendations into “next-step sized” actions.

---

# Phase 4 — Broad audit (Broad Scanner)

Trigger when the chosen depth includes broad.

## 4A) Identify hotspots
Use one or more of:
- largest modules/files by line count
- high-churn areas (recent history) when practical
- core infrastructure directories named in README
- areas scheduled soon in ROADMAP (avoid spending effort on code slated for replacement; flag instead)

## 4B) Lightweight repo-wide smell scanning
Use tooling only if available; otherwise use simple searches (`rg` preferred, else `grep -R`).

Suggested patterns (adapt as appropriate):
- markers: `TODO|FIXME|HACK|WORKAROUND|TEMP|XXX`
- suppressed checks: `eslint-disable|@ts-ignore|nolint|noqa|type: ignore`
- suspicious defaults: `default|fallback|if.*None|null|undefined`
- broad catches: `except:|except Exception|catch \(.*\)`
- “temporary” flags: `feature flag|kill switch|temporary flag`

Summarize into themes, not raw match dumps.

## 4C) Spot-check for architectural drift
Pick a few representative areas and check:
- is the layering consistent with README?
- are boundaries stable with ROADMAP direction?
- do decisions in DECISIONS still hold, or are they being violated?

---

# Phase 5 — Synthesis and prioritization (Synthesizer)

1. Deduplicate findings across phases.
2. Prioritize using a consistent rubric:

**Severity rubric**
- **High**: likely correctness/security issue, production incident risk, or blocks ROADMAP.
- **Medium**: significant maintainability cost, likely bug source, extensibility pain.
- **Low**: clarity/polish; not urgent.

3. Select up to the findings cap for the report.

---

# Phase 6 — Report and review (Reporter)

Create the report file at `.agent-layer/tmp/find-issues.<run-id>.report.md` with:

## Required sections
1. **Executive summary** (5–10 bullets)
2. **Standards & roadmap lens** (from Phase 1)
3. **Top findings** (prioritized list)
4. **Themes and systemic risks**
5. **Suggested next steps**
   - propose small coherent batches (≤ 3) rather than a long backlog

## Optional: ready-to-paste memory entries
If the user asked for proposals or apply mode, include an “Entry candidates” section.

### Candidate entry rules (mandatory)
- Search existing memory files first and avoid near-duplicates.

**Important:** If the user asked for proposals, do not modify files—only provide candidates.

---

# Phase 7 — Optional apply step (only if explicitly requested)

Proceed only if the user explicitly asked to apply changes.

1. Ensure all memory files exist (ask the user before creating any missing files; if approved, copy `.agent-layer/templates/docs/<NAME>.md` into `<NAME>.md`).
2. For each approved finding:
   - Add to `ISSUES.md` if it is a defect, refactor, technical debt, reliability/security/performance risk, or test gap.
   - Add to `BACKLOG.md` only if it is a user-visible capability request.
   - Add to `DECISIONS.md` only if it is a non-obvious, long-term decision and the user wants it logged.
   - Consider `ROADMAP.md` updates only if the user explicitly wants roadmap edits.
3. Deduplicate by merging into existing entries; keep entries compact.

---

## In-chat review step (mandatory)
Present the top findings (High + Medium) to the user and ask which items should be:
- logged to ISSUES, BACKLOG, DECISIONS
- scheduled into ROADMAP
- turned into a fix plan
- deprioritized/ignored

---

## Definition of done
- The report file exists and is readable.
- Findings are evidence-backed, deduplicated, and aligned to README/ROADMAP/DECISIONS.
- No code changes made unless separately requested.
- Memory files are not modified unless the user explicitly asks to apply changes.
