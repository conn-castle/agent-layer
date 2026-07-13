You are auditing one posted PR comment reply. Use only the supplied audit
package:

1. the exact original comment
2. the exact posted reply with its URL or ID
3. verdict-specific supporting evidence:
   - `Fixed`: the named commit hash and the relevant diff needed to evaluate the
     comment's substance
   - `No change`: the repository, specification, test, or documented-contract
     evidence supporting the reason
   - `Deferred`: the real tracking location and evidence that the work is
     outside this pull request

Do not assume missing evidence or inspect other context.

For this triple, decide one verdict:
- `pass` — the reply opens with exactly one required bold verdict, including its
  punctuation: `**Fixed in <short-hash>.**`, `**No change — <specific
  reason>.**`, or `**Deferred — tracked in <location>.**`; the supplied evidence
  proves the corresponding claim.
- `missing_reply` — no reply present.
- `missing_verdict` — reply exists but does not open with a bold verdict in the required form.
- `insufficient_evidence` — the audit package lacks the verdict-specific
  evidence needed to validate the claim.
- `hollow_fix` — `Fixed` reply, but the named commit's diff does not address the comment's substance.
- `unjustified_decline` — `No change` reply, but the justification is vague, off-topic, or merely restates the disagreement without technical grounding.
- `lazy_deferral` — `Deferred` reply, but the tracker location is missing, the deferral is illegitimate (e.g., a bug introduced by this PR), or the entry is hand-waved.
- `generic_dismissal` — reply is generic boilerplate ("addressed", "noted", "thanks") without specifics.

Output one JSON line: `{"comment_id": "<id>", "verdict": "<one of the above>", "evidence": "<concrete reason citing the original comment, posted reply, and applicable supporting evidence>"}`.
