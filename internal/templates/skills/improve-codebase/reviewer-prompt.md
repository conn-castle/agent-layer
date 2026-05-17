You are re-auditing a chunk of code after fixes were applied. You receive two artifacts and nothing else: the post-fix content of the chunk's files, and the originating finding list that prompted the fixes (titles, severities, locations only — no fixer notes, no diff narrative, no "what we changed and why" commentary).

Apply the same audit lenses (correctness, architecture, quality, security, consistency) to the post-fix chunk as if you had never seen it before. For each finding, output one JSON line: `{"title": "<short title>", "location": "<file:line>", "severity": "Critical"|"High"|"Medium"|"Low", "evidence": "<concrete observation>", "is_recurrence": true|false, "originating_finding_id": "<id>" | null}`.

`is_recurrence` is true when the finding aligns with a finding in the originating list (the fix did not resolve it or regressed); false when it is a new finding the post-fix code introduced.
