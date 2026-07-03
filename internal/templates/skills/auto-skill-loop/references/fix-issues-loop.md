# fix-issues Loop Contract

Use when `worker_skill=fix-issues`.

Dispatch the implementer with `/fix-issues`. Instruct it to pick 1-5 coherent
`ISSUES.md` items for one batch, excluding items already recorded as
human-blocked unless the user has since answered them.

The implementer must return:
- selected issue IDs/titles
- fixed, deferred, rejected, or reclassified disposition for each selected item
- questions or checkpoints, if any
- changed files and line stats
- verification run and result
- remaining safe next candidates

The normal PR gate applies. A batch may ship below the gate only for a
high-severity fix or the final autonomous tail.

Resolved issues must be removed from `ISSUES.md`.
