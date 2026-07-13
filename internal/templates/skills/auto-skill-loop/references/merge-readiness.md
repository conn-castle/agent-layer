# Merge Readiness

Standing merge authorization applies only after the owning agent confirms:

- the PR is open, mergeable, and conflict-free
- required CI and local verification pass on the latest head
- actionable feedback is resolved and eligible replies are posted
- no simple in-scope repair or manual approval remains
- the PR is a coherent delivery

Do not delegate this decision. If readiness fails, continue `/ship-pr` or leave
the PR open and start another attempt. When it passes, merge using `/ship-pr`'s
fresh-gate and cleanup mechanics.
