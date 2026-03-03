# Instructions

## Guiding Principles
1. **Fail loudly & quickly (code + MCP servers + chat, including production):** Never guess to keep the system running. If required input/config/state is missing, malformed, or inconsistent, stop and surface a clear error (exception, error response, or explicit message). Silent failure is worse than failing tests.

---

## Critical Protocol
1. **Clarify ambiguity before coding:** If a decision is unclear or the prompt is ambiguous, pause and ask for clarification before generating or editing code.
2. **Root-cause fixes (confirm large refactors):** Prefer fixing the root cause. If the correct fix requires a significant refactor across many files or subsystems, explain the scope and ask for explicit confirmation before proceeding.
3. **Stop and ask when real tradeoffs exist:** When a decision involves genuine tradeoffs — especially architecture, user-facing behavior, irreversible data changes, multiple valid approaches, or scope larger than requested — explain the options with pros/cons and let the human decide.

---

## Code Quality & Philosophy
1. **Adhere to best practices:** Follow widely accepted standards for the language and framework in use. If a request violates best practices or is risky, warn and ask for confirmation before implementing.
2. **Prioritize clarity:** Write clear, readable, and extensible code. Avoid cleverness that reduces maintainability.
3. **Strict scope by default:** Only make changes that are directly requested and necessary. If the correct root-cause fix expands scope, ask for confirmation.
4. **No over-engineering:** Do not add extra files, unnecessary abstractions, speculative flexibility, or "improvements" beyond what was requested. Three similar lines of code is better than a premature abstraction. If an improvement seems worthwhile, propose it separately.

---

## Workflow & Safety
1. **Read before editing; don’t speculate:** Read and understand relevant files before proposing or making edits. Do not invent code you have not inspected.
2. **Context economy:** When searching for files or context during implementation, limit exploration to the specific service or directory relevant to the request. Do not scan the entire repository unless necessary.
3. **Git safety:** Do not stage or commit changes unless the user explicitly asks. When asked, follow repository commit conventions. Authorization to commit or push applies only to the specific request — a prior authorization does not carry forward to subsequent commits or pushes.
4. **Temporary artifacts:** Generate **all** agent-only temporary artifacts in `./.agent-layer/tmp` (one-off scripts, scratch files, logs, dumps, debug outputs). Delete them when no longer needed. Any build artifacts or other temporary files for the parent repository must go in their standard locations and never inside `.agent-layer`.
5. **Debugging strategy:** Debugging follows a strict red-then-green loop: reproduce the bug with a persistent automated test case, then fix it. Avoid one-off scripts unless a test case is impossible. If a one-off script is required, place it in `./.agent-layer/tmp` and delete it immediately after use.
6. **Definition of done:** A task is not complete until:
   - tests are written or updated to cover the change,
   - code is documented with docstrings where appropriate,
   - the README is checked and updated if affected,
   - the project memory files are updated as appropriate,
   - Markdown documentation accuracy is verified (search for terms related to the change and update any affected docs),
   - and the project's test/lint suite passes (see COMMANDS.md).
