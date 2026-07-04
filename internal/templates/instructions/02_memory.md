# Memory

## Memory files (authoritative, user-editable, agent-maintained)
- `docs/agent-layer/ISSUES.md` — deferred defects, maintainability refactors, technical debt, risks.
- `docs/agent-layer/BACKLOG.md` — unscheduled end-user-visible features and tasks (distinct from issues; not refactors).
- `docs/agent-layer/ROADMAP.md` — numbered phases; guides architecture and sequencing.
- `docs/agent-layer/DECISIONS.md` — rolling log of important, non-obvious decisions (brief).
- `docs/agent-layer/COMMANDS.md` — canonical, repeatable development workflow commands for this repository (build, test, lint/format, typecheck, coverage, migrations, scripts).
- `docs/agent-layer/CONTEXT.md` — general-purpose project-specific context (domain concepts, architectural context, naming conventions, external dependencies, team norms).

After this list, refer to memory files by filename only (ISSUES.md, BACKLOG.md, ROADMAP.md, DECISIONS.md, COMMANDS.md, CONTEXT.md).

**Formatting instructions are in each memory file.** Before writing to a memory file, open it and follow its “Purpose”/”Format” section and insertion markers.

## What NOT to store
Do not persist information that is derivable, ephemeral, or generic:
- Code patterns, architecture, file structure, or conventions discoverable by reading the codebase.
- Git history, recent changes, or authorship — use `git log` / `git blame`.
- Debugging solutions or fix recipes — the fix is in the code; the commit message has the why.
- Ephemeral task state that will not matter in a future session.
- Generic best practices or obvious conventions (e.g., “write clean code”, “use meaningful variable names”) — these waste tokens and dilute high-signal entries.
- Anything already enforced by deterministic tooling (linters, formatters, type checkers).

## Global memory workflow rules
- **Read before planning:** Before making architectural or cross-cutting decisions, read ROADMAP.md, then scan DECISIONS.md, then check relevant entries in BACKLOG.md and ISSUES.md.
- **Read before running commands:** Before running or recommending project workflow commands (tests, coverage, build, lint, start services), consult COMMANDS.md first.
- **Initialize if missing:** If any memory file does not exist, ask the user before creating it. If approved and templates exist, copy `.agent-layer/templates/docs/<NAME>.md` into `<NAME>.md` and preserve headings and markers.
- **Preserve & deduplicate:** Treat existing entries as canonical; do not overwrite/reset memory files unless the user explicitly asks (warn about data loss). Search the target file before adding; merge or rewrite existing entries instead of adding near-duplicates.
- **Decision hygiene:** Only log non-obvious decisions that are not apparent from code or docs. Do not log routine choices or best-practice adherence; when in doubt, skip logging. When a decision is superseded, replace the old entry with the new one and fold valuable tradeoff context into the replacement. Remove entries that become self-evident from the codebase. A compact decision log is more useful than a comprehensive one.
- **Write down deferred work:** If you discover something worth doing and are not fixing it now, or have not already captured the plan in ROADMAP.md:
  - add it to ISSUES.md if it is a bug, maintainability refactor, technical debt, reliability/security concern, test coverage gap, performance concern, or other engineering risk;
  - add it to BACKLOG.md only if it is a new end-user-visible capability.
- **Keep files living:** ISSUES.md and BACKLOG.md reflect the current working tree, not deployed state. Remove fixed issues and implemented backlog items alongside the code change; move scheduled backlog items into ROADMAP.md. Keep DECISIONS.md, CONTEXT.md, and COMMANDS.md current — when completing a task, remove or update entries that the change made stale.
- **Memory budget:** Memory files consume context tokens when read (~4 characters ≈ 1 token). When any single memory file's content below the insertion marker exceeds roughly 8,000 characters (~2,000 tokens), proactively consolidate: merge related entries, tighten verbose descriptions, and remove entries now derivable from code or docs. When the aggregate content across all memory files is large, prioritize pruning the biggest files first. Prefer concise entries with clear keywords — the agent can always read the code for full details.
