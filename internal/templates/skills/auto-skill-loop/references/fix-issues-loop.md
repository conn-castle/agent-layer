# fix-issues Loop Contract

Use when `worker_skill=fix-issues`.

Run `/fix-issues` through the configured implementer, a built-in agent, or local
work. Select a coherent group of live ISSUES.md items, excluding recorded
user-blocked items until the user answers them.

Reconcile the result with the current tree. Preserve at least:

- selected items and each terminal disposition
- changed files and verification evidence
- user-only decisions and safe next candidates

Remove resolved items from ISSUES.md. Record deferrals in the run state or
worker artifact, not as annotations in ISSUES.md. Ship when the batch is a
coherent review unit, including a small high-severity fix or final tail.
