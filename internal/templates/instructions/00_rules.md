# Rules

## Core Rules

- **No content substitution:** If you cannot access or fully read requested content, surface the failure and let the user decide.
- **Single source of truth:** Every piece of data must have one canonical source. Do not maintain separate mutable state when it can be derived from the canonical source.
- **Track complex work:** When long-running or complex work involves several items that must be remembered or revisited, create a temporary Markdown tracker in `.agent-layer/tmp` and keep it current until completion.
- **Unexpected repository changes:** Ignore unrelated working tree changes. Stop only when changes overlap files you are editing or could cause a conflict.
- **Destructive actions:** Never run or recommend destructive operations that can remove or overwrite large amounts of data without explicit confirmation from the user.
- **No silent fallbacks or hidden defaults:** Do not mask missing or invalid data with fallback behavior. Name defaults and constants explicitly instead of embedding them in implementation logic.
- **Fail loudly:** When code cannot perform required work or uphold an invariant, return or raise an actionable error. Do not swallow the failure, silently skip the work, or report partial execution as successful completion.

## Human Escalation

- **Stop and ask on substantive tradeoffs:** When at least two viable alternatives involve genuine tradeoffs, stop and ask the user to decide. Such tradeoffs commonly arise in architecture, end-user behavior, irreversible data changes, demoting log severity, and silencing errors or warnings.
- **Evaluate viability first:** Apply the current facts, requested scope, binding constraints, and repository defaults. The decision's category alone does not require escalation. If only one viable path remains, proceed without presenting a decision; never invent a weak alternative to create one.

When escalation is required, present the viable options in plain language using this exact format:

```md
**Decision:** <one direct question>

<minimum context needed to decide>

**Option 1: <name>**
- Pros: <...>
- Cons: <...>

**Option 2: <name>**
- Pros: <...>
- Cons: <...>

**Recommendation:** Option <n>, because <reason tied to the user's priorities>.
```

Repeat the option block for additional options. Ask routine questions without meaningful tradeoffs concisely.

## Communication Style

- Give the user enough context to act.
- Assume the user has not read the code, command output, or prior implementation details.
- Spell out acronyms unless they are widely known or the user already used them.
