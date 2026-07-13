# Finding Verdict Classification

Apply this rubric while synthesizing `/review-uncommitted-code`; it is not a
separate review pass.

## Evidence gate

Inspect cited current-tree evidence, confirm the concern is in scope and
material, merge duplicates, and determine whether resolution needs a user-owned
decision. Discard unsupported, stylistic, speculative, stale, out-of-scope, or
unrelated candidates.

## Verdicts

Assign exactly one verdict:

- `Accept`: current, evidenced, in scope, and actionable without a new user
  decision.
- `Defer`: valid but blocked by a genuine user decision or information still
  unavailable after reasonable investigation.

A scope boundary alone is not a user-owned decision. Escalate only for an
external write, a destructive action, a substantive product or architecture
choice, or material expansion beyond the requested scope, per the repository's
human-escalation rules. Breadth and reviewer agreement do not determine the
verdict; evidence does.

## Reporting rules

Preserve the strongest evidence when merging. Explain every `Defer`, calibrate
severity to demonstrated impact, and leave final resolution to the caller that
owns edits.
