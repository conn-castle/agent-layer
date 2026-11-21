---
name: instruction-sync
description: Manage ordered local and Git-backed Agent Layer instructions. Use for known instruction imports, status, diff, pull, resolve, reset, push, removal, and projection.
compatibility: Requires al in an initialized Agent Layer project; Git access for remote operations.
allowed-tools: Bash(al instructions *) Bash(al sync)
---

<!-- agent-layer-catalog-skill: instruction-sync -->

# Instruction sync

Read `al instructions --help` and the relevant subcommand help before operating.
Start with `al instructions status --all`, which is offline. `al sync` projects
current local content without fetching. Use `pull` to update imports.

`al instructions add <repository> <exact/path.md> --order <integer> --yes`
imports one regular Markdown file. Order is required, nonnegative, globally
unique across imported and local blocks; zero and gaps are valid. Source ref,
tracking, write policy, push repository and push branch use the skills defaults
and flags. Order is configuration-owned and never published upstream.

`diff <filename.md>` accepts `--from` and `--to` base, local, upstream, or
destination. `pull` preserves edits through a three-way merge. For a conflict,
resolve the reported Git workspace index, then run `resolve <filename.md>`.
`reset <filename.md> --yes` discards local imported edits. `remove <repository>
<selector> --yes` removes one import block; modified files block retirement.
`push --yes` respects configured write policies and excludes project-local files.

Use destructive commands and upstream writes only when the user authorizes them.
Import only known repositories/files; do not search or recommend sources.

Project-owned files stay in `.agent-layer/instructions/`, each declared once in
`[[instructions.local]]` with `selectors = ["filename.md"]` and `order = N`.
Imports live in `.agent-layer/instructions-imported/` with shared lifecycle state
in `instructions.lock.json`. Edit imported files there, then sync.

`al upgrade plan` previews conversion of eligible unmodified standard rules and
memory files to Git imports. Apply it with `al upgrade` when a project upgrade is
in scope. Accepted wizard changes can establish local instruction ordering and
add missing imports. Selecting **None** preserves existing instructions. Use the
CLI to maintain the lockfile.
