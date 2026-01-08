# Instructions (Ultimate Source of Truth)

## Guiding Principles
1. **Fail Loudly & Quickly (Including Production):** Never make assumptions to keep the system running. If required input is missing, malformed, or inconsistent, the system must fail immediately and audibly (exception, error response, visible log). Silent failures are worse than failing tests.
2. **Single Source of Truth:** Every piece of data (environment variables, database state, configuration, derived metrics) must have one canonical source. Do not maintain separate mutable state when it can be derived from the canonical source.
3. **Frontend Is Presentation, Not Business Rules:** The frontend must not contain business rules, authoritative computations, or metric derivations. Normal frontend behavior is allowed (presentation formatting, input validation, view-state management, sorting for display, and showing local time while storing and transporting time in Coordinated Universal Time).
4. **No Silent Fallbacks:** Do not implement fallback logic or default values that hide missing data, missing required configuration, or incorrect constants. If a default is part of the product specification, it must be explicit, documented, and tested (not an implicit guess).
5. **Strict Coordinated Universal Time Internals:** All internal time representations, storage, and application programming interface transport must use Coordinated Universal Time. The frontend may display local time as a presentation concern, derived from Coordinated Universal Time.

---

## Critical Protocol
1. **Mandatory Questions and Answers:** If the user asks a direct question, you must answer it explicitly in the response text. If the prompt also requires code generation, answer the questions first before discussing the code.
2. **Clarify Ambiguity Before Coding:** If a decision is unclear or the prompt is ambiguous, pause and ask the user for clarification before generating or editing code.
3. **Root Cause Fixes With Confirmation for Large Refactors:** Prefer fixing the root cause even if the user asked for a small change. If the correct root-cause fix requires a significant refactor across many files or subsystems, explain the refactor scope and ask the user for confirmation before proceeding.

---

## Code Quality & Philosophy
1. **Adhere to Best Practices:** Follow widely accepted standards for the language and framework in use. If a request violates best practices, warn the user and ask for confirmation before implementing the risky approach.
2. **Prioritize Clarity:** Write clear, readable, and extensible code. Avoid cleverness that reduces maintainability.
3. **No Band-Aids:** Do not apply quick fixes that avoid the root cause. If the root-cause fix requires a large refactor, ask for confirmation first (see Critical Protocol).
4. **Search for Excellence:** Always look for the best solution, not just the easiest one. This can conflict with simplicity and strict scope; surface the tradeoff explicitly, choose a well-justified approach, and ask for confirmation when the best solution expands scope significantly.
5. **Strict Scope By Default:** Only make changes that are directly requested and necessary. If the root-cause fix expands scope, ask for confirmation before proceeding.
6. **Test Coverage Integrity:** Do not reduce the minimum allowed code coverage threshold to make tests pass. Write tests and fix the code instead.
7. **Packages (Latest Compatible Stable Versions):** Determine package versions using the package manager and official tooling, not memory. Prefer the latest stable compatible versions. Avoid unstable or pre-release versions. If the latest stable version introduces breaking changes, ask the user for confirmation and, once confirmed, fix what is broken and make the runtime compatible when feasible (including upgrading runtime versions if appropriate).
8. **Strict Typing and Documentation:** All Python code must use type hints. All TypeScript or JavaScript must use strict types. Public functions and non-trivial internal functions must include docstrings describing arguments and return values.

---

## Workflow & Safety
1. **Code Verification:** Always read and understand relevant files before proposing or making edits. Do not speculate about code you have not inspected.
2. **Context Economy:** When searching for files or context during implementation, limit exploration to the specific service or directory relevant to the request. Do not scan the entire repository unless necessary.
3. **Git Safety:** Never commit changes. Ask the user to commit changes.
4. **Temporary Files:** Generate all temporary or debugging files in `./.agentlayer/tmp`.
5. **Schema Safety:** Never modify the database schema via raw structured query language or direct tool access. Always generate a proper migration file using the project’s migration system, and ask the user to apply it.
6. **Debugging Strategy:** Debugging follows a strict red-then-green loop: reproduce the bug with a persistent automated test case, then fix it. Avoid one-off scripts unless a test case is impossible. If a one-off script is required, place it in `./.agentlayer/tmp` and delete it immediately after use.
7. **Definition of Done:** A task is not complete until:
   - tests are written or updated to cover the change,
   - code is documented with docstrings where appropriate,
   - the README is checked and updated if affected,
   - the project memory files (features, issues, roadmap, decisions) are updated as appropriate,
   - and Markdown documentation accuracy is verified using targeted repository-wide search (not manual review of every file):
     - search the repository for terms related to the change (feature name, endpoint names, module names, command names, environment variable names, configuration keys, and any user-facing terms),
     - review every documentation hit for accuracy,
     - and update any Markdown files that are now incorrect.
8. **Environment Variables:** Never modify the `.env` file. Only modify the `.env.example` file. If a change is needed in `.env`, ask the user to make it.
