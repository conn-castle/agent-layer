<!--
  GENERATED FILE - DO NOT EDIT DIRECTLY
  Source: .agentlayer/instructions/*.md
  Regenerate: node .agentlayer/sync/sync.mjs
-->

<!-- BEGIN: 00_base.md -->
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
<!-- END: 00_base.md -->

<!-- BEGIN: 01_memory.md -->
# Dedicated Memory Section (paste into system instructions)

## Project memory files (authoritative, user-editable, agent-maintained)
- `.agentlayer/docs/ISSUES.md` — deferred defects, maintainability refactors, technical debt, risks.
- `.agentlayer/docs/FEATURES.md` — deferred user feature requests (near-term and backlog).
- `.agentlayer/docs/ROADMAP.md` — phased plan of work; guides architecture and sequencing.
- `.agentlayer/docs/DECISIONS.md` — rolling log of important decisions (brief).

## Operating rules
1. **Read before planning:** Before making architectural or cross-cutting decisions, read `ROADMAP.md`, then scan `DECISIONS.md`, and then check relevant entries in `FEATURES.md` and `ISSUES.md`.
2. **Write down deferred work:** If you discover something worth doing and you are not doing it now:
   - Add it to `ISSUES.md` if it is a bug, maintainability refactor, technical debt, reliability, security, test coverage gap, performance concern, or other engineering risk.
   - Add it to `FEATURES.md` only if it is a new user-visible capability.
3. **Maintainability refactors are always issues:** Do not put refactors in `FEATURES.md`.
4. **Keep entries compact and readable:** Each issue and feature entry should be **3 to 5 lines**:
   - Line 1: Identifier and short title.
   - Line 2: Priority (Critical, High, Medium, Low) and area.
   - Line 3: Short description focused on the observed problem or requested capability.
   - Line 4: Next step (for issues) or acceptance criteria (for features).
   - Line 5: Optional dependencies or notes (only if needed).
5. **No abbreviations:** Avoid abbreviations in these files. Prefer full words and short sentences.
6. **Prevent duplicates:** Search the target file before adding a new entry. Merge or rewrite existing entries instead of adding near-duplicates.
7. **Keep files living:** When an issue is fixed or a feature is implemented, remove it from `ISSUES.md` or `FEATURES.md`.
8. **Roadmap phase behavior:**
   - Active and upcoming phases use checkbox task items.
   - When a phase is complete, remove the checkbox items and replace them with a short summary so the file does not grow without bound.
   - Completed phases remain listed (summarized) for context.
9. **Decision logging:** When making a significant decision (architecture, storage, data model, interface boundaries, dependency choice), add an entry to `DECISIONS.md` with decision, reason, and tradeoffs. Keep it brief.
10. **Agent autonomy:** You may propose and apply updates to the roadmap, features, issues, and decisions when it improves clarity and delivery, while keeping the documents compact.
<!-- END: 01_memory.md -->

<!-- BEGIN: 02_rules.md -->
```md
# Rules

These rules are mandatory and apply to all work: editing files, generating patches, running commands, and proposing changes. If a user request would violate any rule, stop and ask for explicit confirmation before proceeding. If the user confirms, proceed only to the minimum extent required.

Keep this document as a single flat bullet list. When adding a new rule, add a new bullet anywhere it improves readability. Do not create subsections or nested bullet lists. Keep each rule readable in one to two sentences.

- **Environment files:** Never modify the `.env` file. Only modify the `.env.example` file. If a change is needed in `.env`, ask the user to make the change and provide exact, copyable instructions.
- **Repository boundary:** Never delete files outside of the repository. If a file outside of the repository needs to be deleted, ask the user to delete it.
- **Secrets and credentials:** Never add secrets, private keys, access tokens, or credentials to repository files, logs, or outputs. Use placeholders and documented variable names in `.env.example`, and instruct the user to supply real values locally.
- **Destructive actions:** Never run or recommend destructive operations that can remove or overwrite large amounts of data without explicit confirmation from the user, and always name the exact paths that would be affected.
- **Verification claims:** Never claim that you ran commands, tests, or verification unless you actually did and observed the output. If you did not run verification, state what should be run and why.
```
<!-- END: 02_rules.md -->
