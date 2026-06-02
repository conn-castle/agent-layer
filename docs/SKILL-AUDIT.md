# Skill Audit

This document describes how to audit skill documents for instruction-following quality. When asked to audit skills, treat this document as the workflow specification.

## Purpose

Evaluate the embedded template skill sources and produce a ranked health report. This audit covers two shipped template trees, not repo-local active skills under `.agent-layer/skills/`:

- **General (workflow) skills** under `internal/templates/skills/*/SKILL.md` — scored against the research-backed criteria in `docs/SKILL-DESIGN.md` (the "Audit criteria" section below).
- **CLI skills** under `internal/templates/skills-catalog/*/SKILL.md` — skills that teach agents to drive an installed command line interface. They are scored against the CLI-specific rules in `docs/CLI-SKILL-DESIGN.md` (the "CLI skill criteria" section below). The line-count and constraint thresholds in this document apply to general skills only; CLI skills are deliberately lean routers and are evaluated on routing, live-help usage, and safety instead.

## When to use

- After creating or significantly modifying skills
- As part of a periodic quality sweep
- Before a release that includes skill changes

## Audit criteria

For each general (workflow) skill, measure and evaluate:

1. **Line count** — Target: 150-300 lines. Flag any skill over 250 as worth reviewing. Over 300 is a hard concern.
2. **Constraint count** — Estimate discrete instructions/constraints. Target: under 50. Over 50 is a concern.
3. **Conditional branching** — Count "if mode X / if mode Y" patterns that switch behavior. Each conditional adds constraint interference risk (ComplexBench 2024). Zero is ideal.
4. **Single responsibility** — Does the skill do ONE thing? Or does it serve multiple purposes via mode switching?
5. **Primacy effect** — Are the most critical constraints in the first 20% of the document? LLMs show universal primacy bias — later instructions are more likely to be dropped.
6. **Sub-skill delegation** — Does the skill delegate to existing sub-skills where appropriate, or re-implement their logic inline?
7. **Human checkpoints** — Are they specific ("ask when X happens") or vague ("ask when uncertain")? Vague checkpoints cause either always-ask or never-ask behavior.
8. **Guardrails** — Are they specific to this skill's failure modes, or generic boilerplate?
9. **Verbosity** — Are there sections that could be shorter without losing clarity? Report templates, examples, and philosophy sections are common sources of bloat.
10. **Description quality** — Does the frontmatter description clearly distinguish this skill from related skills?

## CLI skill criteria

CLI skills are routers to an installed command line interface, not reference manuals, so they are evaluated against the **Review Checklist** in `docs/CLI-SKILL-DESIGN.md`, which is the authoritative rubric — consult it for the complete pass/fail list and the Anti-Patterns table. For each CLI skill, evaluate:

1. **Description routing** — Does the frontmatter description name the CLI, its task domain, trigger conditions in user language, and at least one nearby non-goal? A weak description is an activation bug, not a polish issue.
2. **Lean body** — Is the body a workflow and safety contract rather than a copied manual? Flag copied flag lists, full subcommand references, and version-sensitive examples.
3. **Live help** — Does the body tell the agent to run live `<cli> --help` (and subcommand help) and treat installed help as the source of truth for syntax?
4. **Explicit setup failure** — If the CLI is missing, unauthenticated, or outside the expected environment, does the skill stop and report rather than silently install, authenticate, or fall back to another tool or MCP server?
5. **Read-only before writes and human checkpoints** — Are destructive, deploy, publish, payment, or external-write operations gated behind dry-run/preview or a human checkpoint?
6. **Secret hygiene** — Are secrets kept out of command arguments, examples, and artifacts?
7. **Untrusted output** — Does the skill treat CLI output as untrusted data rather than instructions?
8. **No duplicated references** — Is command reference material absent from system instructions, `SKILL.md`, and `references/` (live help is the single source of truth)?

## Audit workflow

### Phase 1: Gather all skills

1. List all template skill source files from both trees:
   - General (workflow) skills: `internal/templates/skills/*/SKILL.md`
   - CLI skills: `internal/templates/skills-catalog/*/SKILL.md`
2. Read each one. Note which tree each skill belongs to — it determines which criteria apply.
3. Read `docs/SKILL-DESIGN.md` as the criteria reference for general skills, and `docs/CLI-SKILL-DESIGN.md` for CLI skills.

### Phase 2: Measure each skill

For **general (workflow) skills**, record:
- Line count (exact)
- Estimated constraint count
- Number of conditional branches (with descriptions)
- Single responsibility assessment (Strong / Weak / Violated)
- Primacy assessment (Strong / Adequate / Weak)
- Delegation assessment (Good / Adequate / Inline duplication)
- Checkpoint specificity (Specific / Mostly specific / Vague)
- Guardrail specificity (Specific / Generic)
- Verbosity assessment (Lean / Adequate / Verbose)
- Description quality (Good / Adequate / Poor)

For **CLI skills**, record (line/constraint thresholds do not apply):
- Line count (exact) — CLI skills should be lean; flag bodies that copy CLI help into the skill
- Description routing (Good / Adequate / Poor) — names CLI, domain, triggers, non-goal
- Live-help instruction (Present / Missing)
- Setup-failure handling (Present / Missing / Silent fallback)
- Safety gates for side effects (Present / Missing / N/A)
- Secret hygiene and untrusted-output handling (OK / Issue)
- Duplicated command reference (None / Found)

### Phase 3: Identify issues

For each skill, list specific issues with:
- **Severity**: High / Medium / Low
- **Location**: line numbers or section name
- **Issue**: what is wrong
- **Recommendation**: specific fix

Use these severity guidelines for general skills:
- **High**: Exceeds hard thresholds (>300 lines, >50 constraints), has multiple conditional branches, or violates single responsibility
- **Medium**: Near thresholds (250-300 lines, 40-50 constraints), has one conditional branch, vague checkpoints, or inline duplication of sub-skill logic
- **Low**: Verbose sections, minor duplication, inconsistencies with other skills

For CLI skills:
- **High**: Weak or ambiguous description (activation bug), copied CLI manual or full flag lists in the body, silent fallback or silent install/auth on missing setup, or secrets passed on the command line
- **Medium**: Missing live-help instruction, missing setup-failure handling, or missing safety gates for side-effecting operations
- **Low**: Minor verbosity, weak help-probe labeling, or small description overlap with a neighboring skill

### Phase 4: Produce the health report

Output a ranked table of **general (workflow) skills** sorted by instruction-followability (best to worst):

```
| Rank | Skill | Lines | Constraints | Cond. Branches | Verdict |
|------|-------|-------|-------------|----------------|---------|
```

Verdict categories:
- **Excellent**: Under 150 lines, under 35 constraints, no conditional branches, strong single responsibility
- **Good**: Under 250 lines, under 45 constraints, at most 1 conditional branch
- **Needs work**: 250-300 lines, or 45-50 constraints, or 2+ conditional branches
- **Needs split**: Over 300 lines, or over 50 constraints, or single responsibility violated
- **Needs trim**: Under the constraint threshold but over the line threshold due to verbosity

Then output a companion table of **CLI skills** (the line/constraint thresholds above do not apply):

```
| Rank | CLI Skill | Lines | Description | Live Help | Setup Failure | Verdict |
|------|-----------|-------|-------------|-----------|---------------|---------|
```

CLI verdict categories:
- **Excellent**: Lean body, strong routing description, live-help instruction present, explicit setup-failure handling, no duplicated reference material
- **Good**: Minor gaps (slightly verbose body or a help probe that reads like syntax) but routing, live help, and setup failure are all sound
- **Needs work**: Missing live-help instruction, missing setup-failure handling, or a weak description
- **Needs rewrite**: Copied manual, silent fallback or install, secrets in commands, or a description that fails activation tests

After the tables, list:
1. **High-priority issues** across all skills, grouped by theme
2. **Cross-cutting observations** (patterns that affect multiple skills)
3. **Strengths** worth preserving across the skill set

### Phase 5: Log deferred work

For any High-severity issue that is not being fixed immediately:
- Add it to `docs/agent-layer/ISSUES.md` with the standard entry format
- Reference `docs/SKILL-DESIGN.md` (general skills) or `docs/CLI-SKILL-DESIGN.md` (CLI skills) in the Notes field

## Scoring thresholds reference

These thresholds apply to general (workflow) skills. CLI skills are evaluated against the Review Checklist in `docs/CLI-SKILL-DESIGN.md`, not these thresholds.

| Metric | Green | Yellow | Red |
|--------|-------|--------|-----|
| Lines | < 150 | 150-300 | > 300 |
| Constraints | < 35 | 35-50 | > 50 |
| Conditional branches | 0 | 1 | 2+ |
| Single responsibility | Strong | Adequate | Violated |

These thresholds are derived from the research in `docs/SKILL-DESIGN.md`:
- Line/token thresholds from "Same Task, More Tokens" (ACL 2024) and "Context Length Alone Hurts" (2025)
- Constraint thresholds from IFScale (2025)
- Conditional branching risk from ComplexBench (2024)
