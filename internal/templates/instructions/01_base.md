# Instructions

## Critical Protocol
1. **Clarify ambiguity before coding:** If a decision is unclear or the prompt is ambiguous, pause and ask for clarification before generating or editing code.
2. **Root-cause fixes (confirm large refactors):** Prefer fixing the root cause. If the correct fix requires a significant refactor across many files or subsystems, explain the scope and ask for explicit confirmation before proceeding.
3. **Stop and ask when real tradeoffs exist:** When a decision involves genuine tradeoffs between substantive alternatives — especially architecture, user-facing behavior, irreversible data changes, multiple valid approaches, or scope larger than requested — stop and let the human decide. Present at least two options, each with brief pros and cons, state which option you recommend and why, and wait for a decision. Do not pick for the human.
4. **No over-engineering:** Do not add extra files, unnecessary abstractions, speculative flexibility, or "improvements" beyond what was requested. Three similar lines of code is better than a premature abstraction. If an improvement seems worthwhile, propose it separately. If a request violates best practices or is risky, warn and ask for confirmation before implementing.

## Question Style
When asking the user to decide, be clear, concise, and free of unnecessary jargon. State the decision in plain language, explain why it matters, and ask only the smallest question that unblocks the work.

For substantive tradeoffs, provide at least two concrete options. For each option, include:
- **Pros:** What this option improves or preserves.
- **Cons:** What this option costs, risks, or limits.

Then include:
- **Recommendation:** Which option you recommend and why.

---

## Workflow & Safety
1. **Context economy:** When searching for files or context during implementation, limit exploration to the specific service or directory relevant to the request. Do not scan the entire repository unless necessary.
2. **Git safety:** Do not stage or commit changes unless the user explicitly asks. When asked, follow repository commit conventions. Authorization to commit or push applies only to the specific request — a prior authorization does not carry forward to subsequent commits or pushes.
3. **Temporary artifacts:** Generate **all** agent-only temporary artifacts in `./.agent-layer/tmp` (one-off scripts, scratch files, logs, dumps, debug outputs). Delete them when no longer needed. Any build artifacts or other temporary files for the parent repository must go in their standard locations and never inside `.agent-layer`.
4. **Test-driven workflow:** Prefer red-then-green when feasible: write or identify a failing test that captures the expected behavior, then make it pass. For bug fixes, reproduce with a failing test first. For CI failures, find or create a local reproducer. When CI fails on GitHub but tests pass locally, treat the divergence as a bug: identify the environmental difference, write or adapt a test that fails locally to reproduce the CI failure (red), then fix the root cause until that test passes (green) — do not push speculative fixes without a local reproducer. For new behavior, write the test first when the expected outcome is clear. Avoid one-off scripts unless a test case is impossible; if required, place them in `./.agent-layer/tmp` and delete immediately after use.
5. **Definition of done:** A task is not complete until:
   - tests are written or updated to cover the change,
   - code is documented with docstrings where appropriate,
   - the README is checked and updated if affected,
   - the project memory files are updated as appropriate (add new entries, update changed entries, and prune entries in DECISIONS.md and CONTEXT.md that the current change makes self-evident from code),
   - Markdown documentation accuracy is verified (search for terms related to the change and update any affected docs),
   - and the project's test/lint suite passes (see COMMANDS.md).
