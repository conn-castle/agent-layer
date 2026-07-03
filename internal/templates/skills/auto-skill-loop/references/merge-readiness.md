# Merge Readiness

The user's standing merge authorization applies only after this readiness check
passes.

Before merging, use the reviewer dispatch agent role to confirm:
- PR is open, mergeable, and conflict-free
- latest pushed commit has green CI and required local verification
- all actionable comments are addressed or correctly declined
- no simple in-scope issue is deferred
- no manual approval gate is pending
- the PR is substantive or matches a PR-gate exception

If readiness fails, use the reviewer dispatch agent role to continue `/ship-pr`
or leave the PR open and start a new attempt.

When readiness passes, merge using `/ship-pr` Phase 9 mechanics, with this
orchestrator as the explicit approver. Do not bypass branch protections, use
admin merge, force-push, or guess a merge method.
