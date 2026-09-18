---
title: Skills
description: Structured workflows that give agents repeatable, multi-phase processes for common development tasks.
keywords:
  - Agent Skills
  - portable AI agent skills
  - Claude Code skills
  - Codex skills
  - AI agent workflows
sidebar_position: 5
---

Agent skills are structured Markdown workflows that guide AI coding agents through complex, multi-step tasks. Instead of describing what you want step by step, you invoke a skill and the agent follows a tested process with built-in quality gates.

A skill such as `ship-pr` can audit changes, run CI, wait for review comments, address every one, and confirm CI passes before finishing. You get that whole lifecycle from one invocation instead of restating the steps each time.

Agent Layer also provides a Git-backed package workflow for skills: [import Agent Skills from any reachable repository](./skill-imports), customize them locally, merge upstream changes, and project the same content to every enabled client.

## The skill standard

Skills follow the [Agent Skills specification](https://agentskills.io/specification) — an open standard for portable, structured agent workflows. The standard is client-agnostic: skills work across Claude Code, Codex, and any system that supports skill-based workflows.

A skill is a Markdown file (`SKILL.md`) with YAML frontmatter that lives in a named directory:

```
skills/
  implement/
    SKILL.md
  ship-pr/
    SKILL.md
```

Each skill file has:

- **Frontmatter** with a `name` (must match the directory, lowercase alphanumeric and hyphens, 1–64 chars) and a `description` (what it does and when to trigger, max 1,024 chars)
- **A body** containing defaults, constraints, phased workflow steps, quality gates, and a defined handoff

The `description` is the routing signal. When you say "ship this as a pull request," the agent matches that against skill descriptions and activates the right one. Descriptions are written for routing precision, not marketing.

### Progressive disclosure

Skills use a three-stage model that keeps context costs low:

1. **Catalog time** — The agent sees only skill names and descriptions (~50–100 tokens each). This is the routing signal.
2. **Activation time** — When a skill triggers, the full `SKILL.md` body loads with the complete workflow, constraints, and quality gates.
3. **Execution time** — The agent follows the phased workflow, producing outputs, running checks, and stopping at human checkpoints when needed.

You do not pay the context cost of every skill on every request.

### Skill structure

The recommended section order is informed by research on how language models process instructions (primacy and recency effects). Critical rules go early; guardrails go at the end. For the full research basis, see [Skill Design Guide](/skill-design). For skills that route agents to installed command-line tools, use the [CLI Skill Design Guide](/cli-skill-design). For Agent Layer's root-skill model and current-to-target skill mapping, see [Skills approach](./skills-approach).

1. **Opening contract** — one sentence stating what the skill does
2. **Defaults** — what happens with no inputs
3. **Inputs** — what the skill accepts
4. **Required artifacts** — file paths for output
5. **Multi-agent pattern** — recommended sub-agent roles (optional)
6. **Global constraints** — hard rules that always apply
7. **Human checkpoints** — exact conditions for asking the user
8. **Workflow phases** — ordered execution steps
9. **Guardrails** — common failure modes to avoid
10. **Final handoff** — what to report when done

## What Agent Layer adds

The skill standard defines the format. Agent Layer builds on it with:

- **A library of 7 focused development skills** covering implementation, PR lifecycle, auditing, and autonomous delivery — available as the Agent Layer development skills catalog row in the wizard and refreshed through upgrade flows for repos that already have them.
- **Client projection** — User-managed `.agent-layer/skills/` and Git-managed `.agent-layer/skills-imported/` are the files you edit. `al sync` merges them into one immutable source snapshot, then writes that snapshot to `.agents/skills/` for shared-skill clients and `.claude/skills/` for Claude Code.
- **Memory integration** — Skills read from and write to project memory files (`ISSUES.md`, `BACKLOG.md`, `DECISIONS.md`, `COMMANDS.md`, `CONTEXT.md`), keeping project context durable across sessions.
- **Artifact conventions** — Skills keep agent-only plans, reports, and trackers under `.agent-layer/tmp/`, where another agent can pick them up without mixing transient state into product files.
- **Orchestrator composition** — Top-level skills delegate to supporting skills automatically, enabling complex multi-step workflows from a single invocation.

## Recommended workflow

The usual way to use Agent Layer skills follows two phases:

### 1. Plan with the agent

Spend time up front writing plans and requirements with the agent. Put that context in the project memory files (`CONTEXT.md`, `BACKLOG.md`). Clear plans make later autonomous execution more reliable.

### 2. Execute with orchestrator skills

Once planning artifacts exist, use the top-level orchestrator skills to drive development. These skills handle the full lifecycle internally, delegating to supporting skills as needed:

- **`implement`** — Give it a code change or specification and it selects a proportional direct or planned workflow, with independent review only when warranted.
- **`ship-pr`** — Audits uncommitted changes, commits, pushes, creates the PR, monitors CI (fixing failures), waits for review comments, addresses every one, and confirms CI passes.
- **`auto-skill-loop`** — Runs `fix-issue-log`, `implement-backlog`, `improve-interfaces`, `improve-codebase`, or a repository-added mode continuously, preserving blocked work and centrally shipping independent ready deliveries until substantive work is exhausted or the user stops it.

You rarely need to invoke the supporting skills directly. The orchestrators handle delegation automatically. But the supporting skills are available when you want fine-grained control over a specific step.

### Using skills

Skills activate implicitly or explicitly:

**Implicit** — Describe what you want and the agent matches the right skill:
- "Implement Phase 3" → `implement`
- "Ship this as a PR" → `ship-pr`
- "Check whether the docs still match the code" → `audit-documentation`

**Explicit** — Name the skill directly:
- "Use `implement` to complete Phase 3"
- "Run `interface-audit` on the auth module"

Skills accept flexible inputs: plain-language descriptions, file paths, constraints like "diagnosis only," or references to prior artifacts. Everything has sensible defaults, so you can invoke with just a request.

## Built-in skill library

Agent Layer development skills are 7 skills. They are organized here by how you typically use them. Select **Agent Layer development skills** in `al wizard` to install the set; unchecking that row removes it. Agent Layer also ships optional catalog skills for installed tools; those are listed separately below.

### Orchestrator skills

These are the top-level workflows you invoke directly. They delegate to supporting skills internally and handle complex multi-step processes end to end.

| Skill | What it does |
| --- | --- |
| `implement` | Implements a requested change directly or through a planned Agent Dispatch workflow, adding independent plan or code review only when warranted. |
| `auto-skill-loop` | Repeatedly selects, implements, ships, and—under standing authorization—merges one pull request at a time for a named mode. |
| `ship-pr` | Owns commits, pushes, PR creation, hosted continuous integration and review monitoring, repair dispatches, replies, and the merge-authorization gate. |

### Supporting skills

These are used by orchestrators or available for fine-grained control when you need a specific step. You rarely invoke them directly when following the orchestrator workflow.

**Auditing**

| Skill | What it does |
| --- | --- |
| `audit-documentation` | Audits Markdown documentation for static accuracy and cross-document consistency, fixing safe findings. |
| `audit-tests` | Audits the existing test suite for redundancy, misleading coverage, organization problems, and material behavioral gaps, fixing safe findings. |
| `audit-memory` | Audits agent memory files for structure, staleness, placement, consistency, and decision-log bloat, fixing accepted findings. |
| `interface-audit` | Audits product interfaces as component boundaries, scores complexity and debt, and produces interface cleanup recommendations without implementing changes. |

### Optional catalog skills

These skills are installed only when selected in the wizard catalog. Tool rows
route agents to optional integrations and usually need that command on `PATH`.
`dispatch-agent` uses the built-in Agent Dispatch MCP tools. `skill-sync` uses
the current `al` binary.

| Skill | What it does |
| --- | --- |
| `dispatch-agent` | Uses the built-in [Agent Dispatch](./agent-dispatch) MCP tools only when the user names an external dispatch target or another skill explicitly requires dispatch; it has no external binary requirement. |
| `find-docs` | Uses Context7 for current API and library documentation when local docs or CLI help are insufficient. |
| `playwright` | Uses `playwright-cli` for browser automation, screenshots, user interface inspection, and Playwright test work. |
| `skill-sync` | Imports and updates skills from Git. |
| `tavily-web` | Uses Tavily CLI for web search, URL extraction, site mapping, and cited research. |

## Writing your own skills

Create a new directory under `.agent-layer/skills/` with a `SKILL.md` file:

```bash
mkdir -p .agent-layer/skills/my-workflow
touch .agent-layer/skills/my-workflow/SKILL.md
```

Add frontmatter:

```yaml
---
name: my-workflow
description: >-
  One-sentence explanation of what this does and when it should trigger.
---
```

Follow the [skill structure](#skill-structure) outlined above. Keep the body focused: aim for 150–300 lines, put critical rules early, and make artifacts and stop conditions explicit.

Run `al sync` to project the new skill into all enabled clients.

The two client skill roots are Agent Layer-owned disposable output. Do not edit or install skills directly in `.agents/skills/` or `.claude/skills/`; sync replaces each enabled root wholesale and removes all extra content. `SKILL.md` bytes, unknown/provider-specific frontmatter, nested resources, and executable bits are preserved exactly. Lowercase `skill.md` is rejected. Every source-tree node must be a real directory or regular file; symlinks and all other node types are rejected, including symlinked skill directories and `SKILL.md` files.

For the complete research-backed authoring guide — including empirical studies on instruction-following, context length effects, and constraint composition — see [Skill Design Guide](/skill-design). For command-line tool workflows, use [CLI Skill Design Guide](/cli-skill-design) so live `--help` stays the place to look up syntax.

## Importing skills from Git

Agent Layer can import known Agent Skills from any Git repository your existing Git authentication can reach. Imported skills remain editable under `.agent-layer/skills-imported/`, project through ordinary `al sync`, and use recorded source state to preserve compatible local edits when you pull upstream changes.

```bash
al skills add https://github.com/example/skills.git skills/reviewer
al skills status
al skills pull
```

The dedicated [Import Agent Skills from Git](./skill-imports) guide covers reproducible project state, wildcard selection, local editing, three-way pulls, conflict resolution, and contributing improvements through explicit branches or forks. See the [CLI reference](./reference#skill-import-commands) for every command and the [configuration reference](./reference#skill-imports) for every `[[skills.imports]]` field.
## Skills approach

The built-in skills follow an Agent Layer-specific model: root skills with
stable inputs and outputs, composed by workflow skills that own loops and
orchestration. See [Skills approach](./skills-approach) for the design
principles, target root modules, and mapping to the current bundled skill
names.
