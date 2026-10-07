# Merge Authorization

Verify that `/ship-pr` completed for the exact PR and head: all PR feedback is
fully addressed, and current evidence shows the PR meets its merge-readiness
requirements. Do not inspect the selected work or final diff; this is a process
gate, not another review.

Read the PR's current comments, such as with `/ship-pr`'s
`scripts/read-pr-comments.sh`, and verify that the request includes a complete
ledger: every eligible request, including one embedded in an automated approval,
review summary, or pre-merge checklist, appears with its source link, request
text, canonical disposition with evidence, and a link to a posted reply that
supports it. Apply eligibility from `/ship-pr`'s
`references/address-pr-comments.md`: status or CI messages, factual statements,
and verdicts without a new request need no entry, and review state does not
decide eligibility. A reply supports its request when it begins with the
canonical disposition and states its fixing commit, tracker, or reasoning; do
not re-judge its merits. Supported `Deferred.` and `Disagreed.` dispositions
are acceptable; a bot's threshold, such as a coverage percentage, is not
repository policy. A missing or incomplete ledger is not authorized.

If `/ship-pr`'s `references/repo-specific-pr-policy.md` exists, read it and
verify that all of its requirements are also met.

State concisely whether the exact PR and head are authorized for merge. If not
authorized, give the reason.
