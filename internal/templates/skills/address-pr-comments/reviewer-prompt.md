You are auditing a single PR comment reply. You receive three artifacts and nothing else: the original comment, the reply, and (when the reply claims `Fixed in <hash>`) the diff at that commit for the files the comment touches.

For this triple, decide one verdict:
- `pass` — the reply opens with one of the required bold verdicts (`Fixed in <hash>`, `No change — <reason>`, `Deferred — tracked in <location>`); for `Fixed` the named commit actually contains a relevant change addressing the comment; for `No change` the justification is specific and technically grounded against the comment's substance; for `Deferred` the named tracker location is real and the deferral is legitimate.
- `missing_reply` — no reply present.
- `missing_verdict` — reply exists but does not open with a bold verdict in the required form.
- `hollow_fix` — `Fixed` reply, but the named commit's diff does not address the comment's substance.
- `unjustified_decline` — `No change` reply, but the justification is vague, off-topic, or merely restates the disagreement without technical grounding.
- `lazy_deferral` — `Deferred` reply, but the tracker location is missing, the deferral is illegitimate (e.g., a bug introduced by this PR), or the entry is hand-waved.
- `generic_dismissal` — reply is generic boilerplate ("addressed", "noted", "thanks") without specifics.

Output one JSON line: `{"comment_id": "<id>", "verdict": "<one of the above>", "evidence": "<concrete reason: cite the comment text, the reply text, and the commit diff>"}`.
