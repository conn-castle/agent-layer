# Instruction Design Guide

This document is the canonical instruction-authoring guide for always-loaded
agent instruction sources.

Research and sources in this guide were verified on 2026-03-16. The guide
intentionally separates:
- evidence-backed design principles
- practical authoring guidance
- authoring heuristics used to keep instructions maintainable

Participant-terminology sources were checked on 2026-07-04.

This guide is the instruction-file counterpart to `docs/SKILL-DESIGN.md`,
which covers skill authoring. The two documents share several foundational
references (context engineering, instruction density, primacy effects) but
apply them to different authoring surfaces.

## Evidence model

- Official prompting guidance: Anthropic and OpenAI agent/prompting docs.
- Production agent experience: Manus, 47billion, Verdent AI, Addy Osmani.
- Empirical studies: instruction density, context length, constraint
  composition, primacy/recency effects, minimal agent scaffolding.
- Benchmark results: SWE-bench Mobile, SWE-bench Verified, IFScale,
  ComplexBench.

When this guide gives a numeric limit, it is either a published finding or
explicitly labeled as an authoring heuristic.

---

## Core principle: minimal sufficient context

The single most important principle for instruction design is finding the
minimal set of information that fully outlines expected behavior.

> "You should be striving for the minimal set of information that fully
> outlines your expected behavior. (Note that minimal does not necessarily
> mean short; you still need to give the agent sufficient information up
> front to ensure it adheres to the desired behavior.)"
> — Anthropic, "Effective context engineering for AI agents" [ref 1]

This is not a vague guideline. It is the central finding across multiple
independent sources:

- **Anthropic** [ref 1]: Start with a minimal prompt and the best model, then
  add instructions based on observed failure modes — not preemptively.
- **OpenAI** [ref 2]: "GPT-5 ... requires less scaffolding than older models;
  shorter, clearer instructions can perform better."
- **OpenAI** [ref 3]: Reasoning models "perform best with straightforward
  prompts. Some prompt engineering techniques ... may not enhance performance
  (and can sometimes hinder it)."
- **SWE-bench Mobile** [ref 10]: "Complexity appears to hurt performance;
  overly detailed prompts reduce Task Success from 10.0% to 4.0%."
- **Princeton/Stanford SWE-agent** [ref 11]: A ~100-line minimal agent scored
  >74% on SWE-bench Verified — exceeding the SotA at time of publication
  (originally 65% with Sonnet 4; now >74% with newer models).
- **Anthropic on edge cases** [ref 1]: "Teams will often stuff a laundry list
  of edge cases into a prompt... We do not recommend this. Instead, we
  recommend working to curate a set of diverse, canonical examples."

The practical implication: every instruction line must earn its place by
changing agent behavior. If removing a line would not change what the agent
does, the line should not be there.

---

## Evidence-backed design principles

### 1. Start minimal, add based on failures

Do not preemptively specify every rule, edge case, and workflow variation.
Start with the smallest instruction set that covers the core behavior, test
against realistic scenarios, and add instructions only when the agent fails
in a way that a new instruction would fix.

**Evidence:**
- Anthropic [ref 1]: "It's best to start by testing a minimal prompt with the
  best model available ... and then add clear instructions and examples to
  improve performance based on failure modes found during initial testing."
- OpenAI [ref 2]: "Start with evals. Begin by running your existing prompts
  as is against your evals to establish a baseline."
- Anthropic [ref 4]: "Start with simple prompts, optimize them with
  comprehensive evaluation, and add multi-step agentic systems only when
  simpler solutions fall short."
- 47billion production report [ref 8]: "Small changes in system prompts
  produce dramatically different behaviors. Plan for weeks of iterative
  refinement, not days."

**Authoring guidance:**
- Before adding a new rule, test whether the current instructions already
  produce the desired behavior. Models improve over time and may already
  handle cases that required explicit rules for older models.
- When adding a rule, phrase it as a targeted correction to an observed
  failure, not as a general principle the model might already follow.
- Prefer adding one concrete example over adding three abstract rules.

### 2. Keep instructions lean and high-signal

Every token in the instruction set carries a reasoning cost, even if the
content is relevant. Instructions compete with each other and with the rest
of the context for the model's attention budget.

**Evidence — input length degrades reasoning:**
- "Same Task, More Tokens" [ref 5]: Reasoning accuracy drops from 0.92 to
  0.68 as input grows from ~250 to ~3,000 tokens. Even duplicate padding
  degrades accuracy, proving that length itself is the issue.
- "Context Rot" [ref 6]: "Models do not use their context uniformly;
  performance grows increasingly unreliable as input length grows." Even a
  single distractor reduces performance relative to baseline.

**Evidence — instruction count matters independently:**
- IFScale [ref 7]: Even the best frontier models achieve only 68% accuracy at
  500 instructions. Three degradation patterns: threshold decay, linear decay,
  and exponential decay.

**Authoring guidance:**
- Remove repeated rationale, decorative prose, and edge cases that do not
  change behavior.
- Prefer short, atomic bullets over dense paragraphs.
- Deduplicate rules that appear in multiple instruction files. Each rule
  should have one authoritative home.
- If an instruction file references a skill for detailed behavior, do not
  re-specify the same behavior in the instruction file.

### 3. Put critical rules early, safety nets last

Instructions appearing earlier in the prompt are more reliably followed.

**Evidence:**
- IFScale [ref 7]: Universal primacy effect across all 20 models tested.
  Error rates are lowest in the first third of instructions and highest in
  the final third. At moderate density (150-200 instructions), the primacy
  effect peaks.
- "Lost in the Middle" [ref 9]: U-shaped attention curve. Beginning and end
  positions maintain ~80%+ accuracy. Middle positions drop to ~52.9% — worse
  than the closed-book baseline.

**Authoring guidance:**
- Place mandatory rules, hard constraints, and fail-safe behaviors at the top
  of each instruction file.
- Place workflow defaults, definitions of done, and operational details in the
  middle.
- Place guardrails and safety-net reminders at the end (benefiting from
  recency).
- Never introduce a new critical rule in the middle of a long instruction
  file.

### 4. Find the right altitude

System prompts should be specific enough to guide behavior but flexible
enough to let the model reason about edge cases.

**Evidence:**
- Anthropic [ref 1]: "The right altitude is the Goldilocks zone between two
  common failure modes. At one extreme, we see engineers hardcoding complex,
  brittle logic in their prompts... At the other extreme, engineers sometimes
  provide vague, high-level guidance that fails to give the LLM concrete
  signals."
- OpenAI [ref 3]: Reasoning models "perform best with straightforward
  prompts." Prompt engineering techniques like chain-of-thought "may not
  enhance performance (and can sometimes hinder it)."
- SWE-bench Mobile [ref 10]: "Prompts focusing on code quality outperform
  those emphasizing workflow." Overly detailed prompts (Comprehensive,
  Structured Checklist) cut Task Success from 10.0% to 4.0%.
- ComplexBench [ref 12]: Nested conditional logic (if X then A, else if Y
  then B) collapses model performance catastrophically — as low as 8.3% for
  deep Selection compositions.

**Authoring guidance:**
- State what the agent must do and must not do. Do not specify how to do it
  step-by-step unless the sequence is critical and non-obvious.
- Prefer heuristics that the model can apply flexibly ("prefer the smallest
  fix that addresses the root cause") over rigid conditional logic ("if the
  fix touches more than 3 files, ask the user; if it touches 1-3 files,
  proceed; if it touches only tests, always proceed").
- When you find yourself writing if/else branching in instructions, consider
  whether the model would already make the right choice without the branching.

### 5. Do not duplicate across instruction files

Each rule should have one canonical home. Duplication across instruction files
creates contradictions when one copy is updated and the other is not, and
wastes the model's attention budget on redundant content.

**Evidence:**
- IFScale [ref 7]: Instruction count has independent effects beyond length.
  Each additional instruction competes for attention, even if it says the
  same thing as an earlier instruction.
- Anthropic [ref 1]: "Find the smallest set of high-signal tokens."

**Authoring guidance:**
- If a rule appears in one always-loaded instruction file, do not restate it in
  another always-loaded instruction file.
- If a behavior is fully specified in a skill file, do not re-specify it in
  the base instruction files.
- When two instruction files need to reference the same concept, have one
  file own the rule and the other reference it briefly ("see Rules for the
  definitive constraint").

### 6. Validate changes empirically

Prompt changes should be tested, not intuited. Every instruction change is a
hypothesis that should be verified against realistic scenarios.

**Evidence:**
- OpenAI [ref 2]: "Begin by running your existing prompts as is against your
  evals to establish a baseline and see where outputs diverge."
- OpenAI [ref 13]: "Validate prompt changes with internal evals instead of
  intuition."
- Anthropic [ref 1]: Add instructions "based on failure modes found during
  initial testing" — not preemptively.
- Anthropic [ref 4]: "Measuring performance and iterating on implementations"
  is the key to success.

**Authoring guidance:**
- Before editing an instruction file, define what behavior you expect to
  change and how you would verify it.
- After editing, test against the same scenarios that motivated the change.
- If an instruction change does not produce a measurable behavior difference,
  consider reverting it — the instruction is consuming attention budget
  without value.

### 7. Treat instructions as privileged code

Instruction files define what the agent believes is correct behavior. They
have equivalent impact to production code and should be reviewed with the
same rigor.

**Evidence:**
- OpenAI [ref 14]: Skills and instructions "can influence planning, tool
  usage, and command execution" and should be treated as privileged
  instructions/code.
- Anthropic [ref 1]: Agents treat their context as authoritative.
- 47billion [ref 8]: "Small changes in system prompts produce dramatically
  different behaviors."
- Manus [ref 15]: After rebuilding their agent framework four times, Manus
  learned that context engineering is experimental — a "manual process of
  architecture searching, prompt fiddling, and empirical guesswork."
- Osmani [ref 16]: LLMs are "literalists — they'll follow instructions, so
  give them detailed, contextual instructions." Maintaining rules files
  (CLAUDE.md) with process rules and preferences reduces off-script behavior.

**Authoring guidance:**
- Review instruction changes with the same care as code reviews.
- Version instruction files in source control alongside the code they govern.
- Do not make speculative instruction changes without a clear motivation and
  verification plan.

### 8. Use participant terminology precisely

Instruction files should distinguish the agent conversation from the software
being built. Current platform and prompt specifications use `user` as the
standard message role for the person providing instructions to the model, and
`assistant` for the model's messages [ref 17, 19, 20]. OpenAI's current
prompt-engineering guide also distinguishes `developer` messages from `user`
messages, with `user` messages carrying the model-facing application's
per-request input [ref 17].

Use these terms consistently:

- **user**: the person talking to the agent in this session. Use this for
  instruction-following rules such as `ask the user`, `the user requested`,
  and `user-supplied files`.
- **end user**: the person who uses the software, product, API, or workflow the
  agent is helping build. Use this for product impact language such as
  `end-user-facing behavior`, `end-user-visible feature`, and `end-user
  workflow`.
- **human**: a person as distinct from an agent, automation, or programmatic
  consumer. Use this for human-in-the-loop concepts such as `human checkpoint`,
  `human review`, or `human confirmation`, and for model-spec distinctions
  between real-time human interaction and programmatic output [ref 18].

Authoring guidance:

- Do not use `the human` as a generic synonym for `the user`.
- Prefer `ask the user` when the instruction means "ask the person in this
  chat."
- Prefer `end-user-visible` or `end-user-facing` when the instruction concerns
  the people affected by the product being changed.
- Keep `human checkpoint` as the term for an explicit gate that requires a
  person rather than an autonomous agent.

---

## Recommended instruction file structure

| Section | Purpose | Position rationale |
| --- | --- | --- |
| Title and scope | What this file covers | Immediate orientation |
| Hard rules and constraints | Mandatory behavior, never-do rules | Primacy effect: early = most reliable [ref 7, 9] |
| Workflow defaults | Standard operating behavior | Middle: operational body |
| Definitions and protocols | Conventions, definitions of done | Middle: elaboration of rules |
| Safety nets and reminders | Common failure modes, final checks | Recency effect: end = second-most reliable [ref 9] |

This mirrors the U-shaped attention curve: critical content at the beginning
and end, operational detail in the middle.

---

## Instruction file sizing

There is no universal correct length. The right size depends on the
complexity of the behavior being specified and the failure modes observed
in practice.

**Quantitative context from research:**

| Metric | Value | Source |
| --- | --- | --- |
| Reasoning accuracy drop onset | ~500 tokens of input growth | [ref 5] |
| Aggregate accuracy decline (250 to 3K tokens) | 0.92 to 0.68 (24pp) | [ref 5] |
| Best frontier model at 500 instructions | 68% accuracy | [ref 7] |
| Primacy effect peak | 150-200 instructions | [ref 7] |
| Overly detailed prompts vs simple | 4.0% vs 10.0% Task Success | [ref 10] |
| Minimal agent (~100 lines) | >74% SWE-bench Verified | [ref 11] |

**Authoring heuristics (local examples, not universal standards):**
- Each instruction file should aim for the minimum content needed to prevent
  observed failure modes.
- If an instruction file exceeds 50 lines, audit it for duplication,
  redundancy, and rules that the model already follows without prompting.
- The total instruction set (all files combined) should be treated as a single
  attention-budget expense. Adding a new instruction file is not free — it
  competes with all other loaded context.

---

## Anti-patterns to avoid

| Anti-pattern | Why it fails | Evidence | Better pattern |
| --- | --- | --- | --- |
| Duplicating rules across instruction files | Wastes attention budget; creates contradiction risk | Instruction count matters independently [ref 7] | One canonical home per rule |
| Laundry list of edge cases | Dilutes core instructions | "We do not recommend this" [ref 1]; accuracy drops with length [ref 5] | Curate diverse canonical examples |
| Step-by-step workflow in instruction files | Over-constrains reasoning; brittle | Overly detailed prompts hurt performance [ref 10]; nested logic collapses accuracy [ref 12] | State goals and constraints; let the model reason about steps |
| Adding rules preemptively | May not change behavior; wastes budget | Start minimal, add based on failures [ref 1, 2] | Test first, add only for observed failures |
| Vague guidance like "be careful" | No concrete behavioral signal | Models need "concrete signals for desired outputs" [ref 1] | Name the exact constraint or trigger |
| Restating skill behavior in instructions | Double attention cost; drift risk | Smallest set of high-signal tokens [ref 1] | Reference the skill; do not respecify |
| Critical rules buried in the middle | Lower reliability due to attention curve | Lost in the Middle: middle worse than no info [ref 9] | Put critical rules first or last |

---

## Relationship to skill files

Instruction files and skill files serve different purposes and should not
overlap:

- **Instruction files** define global behavior constraints that apply across
  all agent work: rules, protocols, conventions, tool policies, and memory
  workflows.
- **Skill files** define specific workflow behavior for a named task: phases,
  artifacts, delegation patterns, and domain-specific constraints.

When a behavior is specific to a single workflow, it belongs in the skill
file. When it applies across all workflows, it belongs in an instruction
file. When both need to reference the same concept, the instruction file
should own the global rule and the skill should reference it or apply it
in its domain-specific context.

See `docs/SKILL-DESIGN.md` for the skill authoring counterpart to this guide.

---

## Key findings summary

### Minimal sufficient context is the primary lever

Multiple independent sources converge: the most effective instruction sets
are the smallest ones that produce correct behavior. Adding more instructions
does not always improve performance and frequently degrades it.

- Princeton's ~100-line agent scored >74% on SWE-bench Verified — exceeding
  most full-scaffold agents [ref 11].
- SWE-bench Mobile found overly detailed prompts cut Task Success from 10.0%
  to 4.0% [ref 10].
- OpenAI explicitly states GPT-5 "requires less scaffolding" [ref 2].
- Anthropic explicitly warns against "laundry list of edge cases" [ref 1].

**Implication for instructions:** Every rule, constraint, and protocol in the
instruction files should be there because removing it causes observable
failure — not because it seems like good practice to state explicitly.

### Models are improving faster than instructions are pruned

As models improve in instruction following, reasoning, and agentic behavior,
rules that were necessary for earlier models may become unnecessary overhead.
Regular audits of instruction files should test whether existing rules are
still needed.

- GPT-5 "follows instructions more reliably than any of its predecessors"
  [ref 2].
- Reasoning models "understand the user's intent and handle gaps in the
  instructions" [ref 3].
- Anthropic's building-effective-agents guide [ref 4]: "add complexity only
  when it demonstrably improves outcomes."

**Implication for instructions:** Instruction files should be periodically
re-evaluated. Test the current instruction set with recent models and remove
rules that the model follows correctly without explicit prompting.

### The right altitude matters more than the right length

The difference between effective and ineffective instructions is not length
but specificity-at-the-right-level. Instructions that are too specific create
brittle logic; instructions that are too vague create no behavioral signal.

- Anthropic's Goldilocks zone [ref 1]: specific enough to guide, flexible
  enough for heuristics.
- SWE-bench Mobile [ref 10]: quality-focused prompts outperform
  workflow-focused prompts.
- ComplexBench [ref 12]: nested conditional logic collapses performance.

**Implication for instructions:** Prefer clear constraints and goals over
detailed procedural steps. Let the model reason about how to satisfy the
constraints.

---

## References

### Official guidance

1. Anthropic. "Effective context engineering for AI agents." Sep 2025.
   https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents
2. OpenAI. "A practical guide to building with GPT-5." Aug 2025.
   https://openai.com/business/guides-and-resources/a-practical-guide-to-building-with-ai/
3. OpenAI. "Reasoning best practices."
   https://developers.openai.com/docs/guides/reasoning-best-practices
4. Anthropic. "Building effective agents."
   https://www.anthropic.com/research/building-effective-agents

### Empirical studies

5. Levy, Jacoby, Goldberg. "Same Task, More Tokens: the Impact of Input
   Length on the Reasoning Performance of Large Language Models." ACL 2024.
   https://arxiv.org/abs/2402.14848
6. Chroma Research. "Context Rot: How Increasing Input Tokens Impacts LLM
   Performance." 2025. https://research.trychroma.com/context-rot
7. Jaroslawicz et al. "How Many Instructions Can LLMs Follow at Once?"
   (IFScale). 2025. https://arxiv.org/abs/2507.11538
8. 47billion. "AI Agents in Production: Frameworks, Protocols & What Works
   in 2026." 2026.
   https://47billion.com/blog/ai-agents-in-production-frameworks-protocols-and-what-actually-works-in-2026/
9. Liu et al. "Lost in the Middle: How Language Models Use Long Contexts."
   TACL 2023. https://arxiv.org/abs/2307.03172

### Benchmark studies

10. SWE-bench Mobile. "Can Large Language Model Agents Develop Industry-Level
    Mobile Applications?" 2026. https://arxiv.org/abs/2602.09540
11. Princeton/Stanford NLP. "mini-swe-agent: ~100 lines of Python, >74% on
    SWE-bench Verified." 2025. https://github.com/SWE-agent/mini-swe-agent
12. Wen et al. "Benchmarking Complex Instruction-Following with Multiple
    Constraints Composition" (ComplexBench). NeurIPS 2024.
    https://arxiv.org/abs/2407.03978
13. OpenAI. "GPT-5 prompting guide." 2025.
    https://developers.openai.com/cookbook/examples/gpt-5/gpt-5_prompting_guide/
14. OpenAI. "Agent Skills." https://developers.openai.com/codex/skills

### Production experience

15. Manus (Yichao Ji). "Context Engineering for AI Agents: Lessons from
    Building Manus." Jul 2025.
    https://manus.im/blog/Context-Engineering-for-AI-Agents-Lessons-from-Building-Manus
16. Addy Osmani. "My LLM coding workflow going into 2026." Dec 2025.
    https://addyosmani.com/blog/ai-coding-workflow/

### Participant terminology sources

17. OpenAI. "Prompt engineering."
    https://developers.openai.com/api/docs/guides/prompt-engineering
18. OpenAI. "Model Spec." 2025-02-12.
    https://model-spec.openai.com/2025-02-12.html
19. Microsoft. "Prompt engineering concepts - .NET."
    https://learn.microsoft.com/en-us/dotnet/ai/conceptual/prompt-engineering-dotnet
20. Model Context Protocol. "Prompts."
    https://modelcontextprotocol.io/specification/2025-06-18/server/prompts
