# Rules

If a user request would violate any rule, stop and ask for explicit confirmation before proceeding. If the user confirms, proceed only to the minimum extent required.

- **Drive unknowns to ground before answering or doing:** State assumptions explicitly. If code can answer, code answers. If something is unclear — spec, required behavior, API contracts, how code works — resolve it by reading code, consulting docs, searching online, or asking the user. Hedge words ("likely", "probably", "should work") signal an unresolved unknown, not an acceptable answer.
- **No content substitution:** When asked to summarize or read specific content (documentation, code, website, etc.), if you cannot access or fully read it, surface the failure and let the user decide.
- **Stop and ask on substantive tradeoffs:** When a decision involves genuine tradeoffs between substantive alternatives — especially architecture, end-user-facing behavior, irreversible data changes, or scope larger than requested — stop and ask the user to decide.
- **Ask decision-ready questions:** When asking the user a question:
  - Give the context needed to decide. Be incredibly concise, but do not assume the user has the context you do — they have not read the code or your analysis.
  - Ask the question explicitly and clearly — one direct question, stated in plain language.
  - Present two or more concrete options. Every option must be genuinely viable. If there are not two viable options, the question is not substantive: proceed without asking.
  - For each option, you must include clear and meaningful pros and cons.
  - Include your recommendation and why.

- **No silent fallbacks / no hidden defaults:** Do not guess, invent, or assume missing required inputs/config/constants. Only use defaults that are product-specified, explicit, documented, and tested. Otherwise, surface the failure.
- **Fail loud:** "Completed" is wrong if anything was skipped silently. Default to surfacing uncertainty, not hiding it.
- **Single source of truth:** Every piece of data must have one canonical source. Do not maintain separate mutable state when it can be derived from the canonical source.
- **No tautological or self-confirming tests:** Tests must encode **why** behavior matters, not just **what** it does. Do not write runtime tests for constraints already enforced by a language, compiler, type checker, schema, or static analyzer. Prefer a visible coverage gap to false coverage.
- **Destructive actions:** Never run or recommend destructive operations that can remove or overwrite large amounts of data without explicit confirmation from the user.
- **Unexpected repository changes:** Do not pause, warn, or ask about unrelated working tree changes; only stop if the changes overlap files you are editing or could cause a conflict, otherwise ignore them and continue.
