# Context

Note: This is an agent-layer memory file. It is primarily for agent use.

## Purpose

Persistent project-specific knowledge that does not belong in ISSUES, BACKLOG,
DECISIONS, COMMANDS, canonical repository documentation, or the implementation.
Read this file before starting work on a task.

Record only facts, nuances, or lessons that an agent needs across sessions and
cannot reasonably discover from the repository's canonical documentation,
code, tests, schemas, or configuration.

Do not duplicate information that belongs elsewhere:

- Current architecture or product behavior → repository documentation
- Enforceable behavior or invariants → code, tests, schemas, or configuration
- Otherwise-lost rationale that constrains future work → DECISIONS.md
- Deferred bugs or tech debt → ISSUES.md
- Planned features → BACKLOG.md
- Workflow commands → COMMANDS.md

## Format

- Organize by topic using headings (`##`, `###`).
- Prefer concise bullet points. State facts directly; omit hedging language.
- Before adding an entry, search the repository for existing coverage. Update
  the canonical source instead of copying it here.
- Remove or update entries when the underlying facts become documented,
  implemented, or no longer apply.
- Insert all content below `<!-- ENTRIES START -->`.

<!-- ENTRIES START -->
