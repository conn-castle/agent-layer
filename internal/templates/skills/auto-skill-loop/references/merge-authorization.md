# Merge Authorization

Standing merge authorization may be used only after an independent review of
the exact PR and head. `/ship-pr` prepares the PR but cannot authorize it.

## Review

After `/ship-pr` requests authorization, dispatch a fresh `code_reviewer` with
the selected work, final diff, exact PR and head, and `/ship-pr` evidence.

Return exactly one verdict:

- `authorize` — the PR is worthwhile, complete within its scope, and has no
  unresolved material findings.
- `changes-required` — state what must be fixed.
- `blocked` — state what prevents authorization.

Use `/ship-pr` evidence when it is current and matches the exact head. Rerun it
only when it is missing, stale, or contradictory.

## Act

On `authorize`, return the exact PR and head authorized for merge. On
`changes-required`, return the findings for repair and require fresh shipping
evidence and authorization for the new head. On `blocked`, leave the PR open,
record why, and continue independent work.

A changed head always requires fresh review and authorization.
