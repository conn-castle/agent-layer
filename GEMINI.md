<!--
  GENERATED FILE - DO NOT EDIT DIRECTLY
  Source: .agentlayer/instructions/*.md
  Regenerate: node .agentlayer/sync/sync.mjs
-->

<!-- BEGIN: 00_base.md -->
# Instructions (Ultimate Source of Truth)

## Guiding Principles
1. **Fail Loudly & Quickly:** Never make assumptions to keep the code running. If an error occurs, the system must fail immediately and audibly (console error, exception, etc.) rather than degrade silently. Silent failures are worse than no tests.
2. **Single Source of Truth:** Every piece of data (environment variables, database state, configuration, derived metrics) must have a single, canonical source. Never maintain separate mutable state; always derive it from the source.
3. **No Logic in Frontend:** The frontend is strictly a presentation layer. It must **never** compute metrics, logic, or business rules. It only displays pre-computed data from the backend.
4. **No Fallbacks:** Do not implement fallback logic or default values that mask missing data, environment variables, or constants. If required input is missing or malformed, the system must fail rather than guessing.
5. **Strict UTC Only:** All internal time representations, storage, and API transport must be in UTC. No timezone info leaks into the system.

---

## Critical Protocol
1. **Mandatory Q&A:** If the user asks a direct question, you MUST answer it EXPLICITLY in your response text. If the prompt also requires code generation, answer the questions FIRST before discussing the code.
2. **Clarify Ambiguity:** If a decision is unclear or the prompt is ambiguous, pause and ask the user for clarification before generating code.

## Code Quality & Philosophy
1. **Adhere to Best Practices:** Follow industry standards for the language/framework in use. If a request violates these, specifically warn the user and ask for confirmation.
2. **Prioritize Clarity:** Write well-abstracted code where appropriate, but prioritize clarity, readability, and extensibility over clever abstraction.
3. **No Band-Aids:** Never apply quick fixes. Address the root cause, even if it is a major undertaking.
    * *Constraint:* If the fix requires significantly refactoring many files, ask for confirmation first.
4. **Search for Excellence:** Always search for the best solution, not just the easiest one.
5. **Strict Scope:** The agent should only make changes that are directly requested. Keep solutions simple and focused.
6. Never reduce the code coverage percentage in order to pass the tests.
7. **Packages:** Always use the command line to find the latest versions of packages to use. Do not rely on your training or memory to pick a package version.
8. **Strict Typing:** All Python code must use type hints (`typing`), and all TypeScript/JS must use strict types. Function signatures must have docstrings describing arguments and return values. Do not rely on implicit typing.

## Workflow & Safety
1. **Code Verification:** ALWAYS read and understand relevant files before proposing edits. Do not speculate about code you have not inspected.
2. **Context Economy:** When searching for files or context, strictly limit your search to the specific service or directory relevant to the request. Do not scan the entire monorepo unless explicitly necessary.
3. **Git Safety:** Never commit changes to git. Explicitly ask the user to commit changes for you.
4. **Temporary Files:** Generate all temporary/debug files in the `./.agentlayer/tmp` directory.
5. **Schema Safety:** Never modify the database schema via raw SQL or direct tool access. Always generate a proper migration file (e.g., Alembic, Prisma, Django) and ask the user to apply it.
6. **Debugging Strategy:** Debugging follows a strict Red-Green loop: reproduce the bug with a persistent test case, then fix it. **Avoid one-off scripts** unless identifying the issue with a test case is impossible. If a one-off script is absolutely required, it must be generated in `./.agentlayer/tmp` and **deleted** immediately after use.
7. **Definition of Done:** A task is not complete until relevant Markdown files are updated and the code is fully documented.
8. **Environment Variables:** Never modify the .env file. Only ever modify the .env.example file. If a change needs to be made to the .env file, ask the user to make it.
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
