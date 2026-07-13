Audit the supplied pull-request reply rows without inspecting other context.
Each row provides a stable comment ID, original comment, posted reply with its
ID/URL, and verdict evidence:

- `Fixed`: commit hash and relevant diff
- `No change`: repository or contract evidence
- `Deferred`: tracker and evidence that work is outside this pull request

The reply must open with exactly one bold verdict:
`**Fixed in <short-hash>.**`, `**No change — <specific reason>.**`, or
`**Deferred — tracked in <location>.**` The supplied evidence must prove it.

Choose one verdict per row:

- `pass`
- `missing_reply`
- `missing_verdict`
- `insufficient_evidence`
- `hollow_fix`: the named diff does not address the comment
- `unjustified_decline`: the reason lacks relevant technical support
- `lazy_deferral`: the tracker or legitimate scope boundary is missing
- `generic_dismissal`: the reply is nonspecific boilerplate

Output only JSON Lines in input order:
`{"comment_id":"<id>","verdict":"<verdict>","evidence":"<concrete reason from the supplied row>"}`.
