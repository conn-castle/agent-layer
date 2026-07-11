# Finding Verdict Classification

Use this rubric once while synthesizing `/review-uncommitted-code` candidates.
It does not create a separate classifier or review pass.

## Evidence gate

Before reporting a candidate:

1. Locate and inspect its cited evidence in the current tree.
2. Confirm the concern exists in the reviewed state and scope.
3. Confirm it materially affects correctness, safety, scope, reliability,
   performance, test integrity, or meaningful maintainability.
4. Merge it with any duplicate candidate.
5. Determine whether resolving it requires a user-owned decision.

Discard candidates that are unsupported, merely stylistic, speculative,
out-of-scope, unrelated known issues, or based on stale evidence. They are not
findings and do not need report entries.

## Verdicts

Assign exactly one verdict to every reported finding:

- `Accept`: valid now, supported by concrete evidence, within reviewed scope,
  and actionable without a new user decision.
- `Defer`: valid, but blocked by a user-owned decision, an explicit scope
  boundary, or information that cannot be established during this review.

Do not defer merely because a fix is broad, and do not accept a finding merely
because several reviewers agree. Evidence settles the verdict.

## Reporting rules

- Preserve the strongest evidence when merging duplicates.
- Explain every non-accepted verdict concretely.
- Make Critical or High severity proportional to demonstrated impact.
- Findings remain recommendations; the caller that owns edits makes the final
  resolution after validating current state.
