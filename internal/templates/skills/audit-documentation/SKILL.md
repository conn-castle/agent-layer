---
name: audit-documentation
description: Audit all Markdown documentation for accuracy and cross-document consistency against the repository (static validation only). Produce a reviewable report and ask the user which findings to fix, log to ISSUES.md, or ignore.
---

# Documentation audit (all Markdown, report-first)

## Intent
Audit **all `*.md` files** in the repository to ensure:
- Documentation matches the codebase (static validation only).
- Documents are internally consistent with each other (no contradictions, drift, or stale references).

Default behavior is **report-first**:
- No code edits.
- No documentation edits.
- No automatic issue logging.

After presenting findings, ask the user which items should be:
- fixed in documentation,
- logged to `ISSUES.md`,
- both,
- or ignored/deprioritized.

---

## Optional guidance from the user
If the user provides extra direction, interpret it as:

- Mode: default to audit-only; the user may request proposals or apply changes (apply requires explicit approval).
- Scope: default to all tracked Markdown files; the user may specify paths or request only files changed since a git ref.
- Maximum findings: default to 30, prioritizing the most important.
- Snippet length: default to short excerpts; omit if the user requests none.
- Issue logging preference: default to asking which findings to log; the user may request no logging or automatic logging during apply.
- Documentation update preference: default to asking which doc fixes to apply; the user may request no doc fixes or automatic fixes during apply.
- Decision hygiene preference: default to performing decision cleanup during apply; the user may request skipping it.

**Approval gate**
- If the user asks to apply changes, you must have explicit approval and a selection list (see “User response protocol”).

---

## Roles and handoffs (multi-agent)
1. **Doc Inventory**: list all Markdown files in scope; categorize them.
2. **Claim Extractor**: extract “claims” from docs (commands, paths, env vars, APIs, architecture statements).
3. **Static Validator**: validate claims against the repo using targeted search and file existence checks.
4. **Cross-Doc Consistency Checker**: find contradictions and drift between documents.
5. **Fix Proposer**: propose minimal doc edits (and/or issue entries) for user review.
6. **Applier**: apply only the approved subset (doc edits and/or issue logging).
7. **Reporter**: produce the final report and stop for user decisions.

---

## Guardrails
- Static validation only (do not execute commands or run code).
- Avoid speculation: each finding must include evidence (file + section + what was checked).
- Prefer targeted repo search over manual line-by-line reading.
- Keep diffs minimal and localized when applying doc fixes.
- Never claim verification was run unless it was run and observed.
- Decision hygiene edits are allowed only in apply mode after explicit approval.

---

# Phase 0 — Determine scope and inventory docs (Doc Inventory)

## 0A) Build the Markdown file list
Preferred (git repo):
- `git ls-files '*.md'`

If the user provides paths, filter to those paths.
If the user requests changes since a git ref, use:
- `git diff --name-only <ref>..HEAD -- '*.md'`

If not a git repo:
- `find . -type f -name "*.md" -not -path "*/node_modules/*" -not -path "*/.git/*" -print`

## 0B) Categorize docs (for prioritization and better reporting)
Tag each file into one category:
- **Primary**: `README*.md`
- **Docs**: `docs/**`
- **Templates**: `.agent-layer/templates/**`
- **Workflows**: `workflows/**` or `.agent-layer/**`
- **Other**: everything else

This does not exclude files; it improves prioritization and reporting clarity.

---

# Phase 1 — Extract claims from docs (Claim Extractor)

Extract and normalize “claims” into a structured list. Common claim types:

## 1A) Commands
- Commands in code blocks (especially `bash`)
- Mentioned Make targets, package scripts, or tool invocations
- Anything that looks runnable (e.g., starts with `make`, `pnpm`, `npm`, `yarn`, `task`, `just`, `turbo`, `docker`, `python`, etc.)

## 1B) File and directory paths
- paths like `docs/...`, `src/...`, `apps/...`, `.agent-layer/...`
- referenced config files (`.env.example`, `pyproject.toml`, etc.)

## 1C) Environment variables and configuration keys
- `FOO_BAR=...`, `FOO_BAR` references
- config keys referenced in docs

## 1D) API / interface claims (repo-specific)
Examples:
- endpoint paths (`/api/...`)
- public modules or CLI subcommands
- schema or data model naming
- “this file/class/function does X” assertions

## 1E) Architectural invariants
Statements like:
- “X is the single source of truth”
- “Frontend must not contain business rules”
- “No silent fallbacks”
These should be consistent across docs and consistent with the codebase patterns.

---

# Phase 2 — Static validation against the repo (Static Validator)

Validate claims using only static checks:

## 2A) File existence checks
For each referenced path:
- confirm the file or directory exists in the repo
- if not, flag as stale/mismatched

## 2B) Command existence checks (static)
For each command claim, validate it is defined somewhere:
- If it looks like a Make target: check `Makefile` for target presence.
- If it looks like a package script: check `package.json` scripts.
- If it looks like a task/just/turbo command: check their config files if present.
- If it is a tool invocation: check that the repo docs mention how it is installed, or that it is pinned in lockfiles/config (do not install or run).

If a command is documented in multiple places, validate it is consistent (same name, same args, same working directory assumptions).

## 2C) Symbol / term presence checks (targeted search)
For claims about:
- endpoints, module names, configuration keys, environment variables, feature names,
perform a targeted repository-wide search for the term(s). If no credible hits exist, flag as likely stale.

---

# Phase 3 — Cross-document consistency audit (Consistency Checker)

Check for contradictions and drift between documents:

## 3A) Duplicate definitions
Example:
- Two docs define different ways to run tests or coverage.

## 3B) Contradictory rules
Example:
- One doc says “do not renumber phases” while another allows it.
- One doc says “use COMMANDS.md” while another hard-codes commands elsewhere.

## 3C) Stale references
Example:
- Document references a file path that has moved.
- Document references an old command name.

## 3D) Template drift (optional but recommended)
Compare `templates/**` vs `docs/**` for:
- entry format changes
- rule changes
- markers like `<!-- ENTRIES START -->`

If a template diverges from the canonical doc, flag it.

---

# Phase 4 — Synthesize and prioritize findings (Reporter)

Normalize findings into a consistent structure:

Each finding includes:
- **ID**: `A`, `B`, `C`, ...
- **Severity**: High | Medium | Low
- **Type**: command | path | env/config | API/interface | architecture | cross-doc
- **Evidence**: file + section + (optional short snippet)
- **Static check performed**: what you checked (existence/search/definition)
- **Result**: mismatch description
- **Recommendation**: smallest viable fix
- **Suggested disposition**: fix docs | log issue | both | ignore

Prioritization rubric:
- **High**: could cause developer mistakes (wrong command), could cause production risk, or contradicts critical rules.
- **Medium**: confusing/likely to drift into mistakes; not immediately hazardous.
- **Low**: wording polish, minor clarifications, non-blocking cleanup.

Cap total findings at the agreed limit (default: 30), but include “additional findings omitted” count if applicable.

---

# Phase 5 — Propose fixes and issue candidates (Fix Proposer)

Trigger when the user asks for proposals or apply mode.

## 5A) Propose documentation fixes (patch-style)
For each fixable doc mismatch:
- propose the minimal edit needed
- preserve existing doc tone and template markers
- do not introduce new policies not present elsewhere

## 5B) Propose issue entries (ready-to-paste)
If issue logging is requested or allowed, generate an issue candidate for each relevant finding using the project issue format:

- `- Issue 2026-01-11 abcdef: Short title`
    `Priority: <Critical/High/Medium/Low>. Area: <area>`
    `Description: <observed doc inconsistency or doc-to-code mismatch>`
    `Next step: <smallest concrete action>`
    `Notes: <optional>`

Do not write to `ISSUES.md` unless the user selects it, or the user asked for automatic issue logging during apply.

---

# Phase 6 — Present findings and ask the user (mandatory stop) (Reporter)

## Required chat output format
1. **Summary**
   - files scanned
   - number of findings by severity
2. **Findings list**
   - Each finding labeled `A`, `B`, `C`, ...
   - Each includes recommendation and (if in propose/apply) proposed patch + issue candidate.

## User response protocol (required)
The user replies with one line per finding ID:

- `A: fix` — apply doc fix (only when apply mode has been explicitly approved)
- `A: log` — add an issue entry to `ISSUES.md` (only when apply mode has been explicitly approved)
- `A: fix+log` — do both (only when apply mode has been explicitly approved)
- `A: ignore` — do not act on it
- `A: other <instruction>` — user provides a specific edit (e.g., “fix but do not log” or “log as Low priority”)

**Stop after presenting the findings.**
Do not modify any files until the user selects actions and explicitly approves apply mode.

---

# Phase 7 — Apply approved changes (Applier)

Trigger only when:
- the user has requested apply mode, AND
- the user has explicitly approved, AND
- the user has provided selections for at least one finding.

Apply rules:
- Apply only selected doc edits.
- Add only selected issue entries (deduplicate against existing issues).
- If a finding suggests code changes, do not implement code changes in this workflow. Instead:
  - log an issue (if selected), and/or
  - recommend a follow-up implementation workflow.

## 7B) Decision hygiene (apply-only)
After apply approval, review `DECISIONS.md` and remove or consolidate decisions that are:
- unmade or no longer relevant,
- too obvious to help future developers,
- or have no future ramifications.

Use judgment to keep the log concise. If multiple related decisions exist, you may replace them with a single new decision that preserves meaningful context and delete the originals.

## 7C) Post-apply report (Reporter)
After all changes are applied:
- Tell the user to review the diff.
- Summarize which decisions were removed or combined.

---

## Definition of done
- All Markdown files in scope were scanned and claims were validated statically.
- Findings are evidence-backed, deduplicated, and prioritized.
- The user is given a clear, itemized choice to fix, log, both, or ignore.
- No writes occur without explicit user selection and approval.
