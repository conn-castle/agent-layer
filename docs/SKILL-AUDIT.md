# Skill Audit

Use this procedure to audit shipped skill templates for instruction-following
quality. Use reviewer judgment and cite the affected text; do not require
runtime traces, research, or empirical proof. Do not edit skills unless the
caller also requests fixes.

## Scope

Audit both shipped source trees:

- General skills: `internal/templates/skills/*/SKILL.md`
- CLI skills: `internal/templates/skills-catalog/*/SKILL.md`

Do not substitute generated or repo-local copies. Read referenced resources
only to verify an interface or ownership claim.

## Sources of truth

- `docs/SKILL-DESIGN.md`: portable general-skill rubric
- `docs/CLI-SKILL-DESIGN.md`: CLI-skill rubric
- `site/docs/skills-approach.mdx`: Agent Layer architecture and instruction
  ownership
- Audited child skill: internal procedure
- Audited parent skill: child inputs, accepted result, and response to failure
  or required user input

Do not copy the design-guide rubrics into the audit report. Cite the applicable
rule and explain the skill-specific problem.

## Audit rules

- Treat semantically equivalent wording as duplication. Before recommending
  deletion, name the retained owner; without one, deletion is behavior loss.
- Preserve parent/child boundary contracts without repeating child steps,
  rationale, or internal safeguards.
- When execution can pause, require an exact step or phase to resume.
- Do not report wording preferences that do not affect routing, behavior,
  safety, verification, or maintenance.

## General-skill checklist

- **Routing and responsibility**: The description distinguishes adjacent skills;
  the body has one job and one primary output.
- **Inputs and defaults**: Required inputs are explicit and fail before side
  effects; missing paths, roles, or defaults are not invented.
- **Instruction ownership**: Each rule, failure response, output field, and
  approval requirement has one owner.
- **Composition**: Root skills do not call other skills. Workflows state child
  boundaries without copying child procedures.
- **Orchestrator context**: A workflow preserves enough context to validate
  inputs, reconcile child results, apply gates, and produce its final handoff.
- **Child calls**: Each call shows exact arguments, artifact sources, accepted
  results, and handling for failure, missing output, or required user input.
- **User interaction**: Each required question or approval names its trigger,
  requested response, and resume location; the normal path continues without
  asking.
- **Structure**: Critical rules appear early. Steps or phases are addressable
  wherever execution can pause and resume.
- **Guardrails and economy**: Guardrails target this skill's failure modes.
  Remove text that does not change behavior.
- **Completion and handoff**: Completion criteria are observable outcomes, not
  repeated steps; the final handoff contains only fields the caller needs.

## Audit workflow

### Step 1: Inventory and classify

1. List every `SKILL.md` in both source trees.
2. Classify each general skill as a root skill or workflow skill using
   `skills-approach.mdx`.
3. Record exact line count and estimate discrete instructions.
4. Record mode switches separately from ordinary workflow conditions. An
   `if mode X` branch changes the job; `if verification fails` protects one job.

Use `SKILL-DESIGN.md` heuristics to select long or dense skills for closer
review. Do not reward shortness or recommend filler.

### Step 2: Apply the authoritative rubric

Apply the checklist above and the source assigned in `Sources of truth`. Apply
the `CLI-SKILL-DESIGN.md` review checklist to CLI skills. Inspect repository
paths, resources, current skill names, and child contracts when they resolve a
factual question; absence of external proof does not block a finding.

### Step 3: Build an instruction-ownership map

For every safety-critical or workflow-controlling instruction, record:

| Instruction | Authoritative owner | Other occurrences | Verdict |
| --- | --- | --- | --- |

Use these verdicts:

- `single-owner`: stated once in the correct place
- `boundary-reference`: repeated only as a necessary caller/callee contract
- `duplicate`: another occurrence adds no behavior
- `conflict`: occurrences require different behavior
- `missing-owner`: required behavior is implied but never stated

Check within each skill and across parent/child skills, especially Rules,
workflow, Guardrails, Definition of done, and Final handoff.

### Step 4: Validate composition and interfaces

Verify that root skills do not call other skills. For each workflow call, verify
the current child name, required inputs, accepted result, caller boundary, and
failure or user-response handling. Confirm that every referenced role, asset,
script, and path exists.

### Step 5: Rank findings by impact

Use these severities:

- **Critical**: permits unauthorized, destructive, security-sensitive, or wrong-
  target action.
- **High**: breaks activation, the primary output contract, a source-of-truth
  boundary, or required user control.
- **Medium**: duplicates child logic, conflicts across sections, hides a critical
  instruction, or leaves failure/resume behavior ambiguous.
- **Low**: localized verbosity or imprecision with a concrete maintenance or
  instruction-following cost.

For each finding, report the skill and section, violated rule, affected text,
reasoning, smallest correction, and resulting instruction owner.

### Step 6: Produce the report

Start with one inventory, not a subjective ranking:

```text
| Skill | Kind | Lines | Instructions (est.) | Mode switches | Findings |
|-------|------|-------|---------------------|---------------|----------|
```

Then report:

1. Critical and High findings
2. Medium and Low findings
3. The instruction-ownership map from Step 3
4. Strengths worth preserving

Use `pass` only when the applicable rubric has no unresolved finding. Never
derive status from a count.

### Step 7: Preserve deferred work

Add unfixed Critical or High findings to `docs/agent-layer/ISSUES.md` using its
current format. Cite the owning design rule and the audited skill path. Do not
create an issue for a metric alone.
