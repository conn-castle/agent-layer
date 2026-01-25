---
description: Language-agnostic cleanup pass: optionally remove dead code (only if tooling exists), polish comments and micro-refactor without behavior changes, and split oversized source files so they stay under a configurable line limit.
---

# Tidy codebase and split oversized files (universal)

## Applicability
This workflow is intentionally **language- and framework-agnostic**.

- It prefers **commands already defined by the repository** (Make/Task/Just/Turbo/package scripts, CI scripts, etc.).
- It includes **optional “common ecosystem” fallbacks** only when the repo clearly matches that ecosystem.
- If the repo has no tests or build commands, it still performs safe cleanup and provides a clear **verification limitation note**.

Treat this as a **starting point**: adapt thresholds, scopes, and splitting strategy to the repo’s norms.

---

## Optional guidance from the user
If the user provides extra direction, interpret it as:

- A preferred line limit for splitting oversized files (default: 400).
- Whether to attempt dead-code removal (default: auto, only when tooling already exists).
- Scope preference (default: touched files; the user may request all files or provide explicit paths).
- Maximum number of oversized files to split per run (default: 3).
- File-type strategy (default: source files; the user may specify extensions or ask to include all text files).
- Whether new tooling may be installed (default: never).

Defaults are optimized to avoid large diffs and avoid adding dependencies.

---

## Roles and handoffs (multi-agent)
1. **Repo Scout**: detect how the repo is built/tested/linted; determine what “source files” mean here; gather candidates.
2. **Dead Code Analyst**: run dead-code tooling *only if already present*; propose deletions with evidence.
3. **Code Janitor**: comment cleanup + micro-refactors in scope (strictly behavior-preserving).
4. **Splitter**: split oversized files with an explicit plan; preserve public APIs where possible.
5. **Verifier**: run the fastest credible verification available in the repo; re-run size scan.
6. **Reporter**: summarize before/after, commands, and any limitations.

---

## Non-negotiable constraints
- **Never change functionality.** No semantic behavior changes, no algorithm changes, no API contract changes.
- Refactors must be **mechanical and behavior-preserving**:
  - variable/function renames
  - extracting helpers without changing logic
  - moving code across files/modules without changing semantics
  - introducing small seams for testability/splitting (e.g., dependency injection) *only if behavior is preserved*
- Avoid style-only churn in unrelated files.
- Avoid creating many tiny files. New files must be **logical, substantial groupings**.
- Do not add new tooling unless the user explicitly allows it and the repo’s docs/CI clearly expect it.

---

# Phase 0 — Preflight (Repo Scout)

## 0A) Establish baseline
- `git status --porcelain`
- `git diff --stat`

If the repo is not a git repo, proceed but use filesystem scanning and clearly note the limitation.

## 0B) Discover how the repo wants to be run
Prefer *repo-defined* commands and scripts (in roughly this order):
- `Makefile` targets
- `Taskfile.yml` (task), `justfile` (just)
- `turbo.json` tasks (turbo)
- `package.json` scripts
- CI scripts under `.github/workflows`, `.circleci`, etc. (only as references; do not run CI in full unless requested)

**Deliverable (Repo Scout → Orchestrator):**
- Primary languages/ecosystem signals (based on config files present)
- Best available commands for:
  - dead code (or “none available”)
  - formatting/linting (or “none available”)
  - fast verification (tests/build/typecheck)
- File-type strategy for size scanning (extensions list or source/all-text plan)

---

# Phase 1 — Dead code analysis (optional) (Dead Code Analyst)

## 1A) Decide whether to run
- If the user asks to skip dead-code analysis, skip it.
- If the user explicitly asks to run it, run only if tooling already exists; otherwise stop and report.
- Otherwise, run it only if the repo already has a dead-code task/tool configured.

## 1B) Run dead-code tool (repo-first)
Examples of acceptable “already present” dead-code runners:
- `make dead-code`
- `task dead-code`
- `just dead-code`
- `turbo run dead-code`
- `npm/pnpm/yarn run dead-code`

Do **not** introduce a new dead-code tool by default.

## 1C) Triage findings (strict)
For each flagged item:

**Delete only if ALL are true**
- Not referenced by production code, scripts, or runtime wiring.
- Not referenced by tests (except potentially tests for itself).
- Not part of a documented or implied public API surface.

**Keep if ANY are true**
- Used by `scripts/`, migrations, tooling, or operational workflows.
- Public API surface or compatibility layer.
- Framework reflection/registration patterns (commonly false-positives).
- Intentionally kept for future compatibility with an external interface.

When deleting:
- Remove the whole unused symbol(s), not just references.
- Avoid leaving commented-out code; delete it.

**Deliverable (Dead Code Analyst → Orchestrator):**
- A deletion plan (delete/keep) with brief evidence.
- Apply deletions only after the plan is coherent.

---

# Phase 2 — Code quality cleanup (Code Janitor)

## 2A) Determine the working set
- If the user asks for touched scope, limit to files changed in Phase 1 plus files that must change due to splitting.
- If the user provides explicit paths, limit to those paths.
- If the user asks for all files, still avoid repo-wide “beautification”; only clean where you touch or where high-impact debt exists.

## 2B) Comment cleanup rules
Remove:
- comments that restate code (“increment i”)
- stale “dev notes” / narrative TODOs with no owner or action
- commented-out code blocks

Keep or improve:
- “why” comments (constraints, invariants, tradeoffs)
- TODOs that are actionable and near-term

If a removed TODO is still important:
- convert it into the repo’s preferred backlog mechanism (issue tracker, `docs/TODO.md`, etc.) if one exists.

## 2C) Micro-refactors only (no behavior change)
Allowed:
- naming improvements
- extracting helpers
- simplifying control flow without changing outputs/side-effects
- import cleanup consistent with repo norms

Not allowed:
- changes to algorithms or semantics
- changes to return types/shapes or error behavior
- changes to ordering of side effects where it could matter

---

# Phase 3 — Identify oversized files (Repo Scout)

## 3A) Define “source files”
If the user provides extensions, use them.

Otherwise, choose an extension list based on repo signals:
- Prefer the repo’s main languages (e.g., detected by presence of `go.mod`, `Cargo.toml`, `pom.xml`, `.csproj`, etc.).
- If uncertain, use a conservative default set of common source/script extensions:

  - **General-purpose:** `.py,.rb,.php`
  - **JS/TS:** `.js,.mjs,.cjs,.ts,.tsx,.jsx`
  - **Frontend SFCs (common):** `.vue,.svelte`
  - **Go/Rust:** `.go,.rs`
  - **JVM:** `.java,.kt`
  - **.NET:** `.cs`
  - **C/C++:** `.c,.h,.cpp,.hpp`
  - **Swift:** `.swift`
  - **Shell scripts:** `.sh,.bash,.zsh`

Notes:
- Many repos also contain executable scripts with **no extension**. If those are important, include all text files or provide explicit extensions and/or include a custom `scripts/` path.

## 3B) Scan for oversized files
**Preferred (git repo):** use tracked files to avoid vendor/build artifacts.
- Use `git ls-files` as the file list source.
- Filter by extensions unless the user asked to include all text files.

If not a git repo:
- Use `find` with excludes for common artifact directories (vendor, node_modules, dist, build, out, target, bin, obj, .venv, .git, etc.)

**Result format:** `<line_count> <path>`

## 3C) Select files to split
- Sort descending by line count.
- Select at most the max-files cap.
- If using touched scope, prefer oversized files already in the working set.

**Deliverable (Repo Scout → Orchestrator):**
- Ranked list of oversized files.
- Chosen subset for splitting this run.

---

# Phase 4 — Split oversized files (Splitter)

For each selected file:

## 4A) Create a split plan before edits
1. Identify logical groupings:
   - cohesive types/classes
   - related functions/utilities
   - pure logic vs I/O boundaries
   - constants/types vs runtime logic
2. Propose a minimal module layout:
   - Prefer **2–4 files**, not 10+.
   - Name by responsibility (`*_types`, `*_core`, `*_io`, `*_helpers`, etc.).
3. Preserve API stability:
   - Prefer keeping the original file as a thin façade that re-exports / delegates.
   - If façade causes circular dependencies, adjust boundaries or move shared types to a neutral module.

## 4B) Language-specific notes (optional, apply only if relevant)
Apply only if the repo’s language/ecosystem matches:

- **Go:** split into multiple files in the same package; imports often unchanged.
- **Java/C#:** split by moving top-level classes into separate files; keep package/namespace consistent.
- **C/C++:** consider separating declarations (headers) from implementations; avoid changing linkage/visibility.
- **Rust:** split modules with `mod` and `pub use` to preserve paths; watch for visibility.
- **Python/JS/TS:** use modules + re-export patterns; keep old imports stable where possible.
- **Shell scripts:** split by extracting functions into separate sourced files (e.g., `src/lib/*.sh`) or by breaking a large “do-everything” script into multiple scripts; preserve:
  - shebang line
  - `set -euo pipefail` semantics (if present)
  - environment variable expectations and working-directory assumptions
  - executable bit (if relevant)

If none match, split using the project’s existing modularization conventions.

## 4C) Execute the split
- Move cohesive blocks into new or existing modules.
- Update imports/references across the repo.
- Ensure:
  - the original file ends under the line limit
  - no circular imports introduced
  - no public API breaks unless explicitly intended (generally avoid)

---

# Phase 5 — Verify behavior (Verifier)

## 5A) Prefer fix-tests when available
- If a `fix-tests` workflow exists, run it to reach a commit-ready state.
- If it does not exist, proceed with the verification guidance below.

## 5B) Choose the fastest credible verification
Prefer repo-defined “fast lane” commands:
- `make test-fast` / `make test`
- `task test` / `just test`
- `turbo run test`
- `npm/pnpm/yarn test`
- any documented “quick checks” in the README/CONTRIBUTING

If none exist, choose ecosystem-appropriate defaults **only when clearly indicated by repo files**:

- `go test ./...` (if `go.mod`)
- `cargo test` or `cargo test -q` (if `Cargo.toml`)
- `dotnet test` (if `.sln`/`.csproj`)
- `mvn test` / `gradle test` (if `pom.xml` / `build.gradle`)
- Shell tooling only if already used by repo:
  - `shellcheck` (if configured/available)
  - `bash -n` for basic syntax checking where applicable

If no tests/build exist:
- run at least a syntax/compile check if available
- otherwise document the limitation clearly

## 5C) Re-scan oversized files
Confirm:
- all files you modified are under the line limit
- no new oversized files were created unintentionally

If verification fails:
- fix issues without changing behavior
- if needed, revert and simplify the split plan

---

# Phase 6 — Report (Reporter)

Provide:
- **Scope:** which files were touched and why
- **Dead code:** what was removed (high-level) and evidence of safety
- **Cleanup:** comment/todo changes + micro-refactors performed
- **Splits:** per file:
  - before → after line counts
  - new modules created/moved
  - API preservation approach (façade/re-exports vs direct import updates)
- **Commands run:** exact commands
- **Verification outcome:** pass/fail and any limitations
- **Follow-ups:** remaining oversized files (if any) beyond the max-files cap
