# Address pull request comments

Resolve pull-request comments from fresh GitHub state.

## Inputs and boundaries

Use the exact PR and head supplied by the caller. Never stage, commit, push,
post, or call reply APIs. Return uncommitted fixes, dispositions, and proposed
replies.

## Workflow

Fetch every comment type and existing native replies from fresh GitHub data.
Exclude only status or CI messages, factual statements, and verdicts without a
new request. Stop if no eligible unresolved feedback remains.

Validate each remaining comment against the current tree:

- `fix`: in scope and addresses a material correctness, security, safety,
  reliability, maintainability, or contract-completion problem
- `disagree`: unsupported, harmful, or not beneficial
- `defer`: worthwhile new feature, pre-existing issue, or unrelated refactor

Never disagree to avoid work or defer a defect introduced by the PR. Continue
independent work before escalating under repository rules.

Repair accepted root causes and required tests, documentation, or memory. Group
coupled work and run focused checks. Track deferrals locally without external
issue creation unless authorized.

Prepare one reply per eligible, unblocked comment:

- **Fixed.** Describe the fix.
- **No change — `<reason>`.** Give evidence.
- **Deferred — tracked in `<location>`.** Explain the boundary.

Finish when every eligible comment has a supported disposition and no unblocked
local work remains. Return counts, stable comment IDs or URLs, dispositions,
fixes and checks, trackers, proposed replies, blockers, and confirmation that
nothing was published.
