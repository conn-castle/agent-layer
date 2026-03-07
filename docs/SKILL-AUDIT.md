# Skill Audit

This document describes how to audit skill documents for instruction-following quality. When asked to audit skills, treat this document as the workflow specification.

## Purpose

Evaluate the embedded template skill sources against the research-backed criteria in `docs/SKILL-DESIGN.md` and produce a ranked health report. This audit covers the shipped template sources under `internal/templates/skills/*/SKILL.md`, not repo-local active skills under `.agent-layer/skills/`.

## When to use

- After creating or significantly modifying skills
- As part of a periodic quality sweep
- Before a release that includes skill changes

## Audit criteria

For each skill, measure and evaluate:

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

## Audit workflow

### Phase 1: Gather all skills

1. List all template skill source files: `internal/templates/skills/*/SKILL.md`
2. Read each one.
3. Also read `docs/SKILL-DESIGN.md` as the criteria reference.

### Phase 2: Measure each skill

For each skill, record:
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

### Phase 3: Identify issues

For each skill, list specific issues with:
- **Severity**: High / Medium / Low
- **Location**: line numbers or section name
- **Issue**: what is wrong
- **Recommendation**: specific fix

Use these severity guidelines:
- **High**: Exceeds hard thresholds (>300 lines, >50 constraints), has multiple conditional branches, or violates single responsibility
- **Medium**: Near thresholds (250-300 lines, 40-50 constraints), has one conditional branch, vague checkpoints, or inline duplication of sub-skill logic
- **Low**: Verbose sections, minor duplication, inconsistencies with other skills

### Phase 4: Produce the health report

Output a ranked table sorted by instruction-followability (best to worst):

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

After the table, list:
1. **High-priority issues** across all skills, grouped by theme
2. **Cross-cutting observations** (patterns that affect multiple skills)
3. **Strengths** worth preserving across the skill set

### Phase 5: Log deferred work

For any High-severity issue that is not being fixed immediately:
- Add it to `docs/agent-layer/ISSUES.md` with the standard entry format
- Reference `docs/SKILL-DESIGN.md` in the Notes field

## Scoring thresholds reference

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
