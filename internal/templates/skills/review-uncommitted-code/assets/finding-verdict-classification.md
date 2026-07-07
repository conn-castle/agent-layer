# Finding Verdict Classification

Use this asset when synthesizing a review report. Treat every finding from the
lead reviewer or review agents as a candidate until it has been checked against
the current repo state.

The report must keep all candidates visible, but group them by recommended
verdict. Verdicts are reviewer recommendations only; a later fixer or resolver
owns the final decision.

## Verdicts

Assign exactly one recommended verdict to each candidate:

- `Accept`: valid now, supported by concrete evidence, tied to the reviewed
  scope, and actionable without a new human decision.
- `Reject`: not valid, not actionable, unsupported by the cited evidence,
  based on incorrect evidence, merely stylistic, already tracked without being
  introduced or worsened by the reviewed target, or outside the review contract.
- `Defer`: valid, but blocked by a human checkpoint, an explicit scope
  boundary, missing information that cannot be resolved during review, or a
  substantive tradeoff that the user must decide.
- `Already Resolved`: valid for an earlier state or intermediate reviewer
  observation, but no longer present in the current repo state.

Do not accept a finding just because it sounds plausible.

## Classification Workflow

For each candidate:

1. Locate the cited evidence.
2. Inspect the current repo state directly.
3. Decide whether the issue exists now.
4. Check whether the issue is inside the reviewed scope.
5. Check whether the recommendation is actionable without a human checkpoint.
6. Assign one recommended verdict and record a concrete reason.

Only `Accept` findings are real current findings for the review's action
summary. `Reject`, `Defer`, and `Already Resolved` candidates stay in the
report for transparency, but they must not be counted as accepted findings or
presented as work the user should fix now.

## Reporting Rules

- Preserve every candidate in exactly one verdict group.
- Make clear that verdicts are recommendations, not final resolution.
- Explain every non-accepted verdict in concrete terms.
- Merge duplicates before reporting; when duplicate candidates disagree, keep
  the strongest evidence and mention the duplicate source in the reason.
- If a Critical or High candidate is not accepted, make the reason especially
  explicit.
- Do not silently drop weak findings. Reject them with a reason.
- Do not defer merely because the fix might be broad. Defer only for a real
  checkpoint, explicit scope boundary, or unresolved information gap.
