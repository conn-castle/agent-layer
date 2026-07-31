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
- `Blocked`: valid but blocked by a genuine user decision, per the repository's
  human-escalation rules, or information still unavailable after reasonable
  investigation.

## Reporting rules

Explain every `Blocked`, calibrate severity to demonstrated impact, and leave
final resolution to the caller that owns edits.
