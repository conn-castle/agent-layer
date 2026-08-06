---
name: skill-sync
description: Manage local skills backed by known remote Git repositories. Use when the user wants to import, inspect, pull, push, reset, or remove them, or project current local skills to enabled clients with `al sync`. Do not use to discover new skills or create user-owned skills.
compatibility: Requires `al` in an initialized Agent Layer project and Git access for remote operations.
allowed-tools: Bash(al:*)
---

<!-- agent-layer-catalog-skill: skill-sync -->

# Skill Sync

Use this skill to import known skills from Git and manage them over time. Users
can edit imported skills locally, pull upstream updates without overwriting
those edits, pin imports to a chosen version, reset them to upstream, or publish
changes back.

Use live `al skills --help` and subcommand help as the source of truth. If `al`
is unavailable or the project is not initialized, stop and report the error.
Do not replace an `al skills` operation with manual file edits.

There is no `al skills sync` command. Use `al skills pull` to update skills from
Git. Use `al sync` only to copy the current local skills into enabled client
directories.

1. Start with `al skills status --all`. It reads local state without using the
   network.
2. To import skills, run `al skills add <repository> <selector>...` only when the
   repository and selectors are known. Quote selectors that contain wildcards
   or start with `!`. The command does not search for, recommend, or preview
   skills. Run `al skills add --help` before choosing source, tracking, or
   publishing options.
3. To update imported skills, run `al skills pull`. It fetches every configured
   source and merges upstream changes with local edits. Pinned imports stay at
   their locked versions unless their configured `ref` changes. Treat any
   partial or conflicted result as a failure and report every result.
4. To stop managing one selector, run
   `al skills remove <repository> <selector>`, using the exact values from
   `.agent-layer/config.toml`. Skills still matched by another selector remain
   managed. Clean skills that are no longer matched are deleted. If one has
   local edits, the command fails and preserves it. Removing an exclusion may
   import newly included skills.

Edit an imported skill only in `.agent-layer/skills-imported/<skill-name>/`. Do
not edit the generated copies under `.agents/skills/` or `.claude/skills/`. The
`al sync` command replaces them.

## Discarding local edits

Run `al skills reset <name>` only when the user explicitly asks to discard that
skill's local edits. It permanently replaces exactly one skill with the current
upstream version selected by its import configuration, even when the import is
pinned. It creates no backup. Use the exact skill name shown by
`al skills status --all`.

## Publishing changes

`al skills push` can publish changes from every writable import in one run. Run
it only when the user explicitly asks to publish local changes. Before running
it, confirm in `.agent-layer/config.toml` that every writable destination
matches the user's request. If one does not match, stop and ask. Do not change
write settings silently.

Push uses only configured destinations and branches. It never pulls first,
force-pushes, or opens a pull request. If it fails, report each failed skill and
the required action.

Use existing Git authentication. Repository URLs must not contain literal
credentials. Use `${AL_*}` placeholders whose values are stored in
`.agent-layer/.env`.
