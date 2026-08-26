---
title: Import Agent Skills from Git
sidebar_label: Skill imports
description: Import portable Agent Skills from Git, edit them locally, merge upstream updates, and contribute improvements without losing project-specific changes.
keywords:
  - import Agent Skills
  - Git Agent Skills
  - portable AI agent skills
  - Claude Code skills
  - Codex skills
  - skills synchronization
sidebar_position: 6
---

Agent Layer turns any reachable Git repository into a managed source of portable [Agent Skills](https://agentskills.io/specification). Import one skill or a selected set, project them to every enabled coding agent, customize them in your project, pull compatible upstream changes, and optionally contribute your improvements back.

Unlike copying a `SKILL.md` into several client-specific folders, a skill import keeps source identity, resolved Git state, local edits, and publication state explicit. Your team can review the imported content and lock file in Git, while `al sync` produces each client's disposable native skill directory.

## Quick start

Import a known skill by repository and path:

```bash
al skills add https://github.com/example/agent-skills.git skills/code-review
al skills status
```

Agent Layer fetches and validates the skill, records its source state, writes it under `.agent-layer/skills-imported/code-review/`, and projects it through the normal sync pipeline. Invoke it from any enabled client just like a skill authored in `.agent-layer/skills/`.

To inspect and receive upstream changes later:

```bash
al skills diff code-review
al skills pull
```

## Why skill imports are different

- **One import, every enabled agent.** The same validated skill tree projects to the shared `.agents/skills/` directory and Claude Code's `.claude/skills/` directory rather than drifting across hand-maintained copies.
- **Local customization survives pulls.** Agent Layer performs a three-way reconciliation against the exact version you imported. Compatible local and upstream edits are both preserved.
- **Reproducible clones and CI.** Imported content and `.agent-layer/skills.lock.json` are project state. Commit them together so another checkout does not need network access or Git credentials before `al sync`.
- **Precise selection.** Import one path, several paths, or wildcard matches with explicit exclusions. Agent Layer never searches for or silently recommends unknown skills.
- **Safe pull progress.** During `al skills pull`, a source failure blocks that source; a validation or merge failure blocks only the affected skill. Other independent imports can still complete. `add` and `remove` instead preflight the whole requested change and leave local state untouched on failure.
- **Contribution without hidden automation.** Configure an explicit destination and branch, then push local skill changes without force-pushes, invented branches, or surprise pull requests.

## Import several skills

Selectors are paths inside the source repository. Wildcards can select a catalog, and a selector prefixed with `!` excludes matches within that same import block:

```bash
al skills add https://github.com/example/agent-skills.git \
  "skills/*" \
  "!skills/internal-only"
```

An exact selector that does not contain a valid skill is an error. Wildcards ignore ordinary non-skill directories, but a directory that claims to be a skill and fails validation is reported rather than silently skipped.

Pin a source when stability matters:

```bash
al skills add https://github.com/example/agent-skills.git \
  skills/code-review \
  --ref v1.4.0
```

Without `--ref`, the import tracks the repository's default branch. Use [`al skills status --all`](./reference#skill-import-commands) to see resolved source and local state.

## Edit imported skills safely

Edit `.agent-layer/skills-imported/<name>/` directly. Do not edit `.agents/skills/` or `.claude/skills/`; those are Agent Layer-owned projections and are replaced on sync.

`al skills pull` fetches the configured source and reconciles three trees:

```text
recorded source version + your local skill + current upstream skill
```

If the changes are compatible, Agent Layer keeps both. If the same content conflicts, it leaves the live skill untouched and creates a normal Git conflict workspace under `.agent-layer/tmp/skill-conflicts/<name>/`. Resolve it with Git, stage the intended tree, then apply it explicitly:

```bash
git -C .agent-layer/tmp/skill-conflicts/code-review status
git -C .agent-layer/tmp/skill-conflicts/code-review add .
al skills resolve code-review
```

To permanently discard one imported skill's local edits, use `al skills reset <name>`. Reset prompts by default and creates no backup, commit, or stash.

## Contribute improvements upstream

Imports are read-only by default. To publish local changes, configure an explicit write policy when adding the skill. A contribution branch is the safest common workflow:

```bash
al skills add https://github.com/example/agent-skills.git \
  skills/code-review \
  --write branch \
  --push-branch improve-code-review

al skills push
```

Use `--push-repository` when the contribution should go to a fork. Agent Layer reconciles destination-side changes before pushing, records a separate publication checkpoint, and can add later commits to the same branch. It never force-pushes, invents a branch, opens a pull request, or pulls source updates as a side effect of push.

## Remove an import

Remove the exact repository and selector you no longer want:

```bash
al skills remove https://github.com/example/agent-skills.git skills/code-review
```

A clean imported copy is deleted. A locally edited copy is preserved and reported so you can move it into `.agent-layer/skills/` and take ownership instead of losing work.

## Trust and security boundaries

Imported skills are executable instructions for agents and may include scripts or other resources. Review the source and diff before enabling unfamiliar content. Agent Layer uses your existing Git authentication, refuses plain HTTP repositories, rejects symlinks and unsupported filesystem nodes, validates every skill tree, and never places credentials in its lock file.

For every command and flag, see [Skill import commands](./reference#skill-import-commands). For configuration fields such as tracking, refs, write policies, and push destinations, see [Skill imports](./reference#skill-imports). For authoring and projection concepts, see [Skills](./skills).
