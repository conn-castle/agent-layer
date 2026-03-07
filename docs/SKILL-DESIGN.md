# Skill Design Guide

This document is the canonical skill-authoring guide for repo-local skill
sources such as `.agent-layer/skills/<name>/SKILL.md`.

Research and standards in this guide were re-verified on 2026-03-06. The guide
intentionally separates:
- specification or product requirements
- evidence-backed authoring guidance
- repo heuristics used to keep skills maintainable

## Evidence model

- Standards and platform behavior: Agent Skills specification, OpenAI Codex
  skills docs, and agent-layer's own validator/README.
- Official prompting guidance: Anthropic and OpenAI agent/prompting docs.
- Empirical studies: long-context, instruction-following, constraint-
  composition, and context-rot papers.
- Software design guidance: maintainability rules for when to split or abstract
  skills.

When this guide gives a numeric limit, it is either a published limit or
explicitly labeled as a local heuristic.

---

## Specification and product requirements

### Canonical source format

- Author skills in directory format: `.agent-layer/skills/<name>/SKILL.md`
  [ref 1].
- Required frontmatter fields: `name`, `description` [ref 1].
- Optional frontmatter fields: `license`, `compatibility`, `metadata`,
  `allowed-tools` [ref 1].
- `name` must match the directory name and pass agent-layer validation.
- `description` must explain both what the skill does and when it should
  trigger (max 1,024 characters [ref 1]).
- `name` must be 1-64 characters, lowercase alphanumeric and hyphens only, no
  leading/trailing/consecutive hyphens [ref 1].

### Progressive disclosure budget

- The Agent Skills specification [ref 1] recommends keeping the main `SKILL.md`
  under 500 lines and under roughly 5,000 tokens, with deeper material moved
  into `scripts/`, `references/`, and `assets`.
- At the catalog level, each skill adds roughly 50-100 tokens (name +
  description only) [ref 1]. Full instructions load only on activation.
- `al doctor` warns when a skill source exceeds 500 lines.
- Treat 500 lines as a warning threshold, not a design target.

### Experimental surfaces

- `allowed-tools` is optional and experimental; support varies across clients.
- `compatibility` should only be used for real environment requirements such as
  tools, network access, or intended runtime.

### Current agent-layer implementation gap

- The Agent Skills spec supports `scripts/`, `references/`, and `assets`, but
  agent-layer's internal MCP prompt server currently serves only the `SKILL.md`
  body.
- Implication: every skill in this repo must remain operationally understandable
  from `SKILL.md` alone.
- Companion files can still help direct file-system workflows and future
  clients, but core behavior cannot depend on them today.

---

## Evidence-backed design principles

### 1. Optimize for activation accuracy first

A skill that does not trigger reliably is already broken, even if its body is
excellent. The Agent Skills spec and Codex skills docs both treat `name` and
`description` as the main routing metadata, and OpenAI explicitly recommends
testing prompts against the description to confirm the right trigger behavior
[ref 3, 7].

Anthropic's routing pattern recommends classifying inputs and directing them to
specialized downstream handlers, with each handler having clearly distinct
trigger conditions [ref 8]. This maps directly to skill activation: the
`description` is the routing classifier's input.

**Evidence:**
- OpenAI's eval-skills guide [ref 7] recommends building evaluation sets that
  include both `should_trigger = true` and `should_trigger = false` cases. A
  good skill description should make the boundary between these two sets as
  unambiguous as possible.
- Anthropic's routing pattern [ref 8] works best when categories are "distinct
  and well-defined" — the same principle applies to skill descriptions that
  share semantic territory with adjacent skills.

Authoring guidance:
- Write the `description` for routing, not marketing. State the job, likely
  trigger phrases, and nearby non-goals.
- Add explicit `Use this when` and `Do not use this when` guidance when
  adjacent skills are easy to confuse.
- Evaluate both positive and negative prompts. A good prompt set should include
  `should_trigger = false` cases, not just happy paths.
- Test activation against the descriptions of all sibling skills to ensure the
  routing signal is unambiguous.

### 2. Keep each skill focused on one workflow

Anthropic recommends simple, composable patterns and routing to specialized
follow-up tasks [ref 8]. OpenAI's Codex skills docs make the same
recommendation more directly: keep each skill focused on one job [ref 3]. Both
sources converge on a fundamental design insight: specialized, focused
components are easier to route to, reason about, and maintain.

**Evidence — constraint composition degrades performance:**
- ComplexBench (NeurIPS 2024) [ref 13] proposes a taxonomy of 4 constraint
  types, 19 constraint dimensions, and 4 composition types (And, Chain,
  Selection, combined). Their results show that flat AND composition scores
  0.881 for GPT-4, but Chain drops to 0.787, Selection to 0.815, and deeply
  nested compositions collapse catastrophically — combined compositions with
  multiple Selection layers score as low as 0.083. Even GPT-4 fails on 20% of
  complex instructions. This is exactly the kind of branching logic that
  multi-mode skills create.
- IFScale [ref 12] finds three distinct degradation patterns as instruction
  density increases: threshold decay (stable until a critical density, then
  steep decline), linear decay (steady consistent decline), and exponential
  decay (rapid early degradation). Even the best frontier models achieve only
  68% accuracy at 500 instructions — and importantly, reasoning models that
  maintain high accuracy at moderate densities still eventually hit performance
  cliffs.
- Anthropic's agent guide [ref 8] explicitly recommends the routing pattern
  over mode-switching: classify the input, then dispatch to a specialized
  handler. This avoids the constraint-composition penalty entirely.

Authoring guidance:
- Split skills when they have materially different triggers, outputs, or
  decision rules.
- Avoid mode-switching sections like `if mode X` / `if mode Y` unless the
  branches are tiny and inseparable.
- Prefer separate skills for separate targets, such as `review-plan` versus
  `review-scope`.
- Local heuristic: if a skill needs the same branching explanation more than
  once, it is a strong candidate for splitting.
- When two skills share structural similarity (e.g., both use an audit loop),
  accept the duplication until there are at least three real consumers of the
  shared structure (Rule of Three [ref 14, 15]).

### 3. Keep the core context lean and high-signal

Context is a finite resource with diminishing marginal returns. Anthropic's
context engineering guide [ref 9] defines context engineering as "curating and
maintaining the optimal set of tokens during LLM inference" and recommends
finding "the smallest set of high-signal tokens that maximize the likelihood
of some desired outcome." Models have an "attention budget" depleted by each
new token — performance shows a gradient rather than a hard cliff.

**Evidence — input length degrades reasoning:**
- "Same Task, More Tokens" (ACL 2024) [ref 10] demonstrates that LLM reasoning
  performance degrades "at much shorter input lengths than their technical
  maximum." Aggregate accuracy dropped from 0.92 to 0.68 (a 24-point decline)
  as input extended from ~250 to ~3,000 tokens, with significant degradation
  beginning beyond ~500 tokens. Crucially, even when padding consisted of exact
  duplicates of the relevant text (no distraction, no search needed), accuracy
  still decreased — proving that length itself, independent of content quality,
  degrades reasoning. The paper further shows that perplexity correlates
  negatively with reasoning (Pearson rho = -0.95, p = 0.01), meaning standard
  benchmarks mask this degradation.
- "Context Rot" (Chroma Research 2025) [ref 16] tests 18 models and finds that
  "models do not use their context uniformly; performance grows increasingly
  unreliable as input length grows." Even simple tasks show degradation, and
  the researchers note that "synthesis or multi-step reasoning" tasks would
  show "even more severe" degradation. Crucially, they find that even a single
  distractor reduces performance relative to baseline.
- Anthropic's context engineering guide [ref 9] notes the architectural root
  cause: context creates "n-squared pairwise relationships for n tokens" and
  models have "less experience with, and fewer specialized parameters for,
  context-wide dependencies."

**Evidence — instruction count matters independently:**
- IFScale [ref 12] shows that even the best reasoning models only achieve 68%
  accuracy at 500 instructions. The study identifies that at moderate densities
  (150-200 instructions), models exhibit the strongest primacy bias — they
  reliably follow early instructions but increasingly violate later ones. At
  extreme densities, error type shifts from modification (attempting but
  failing) to omission (completely ignoring), suggesting the model's attention
  simply cannot cover all instructions.

Authoring guidance:
- Keep only activation-critical instructions, defaults, invariants, and
  workflow skeleton in `SKILL.md`.
- Remove repeated rationale, decorative prose, and edge cases that do not
  change behavior.
- Prefer short, atomic bullets over dense paragraphs.
- Every token in `SKILL.md` should change the model's behavior. If removing a
  sentence would not change what the model does, the sentence should not be
  there.
- Repo heuristic: most healthy skills should stay roughly in the 150-300 line
  range; cross 300 only with a clear reason. The validator warning at 500 lines
  is the hard backstop.

### 4. Put critical instructions early

Instructions appearing earlier in the prompt are more reliably followed.
Multiple independent studies confirm this primacy effect, which has direct
implications for how skills should be structured.

**Evidence — primacy effects are universal:**
- IFScale [ref 12] computes primacy effects as the ratio of error rates in the
  final third of instructions to error rates in the first third. The finding is
  universal: all 20 models tested show higher error rates for later
  instructions. The primacy effect peaks at moderate densities (150-200
  instructions), where models begin to struggle under cognitive load. At high
  densities, error type shifts from modification (attempting but failing) to
  omission (completely ignoring): weaker models show omission-to-modification
  ratios of 26-35:1, while reasoning models maintain lower ratios (~6:1) by
  attempting modifications rather than pure omission.
- "Lost in the Middle" (TACL 2023) [ref 11] finds a U-shaped performance curve
  in both multi-document QA and key-value retrieval tasks: models perform best
  when relevant information appears at the very beginning (primacy bias) or end
  (recency bias) of the context, with "significant degradation when models must
  access relevant information in the middle of the input context." In 20-
  document settings, GPT-3.5-Turbo's accuracy dropped to ~52.9% for middle-
  positioned information — worse than the 56.1% closed-book baseline, meaning
  providing the information in the middle actually hurt performance compared to
  not providing it at all. Beginning/end positions maintained ~80%+ accuracy.
- A separate position-bias study (Zeng et al. 2025) [ref 17] confirms that
  constraint ordering matters: "LLMs often perform better when the constraints
  are presented in a hard-to-easy order" — further evidence that position is
  not neutral and must be designed deliberately.
- "Context Rot" [ref 16] finds that for the repeated-words task, position
  accuracy is "highest when unique word appears near beginning" — consistent
  with the primacy finding.

Authoring guidance:
- Put mission, defaults, required artifacts, hard constraints, and human
  checkpoint rules near the top.
- Introduce safety-critical limits once, early, in the clearest language
  available.
- Use later sections to elaborate or operationalize earlier rules, not to
  smuggle in new critical constraints.
- If a rule matters enough to block execution, it should not first appear in
  Phase 6.
- Consider the recency effect too: guardrails and final-handoff instructions at
  the very end of the skill benefit from the tail of the U-shaped curve.

### 5. Prefer explicit contracts over vague prose

Anthropic recommends simple, direct language and clearly separated prompt
sections [ref 8, 9]. OpenAI's prompt engineering and reasoning guidance
likewise favors precise instructions, delimiters, and explicit success criteria
[ref 4, 5]. Skills work best when the model can see the contract, not infer it.

**Evidence — ambiguity increases constraint violations:**
- ComplexBench [ref 13] finds that constraint violations increase when
  constraints are implicit or require inference across multiple instructions.
  Explicit, independently verifiable constraints are followed most reliably.
- Anthropic's context engineering guide [ref 9] recommends using "XML tags or
  Markdown headers" to delineate sections, with "simple, direct language that
  presents ideas at the right altitude for the agent." They warn against both
  extremes: don't hardcode brittle if-else logic, nor provide vague high-level
  guidance.
- OpenAI's prompt engineering guide [ref 4] recommends using delimiters to
  separate distinct parts of the input and specifying output format explicitly.
- OpenAI's reasoning best practices [ref 5] recommend keeping prompts clear and
  concise for reasoning models, and note that overly detailed instructions can
  sometimes constrain the model's reasoning process rather than help it.

Authoring guidance:
- Name required artifacts, filenames, report headings, schemas, and stop
  conditions explicitly.
- Distinguish required behavior, defaults, and optional behavior with separate
  sections.
- Prefer `If X is missing, stop and report Y` over `Handle missing inputs
  appropriately`.
- Use stable section names across skills so authors and reviewers know where to
  look.
- Use markdown headers and consistent formatting to create unambiguous section
  boundaries.

### 6. Use progressive disclosure and shallow delegation

The Agent Skills spec is built around progressive disclosure: metadata first,
`SKILL.md` on activation, supporting resources only when needed. Anthropic's
context engineering guidance recommends the same pattern: assemble understanding
layer by layer and load more context only when required [ref 9].

**Evidence — layered context enables focused reasoning:**
- Anthropic's context engineering guide [ref 9] describes progressive
  disclosure as a strategy where "agents incrementally discover relevant
  context through exploration." The guide notes that this keeps agents "focused
  on relevant subsets rather than drowning in exhaustive but potentially
  irrelevant information."
- The Claude Code agent [ref 9] implements this pattern directly: CLAUDE.md
  files are loaded upfront for always-needed context, while glob/grep provide
  just-in-time retrieval. The agent "maintains lightweight identifiers (file
  paths, queries, links) instead of loading full data objects."
- Anthropic's sub-agent architecture [ref 9] demonstrates the same principle at
  the agent level: each sub-agent "might explore extensively, using tens of
  thousands of tokens or more" but returns "only a condensed, distilled summary
  of its work (often 1,000-2,000 tokens)." This achieves "clear separation of
  concerns" with detailed search isolated within sub-agents.

Authoring guidance:
- Keep top-level skills as control planes, not encyclopedias.
- Delegate repeated mechanical work to scripts and repeated judgment workflows
  to named sub-skills.
- Keep delegation shallow. A top-level skill should usually call leaf skills,
  not build a three-layer wrapper stack.
- Prefer one-level-deep file references from `SKILL.md`. Avoid chains of
  `REFERENCE.md` files that point to more reference files.
- Design skills so the model can start executing from `SKILL.md` alone, then
  load additional context only when a specific phase requires it.

### 7. Use scripts only when deterministic execution beats prose

OpenAI's Codex skills docs recommend preferring instructions over scripts unless
you need deterministic behavior or external tooling [ref 3]. The Agent Skills
scripting guidance adds the operational details: scripts should avoid
interactive prompts, support `--help`, produce helpful errors, and return
structured output when possible [ref 2].

**Evidence — prose instructions are more flexible:**
- Anthropic's agent design guide [ref 8] recommends that agents "maintain
  flexibility in how tasks are accomplished" — hardcoded scripts remove the
  model's ability to adapt to unexpected conditions.
- OpenAI's Codex prompting guide [ref 6] notes that instructions allow the
  model to exercise judgment, while scripts are better for tasks that must
  produce identical results regardless of context.
- The practical dividing line: if the output must be byte-identical across
  runs (formatting, validation, scaffolding), use a script. If the output
  requires judgment about the specific codebase or context, use instructions.

Authoring guidance:
- Use scripts for parsing, validation, formatting, scaffolding, or other
  mechanical tasks that are easier to run than describe.
- Do not hide core judgment or routing logic inside scripts.
- Never require interactive input from a script in a normal skill workflow.
- If a script is required, say exactly when to run it, what inputs it needs,
  and what outputs or side effects count as success.

### 8. Design human checkpoints narrowly and explicitly

Anthropic's agent guidance describes humans as checkpoints for blockers and
judgment calls, not as generic fallbacks [ref 8]. The building-effective-agents
guide specifically recommends that agents "return to humans for information or
judgment when needed" and "pause at checkpoints or when encountering blockers."
OpenAI's skills guidance similarly recommends explicit approval for sensitive
actions [ref 3].

**Evidence — vague escalation creates worse outcomes:**
- Anthropic's building-effective-agents guide [ref 8] frames agents as most
  effective in "trusted environments with appropriate guardrails." The human
  role is specific: provide information the agent lacks, or make judgment calls
  the agent should not make autonomously.
- Research on instruction density [ref 12] shows that the more constraints a
  model must satisfy simultaneously, the more likely it is to violate some of
  them. Vague checkpoint rules like "ask when uncertain" add another
  constraint the model must interpret on every action, competing with the
  actual workflow instructions for attention.
- Anthropic's context engineering guide [ref 9] recommends that tool design
  follow "poka-yoke" principles — making mistakes harder to make. The same
  principle applies to checkpoints: make the trigger condition so specific
  that the model cannot mistake when to ask.

Authoring guidance:
- Name the exact ambiguity trigger that requires human input.
- Keep the normal path autonomous.
- Prefer concrete checkpoint rules such as `ask before creating a missing
  memory file` or `ask before applying destructive deletes`.
- Avoid generic instructions like `ask when uncertain`.
- Limit the number of distinct checkpoint conditions. Each one is an
  instruction the model must carry, competing for attention with the workflow.

### 9. Make the workflow measurable from day one

OpenAI's prompting guide recommends evals for prompt changes [ref 4], and their
skill-evals guide [ref 7] is even more direct: define success before writing
the skill, capture traces, and grade both outcomes and process. A skill without
measurable success criteria is difficult to improve and easy to regress.

**Evidence — evaluation drives reliability:**
- OpenAI's eval-skills guide [ref 7] recommends building evaluation suites
  that test both positive triggers (skill should activate) and negative
  triggers (skill should not activate). This catches routing regressions that
  pure functional tests miss.
- IFScale [ref 12] demonstrates the value of automated grading: by using
  simple keyword-inclusion checks, the benchmark scales to 500 instructions
  per task. Skills benefit from similarly automatable success criteria.
- Anthropic's building-effective-agents guide [ref 8] notes that coding agents
  are "particularly effective because solutions are verifiable via automated
  tests; agents iterate using test feedback." Skills that produce verifiable
  artifacts inherit this advantage.

Authoring guidance:
- Give every skill a checkable definition of done.
- Separate outcome goals, process goals, and style goals.
- Prefer deterministic checks first; use rubric-based grading only where
  deterministic checks cannot express the requirement.
- Make artifact paths and output formats explicit so eval harnesses can inspect
  them without guesswork.
- Design at least one positive-trigger and one negative-trigger prompt for
  activation evaluation. OpenAI's eval-skills guide [ref 7] recommends 10-20
  prompts per skill covering four types: explicit invocation (names the skill),
  implicit invocation (describes the scenario), contextual (adds domain noise),
  and negative control (should not trigger).

### 10. Treat skills as privileged instructions and code

OpenAI's skills docs explicitly warn that skills can influence planning, tool
usage, and command execution, and should be treated as privileged
instructions/code [ref 3]. This matters even for repo-local skills: the skill
author is defining what the agent is allowed to believe is the intended
workflow.

**Evidence — context is trusted by default:**
- Anthropic's context engineering guide [ref 9] notes that agents treat their
  context as authoritative. A skill that claims certain permissions or
  behaviors will be followed as written — the model does not independently
  verify whether the skill's claims are legitimate.
- OpenAI's Codex skills docs [ref 3] warn that skills "can influence planning,
  tool usage, and command execution" and recommend treating them with the same
  care as production code.
- This is especially important for skills that delegate to sub-agents or
  scripts: the delegated work inherits the permissions and assumptions of the
  parent skill.

Authoring guidance:
- Never assume network access, elevated permissions, or non-standard tools
  without saying so.
- Use `compatibility` for genuine environment constraints, not discoverability
  or marketing text.
- Require explicit approval for sensitive or high-impact actions.
- Keep least-privilege assumptions intact; a skill should guide execution
  within policy, not try to broaden policy.
- Review skills with the same rigor as code reviews — they have equivalent
  impact on agent behavior.

### 11. Manage context budget across the skill lifecycle

Skills do not operate in isolation. They are loaded into a context window that
already contains system prompts, instructions, conversation history, tool
results, and potentially other skills. The effective budget for any single
skill's instructions is a fraction of the total context window.

**Evidence — context accumulates and degrades:**
- Anthropic's context engineering guide [ref 9] describes context as creating
  "n-squared pairwise relationships for n tokens" and notes that LLMs have an
  "attention budget" that is depleted by every token, not just the skill's
  tokens.
- "Context Rot" [ref 16] finds that even for simple tasks, "models do not use
  their context uniformly; performance grows increasingly unreliable as input
  length grows." The practical implication is that a skill's effective accuracy
  depends on how much other content is in the context.
- Anthropic's guide [ref 9] recommends "tool result clearing" as "one of the
  safest, lightest-touch forms of compaction" — once a tool has been called
  deep in the message history, the raw result is unlikely to be needed again.
  Skills should design their workflows to minimize the context footprint of
  intermediate results.
- The Claude Code agent [ref 9] implements compaction by summarizing
  conversation contents when nearing context limits, preserving "architectural
  decisions, unresolved bugs, and implementation details" while discarding
  "redundant tool outputs."

Authoring guidance:
- Design skills to be context-efficient: produce explicit artifacts (files)
  rather than relying on the model remembering long intermediate outputs.
- When a skill delegates to sub-agents, expect summaries (1,000-2,000 tokens)
  rather than full transcripts.
- Prefer file-based artifacts over in-context accumulation for multi-step
  workflows. Write intermediate results to `.agent-layer/tmp/` and reference
  them by path.
- Be aware that a 300-line skill loaded into a 200K-token context is a small
  fraction of the window, but a 300-line skill loaded after 180K tokens of
  prior conversation is competing for the model's remaining attention.

### 12. Design for error recovery and convergence

Agent workflows fail. Skills should be designed so that failures are
recoverable, progress is visible, and the workflow converges toward completion
rather than looping indefinitely.

**Evidence — agents need environmental feedback:**
- Anthropic's building-effective-agents guide [ref 8] recommends that agents
  gain "ground truth from environment (tool results, code execution) at each
  step." Skills should design each phase to produce observable, checkable
  output that the next phase can validate.
- The same guide notes that coding agents are effective because "solutions are
  verifiable via automated tests; agents iterate using test feedback." This
  feedback-loop pattern generalizes: any skill phase that produces verifiable
  output enables the agent to self-correct.
- IFScale [ref 12] shows that error types shift under load: at low densities,
  models make modification errors (attempting but failing); at high densities,
  they make omission errors (completely ignoring instructions). Skills with
  explicit checkpoints between phases can catch omission errors before they
  compound.

Authoring guidance:
- Define convergence criteria for iterative workflows. State what "done" looks
  like and what triggers re-iteration vs. escalation.
- Require observable outputs at each phase transition (artifact files, status
  lines, or explicit state summaries).
- Set explicit loop limits or escalation triggers to prevent infinite loops
  when the workflow is not converging.
- Design phases so each one can be re-run independently if the previous
  attempt failed.

---

## Recommended section order for agent-layer skills

| Section | Purpose | Why it belongs there |
| --- | --- | --- |
| Frontmatter | Routing metadata | Loaded before the body; determines activation accuracy |
| Opening contract | One-sentence statement of what the skill does | Gives immediate orientation |
| Defaults / inputs | States the no-input behavior and accepted inputs | Prevents silent guessing |
| Required artifacts | Names file paths and report outputs | Makes the workflow inspectable |
| Multi-agent pattern | Names recommended reviewer/worker roles | Encourages shallow, explicit delegation |
| Global constraints | Hard rules that always apply | Critical invariants benefit from primacy [ref 11, 12] |
| Human checkpoints | Exact ask-user triggers | Keeps escalation explicit and rare |
| Workflow phases | Ordered execution steps | Main operational body |
| Guardrails | Common failure modes and negative constraints | Benefits from recency at the tail [ref 11] |
| Final handoff | What to report back | Keeps closeout deterministic |

This order is consistent with OpenAI's recommended prompt structure [ref 4]:
Identity (what the skill is) → Instructions (rules and constraints) → Examples
(if any) → Context (variable content loaded at runtime). The skill's opening
contract and global constraints correspond to Identity and Instructions; the
workflow phases correspond to the operational body; and dynamic context (file
reads, artifacts) is loaded during execution.

Practical ordering rule:
- Put routing, defaults, artifacts, and hard constraints before the phased
  workflow. These benefit most from the primacy effect [ref 12].
- Put explanations and examples after the contract, or move them out of
  `SKILL.md` entirely.
- Put guardrails and final-handoff instructions last. These benefit from the
  recency effect at the tail of the U-shaped attention curve [ref 11].
- Avoid placing new critical rules in the middle of the workflow phases, where
  the "lost in the middle" effect is strongest [ref 11].

---

## Authoring checklist

Before considering a skill done, verify that:
- The `description` clearly says what the skill does and when it should trigger.
- The skill has one primary job and one primary output contract.
- The top of the file contains the defaults, artifact rules, and hard stop
  conditions.
- Every required file or report path is explicit.
- Human checkpoints are concrete and sparse.
- The workflow has measurable success criteria.
- Any required script is non-interactive and documented with inputs/outputs.
- The skill still makes sense if a client only loads `SKILL.md`.
- The body is as short as possible without making behavior ambiguous.
- At least one positive-trigger and one negative-trigger prompt exist for
  evaluation.
- Guardrails and negative constraints appear at the end, not buried in the
  middle.
- The skill's context footprint is proportional to its complexity — simple
  skills should be short.

---

## Anti-patterns to avoid

| Anti-pattern | Why it fails | Evidence | Better pattern |
| --- | --- | --- | --- |
| Vague description like `Helps with PDFs` | Weak routing signal; poor implicit activation | Routing pattern requires distinct categories [ref 8] | Describe both the job and the trigger conditions |
| Multi-mode skill with several major branches | Increases instruction count and interference | Constraint composition degrades accuracy [ref 13]; 68% ceiling at 500 instructions [ref 12] | Split into separate skills with narrower triggers |
| Critical rule buried late in the file | Later instructions are easier to drop | Primacy effect peaks at 150-200 instructions [ref 12]; middle-position drop of ~20pp [ref 11] | Move invariant rules near the top |
| Laundry list of edge cases in `SKILL.md` | Bloats context and dilutes core instructions | Reasoning degrades "at much shorter input lengths than technical maximum" [ref 10] | Keep only canonical cases in the main file |
| Interactive script | Hangs or fails in autonomous runs | Scripts must avoid interactive prompts [ref 2] | Make scripts fully flag-driven with `--help` |
| Core behavior hidden in companion files | Breaks on clients that only load `SKILL.md` | Agent-layer serves only `SKILL.md` body | Keep the main workflow understandable in `SKILL.md` |
| Shared base-skill wrapper hierarchy | Creates fragile abstractions and drift | Rule of Three [ref 14, 15]; prefer duplication until N=3 | Accept structural duplication until reuse is clearly real |
| Untestable definition of done | Hard to evaluate or regress | Eval-driven improvement requires automatable checks [ref 7] | Add explicit artifacts, commands, or rubric outputs |
| Blanket permission expansion in the skill body | Conflicts with least-privilege policy | Skills are privileged instructions [ref 3]; context is trusted by default [ref 9] | Ask for approval at named high-impact steps |
| Accumulating intermediate results in context | Depletes attention budget for later instructions | Context creates n-squared pairwise relationships [ref 9]; context rot [ref 16] | Write intermediates to files; reference by path |
| Generic "ask when uncertain" checkpoint | Model interprets on every action, adding constraint load | Each checkpoint rule competes with workflow instructions [ref 12] | Name exact trigger conditions |

---

## Repo heuristics that are intentionally not universal

These are local preferences for this repository, not claims about the wider
ecosystem:
- Aim for skills that are comfortably below the 500-line validator warning;
  150-300 lines is the healthy default range here.
- Prefer separate skills over wrapper/base-skill patterns until there are at
  least three real consumers of the shared structure.
- Use repo-local artifact naming under
  `.agent-layer/tmp/<skill-name>.<run-id>.<type>.md` when the workflow produces
  reviewable intermediate files.
- Favor stable section names (`Defaults`, `Global constraints`,
  `Human checkpoints`, `Guardrails`, `Final handoff`) so reviewers can compare
  skills quickly.
- Accept structural duplication between skills with similar audit-loop shapes
  (e.g., `audit-and-fix-uncommitted-changes` and `improve-codebase`) rather
  than extracting a shared abstraction prematurely.

---

## Key findings summary

This section summarizes the most decision-relevant findings from the research
base. Each finding is linked to the principle(s) it supports.

### Input length degrades reasoning before the technical limit

"Same Task, More Tokens" [ref 10] shows that aggregate reasoning accuracy drops
from 0.92 to 0.68 (a 24-point decline) as input grows from ~250 to ~3,000
tokens — far below any model's technical context limit. Degradation begins at
~500 tokens. Even duplicate padding (exact copies of relevant text, requiring no
search or filtering) degrades accuracy, proving that length itself is the
issue. Chain-of-thought prompting does not mitigate this: CoT "does not mitigate
the drop in performance due to length" [ref 10]. Perplexity correlates
negatively with reasoning (rho = -0.95), so standard benchmarks mask this
problem. An independent study [ref 18] confirms that "performance degradation
is directly related to input length alone, regardless of where the relevant
evidence is positioned."

**Implication for skills:** Every additional line in a skill carries a reasoning
cost, even if the content is relevant. Principles 3 and 11.

### Context is used non-uniformly

"Lost in the Middle" [ref 11] finds a U-shaped attention curve: models attend
most to the beginning and end of context, with the middle receiving the weakest
attention. In 20-document QA, middle-positioned information yields ~52.9%
accuracy — worse than the 56.1% closed-book baseline — while beginning/end
positions maintain ~80%+. This means providing information in the wrong position
can be worse than not providing it at all. Extended-context models do not
eliminate this pattern: expanding context windows does not fix the U-shape.

**Implication for skills:** Place the most critical instructions at the
beginning (frontmatter, opening contract, global constraints) and the final
safety net at the end (guardrails, final handoff). Avoid introducing new
critical rules in the middle workflow phases. Principles 4 and section ordering.

### Instruction count has independent effects beyond length

IFScale [ref 12] benchmarks 20 models on 10-500 simultaneous instructions and
finds:
- Even the best frontier models only achieve 68% accuracy at 500 instructions.
- Three distinct degradation patterns: threshold (stable then cliff), linear
  (steady decline), and exponential (rapid early drop).
- Universal primacy effect: all models more reliably follow early instructions.
  Primacy peaks at 150-200 instructions.
- Error type shifts from modification (attempting but failing) to omission
  (completely ignoring) as density increases.

**Implication for skills:** Instruction count matters independently of token
count. A 200-line skill with 50 distinct behavioral instructions may perform
worse than a 300-line skill with 20 well-structured instructions. Principles 2,
3, and 4.

### Constraint composition multiplies difficulty

ComplexBench [ref 13] identifies 4 composition types and finds catastrophic
degradation under nesting. GPT-4 scores:
- Flat AND composition: 0.881
- Chain (depth 1): 0.787
- Selection (depth 1): 0.815
- Combined multi-layer compositions: as low as 0.083

Even GPT-4 fails on 20% of complex instructions. Deeply nested Selection
compositions (the pattern matching `if X then A, else if Y then B, else C`)
drop to 14.9% accuracy. Length constraints are the hardest single dimension
(0.409 average score), while topic/format constraints are easiest (~0.85).

**Implication for skills:** Multi-mode skills with conditional branching
(`if scope = X, do Phase 3a; if scope = Y, do Phase 3b`) create exactly the
composition types that LLMs handle worst. Principle 2.

### Context engineering is the primary lever

Anthropic's context engineering guide [ref 9] frames context management as a
discipline with four strategies: writing (curating what goes in), selecting
(choosing what to load), compressing (summarizing what's been used), and
isolating (using sub-agents for focused tasks). Sub-agents typically return
1,000-2,000 token summaries from explorations that consumed tens of thousands
of tokens.

**Implication for skills:** Skills are a context engineering artifact. Every
design decision about length, structure, and delegation is a context engineering
decision. Principle 11.

### Distractors degrade performance even at quantity one

"Context Rot" [ref 16] finds that even a single distractor reduces performance
relative to baseline. The degradation is non-uniform: some distractors cause
more damage than others, and the effect is model-specific.

**Implication for skills:** Extraneous content in a skill is not harmless
padding; it actively interferes with the model's ability to follow the core
instructions. Principle 3.

### Quantitative thresholds summary

| Metric | Value | Source |
| --- | --- | --- |
| Reasoning accuracy drop onset | ~500 tokens of input growth | [ref 10] |
| Aggregate accuracy decline (250→3K tokens) | 0.92 → 0.68 (24pp) | [ref 10] |
| Middle-position QA accuracy vs closed-book | 52.9% vs 56.1% (worse than no docs) | [ref 11] |
| Beginning/end-position accuracy | ~80%+ | [ref 11] |
| Best frontier model accuracy at 500 instructions | 68.9% (gemini-2.5-pro) | [ref 12] |
| Primacy effect peak density | 150-200 instructions | [ref 12] |
| Omission-to-modification ratio (weak models) | 26-35:1 | [ref 12] |
| Flat AND constraint composition (GPT-4) | 0.881 | [ref 13] |
| Nested multi-layer Selection (GPT-4) | 0.083-0.694 | [ref 13] |
| GPT-4 failure rate on complex instructions | 20% | [ref 13] |
| Catalog token cost per skill | ~50-100 tokens | [ref 1] |
| Full skill instruction budget | <5,000 tokens | [ref 1] |
| Recommended eval prompt set per skill | 10-20 prompts | [ref 7] |

---

## References

### Standards and platform documentation

1. Agent Skills. `Specification`. https://agentskills.io/specification
2. Agent Skills. `Using scripts in skills`.
   https://agentskills.io/skill-creation/using-scripts
3. OpenAI. `Agent Skills`. https://developers.openai.com/codex/skills
4. OpenAI. `Prompt engineering`.
   https://platform.openai.com/docs/guides/prompt-engineering
5. OpenAI. `Reasoning best practices`.
   https://platform.openai.com/docs/guides/reasoning-best-practices
6. OpenAI. `Prompting`. https://developers.openai.com/codex/prompting
7. OpenAI. `Testing Agent Skills Systematically with Evals`.
   https://developers.openai.com/blog/eval-skills
8. Anthropic. `Building effective agents`.
   https://www.anthropic.com/research/building-effective-agents
9. Anthropic. `Effective context engineering for AI agents`.
   https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents

### Empirical studies

10. Levy, Jacoby, Goldberg. `Same Task, More Tokens: the Impact of Input
    Length on the Reasoning Performance of Large Language Models`. ACL 2024.
    https://arxiv.org/abs/2402.14848
11. Liu et al. `Lost in the Middle: How Language Models Use Long Contexts`.
    TACL 2023. https://arxiv.org/abs/2307.03172
12. Jaroslawicz et al. `How Many Instructions Can LLMs Follow at Once?`
    (IFScale). 2025. https://arxiv.org/abs/2507.11538
13. Wen et al. `Benchmarking Complex Instruction-Following with Multiple
    Constraints Composition` (ComplexBench). NeurIPS 2024.
    https://arxiv.org/abs/2407.03978
14. Fowler, Martin. `Beck Design Rules`.
    https://martinfowler.com/bliki/BeckDesignRules.html
15. Metz, Sandi. `The Wrong Abstraction`.
    https://sandimetz.com/blog/2016/1/20/the-wrong-abstraction
16. Chroma Research. `Context Rot: How Increasing Input Tokens Impacts LLM
    Performance`. 2025. https://research.trychroma.com/context-rot
17. Zeng et al. `Order matters: Investigate the position bias in
    multi-constraint instruction following`. 2025.
18. `Context Length Alone Hurts LLM Performance Despite Perfect Retrieval`.
    2025. https://arxiv.org/abs/2510.05381
