# Merge Authorization

Verify that `/ship-pr` completed for the exact PR and head: all PR feedback is
fully addressed, and current evidence shows the PR meets its merge-readiness
requirements. Do not inspect the selected work or final diff; this is a process
gate, not another review.

If `/ship-pr`'s `references/repo-specific-pr-policy.md` exists, read it and
verify that all of its requirements are also met.

State concisely whether the exact PR and head are authorized for merge. If not
authorized, give the reason.
