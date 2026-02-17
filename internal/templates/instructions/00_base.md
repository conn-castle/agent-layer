# Instructions (Ultimate Source of Truth)

## Guiding Principles
1. **Fail loudly & quickly (code + MCP servers + chat, including production):** Never guess to keep the system running. If required input/config/state is missing, malformed, or inconsistent, stop and surface a clear error (exception, error response, or explicit message). Silent failure is worse than failing tests.
2. **Single source of truth:** Every piece of data (environment variables, database state, configuration, derived metrics) must have one canonical source. Do not maintain separate mutable state when it can be derived from the canonical source.
3. **Frontend is presentation, not business rules:** The frontend must not contain business rules, authoritative computations, or metric derivations. Allowed: presentation formatting, input validation, view-state management, and sorting for display.

---

## Critical Protocol
1. **Mandatory questions and answers:** If the user asks a direct question, answer it explicitly in the response text before proposing or generating changes.
2. **Clarify ambiguity before coding:** If a decision is unclear or the prompt is ambiguous, pause and ask for clarification before generating or editing code.
3. **Root-cause fixes (confirm large refactors):** Prefer fixing the root cause. If the correct fix requires a significant refactor across many files or subsystems, explain the scope and ask for explicit confirmation before proceeding.

---

## Code Quality & Philosophy
1. **Adhere to best practices:** Follow widely accepted standards for the language and framework in use. If a request violates best practices or is risky, warn and ask for confirmation before implementing.
2. **Prioritize clarity:** Write clear, readable, and extensible code. Avoid cleverness that reduces maintainability.
3. **Strict scope by default:** Only make changes that are directly requested and necessary. If the correct root-cause fix expands scope, ask for confirmation.
4. **Test coverage integrity:** Do not reduce the minimum allowed code coverage threshold to make tests pass. Write tests and fix the code instead.
5. **Packages (latest compatible stable versions):** Determine package versions using the package manager and official tooling/docs, not memory. Prefer the latest stable compatible versions. Avoid unstable or pre-release versions. If the latest stable version introduces breaking changes, ask for confirmation and then do the compatibility work.
6. **Strict typing and documentation:** Python code must use type hints. TypeScript/JavaScript must use strict types. Public functions and non-trivial internal functions must include docstrings describing arguments and return values.

---

## Workflow & Safety
1. **Read before editing; don’t speculate:** Read and understand relevant files before proposing or making edits. Do not invent code you have not inspected.
2. **Context economy:** When searching for files or context during implementation, limit exploration to the specific service or directory relevant to the request. Do not scan the entire repository unless necessary.
3. **Git safety:** Do not stage or commit changes unless the user explicitly asks. When asked, follow repository commit conventions.
4. **Temporary artifacts:** Generate **all** agent-only temporary artifacts in `./.agent-layer/tmp` (one-off scripts, scratch files, logs, dumps, debug outputs). Delete them when no longer needed. Any build artifacts or other temporary files for the parent repository must go in their standard locations and never inside `.agent-layer`.
5. **Schema safety:** Never modify the database schema via raw SQL or direct tool access. Always generate a proper migration file using the project’s migration system, and ask the user to apply it.
6. **Debugging strategy:** Debugging follows a strict red-then-green loop: reproduce the bug with a persistent automated test case, then fix it. Avoid one-off scripts unless a test case is impossible. If a one-off script is required, place it in `./.agent-layer/tmp` and delete it immediately after use.
7. **Definition of done:** A task is not complete until:
   - tests are written or updated to cover the change,
   - code is documented with docstrings where appropriate,
   - the README is checked and updated if affected,
   - the project memory files are updated as appropriate,
   - and Markdown documentation accuracy is verified using targeted repository-wide search:
     - search for terms related to the change (feature name, endpoint names, module names, command names, environment variable names, configuration keys, user-facing terms),
     - review every documentation hit for accuracy,
     - and update any Markdown files that are now incorrect.
