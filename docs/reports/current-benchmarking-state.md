# Current benchmarking state

**As of:** 2026-08-01  
**Purpose:** Preserve the evidence and decisions needed to benchmark the newly simplified template skills without rediscovering the July campaign.

## Executive summary

The benchmark harness is now usable for iterative skill experiments, but the evidence does not support a general claim that Agent Layer improves quality. It does consistently show substantial cost inflation. The current reduced template skills and an instructions-only control have now been measured on the eight-task correlation-sorted Luna matrix.

- Across completed like-for-like comparisons, Agent Layer cost ranged from **1.35× to 14.48×** the bare arm.
- The current simplified Luna-low skills scored **36.46%** at **$20.92**, versus a fresh environment-qualified bare Luna-low baseline at **20.43%** and **$1.58**. The score rose **16.03 percentage points**, but cost rose **13.23×**. This treatment was also **37.36% more expensive** than the historical Agent Layer Luna-low arm despite using fewer invocations (55 versus 62).
- The system-instructions-only Luna-low control scored **15.08%** at **$2.14**: **5.35 points below** the fresh bare baseline while costing **1.35×** as much. System instructions alone did not explain the simplified treatment's score gain.
- The current simplified treatment was effectively tied with bare Luna medium on this matrix (**36.46% versus 36.47%**, `p = 0.998`) while costing **2.95×** as much. This remains the strongest evidence that raising bare reasoning is more cost-efficient than the current workflow.
- The strongest current high-reasoning result is negative for value: Luna high with Agent Layer scored **54.83%** versus **56.33%** bare (`p = 0.478`) and cost **$63.75** versus **$15.02** (**4.24×**).
- A Sol-medium run scored **57.59%** with Agent Layer versus **54.31%** bare, but cost **$55.38** versus **$15.85** (**3.49×**). With one run per task and no local repetition-based uncertainty, the **+3.28 percentage-point** difference is descriptive, not demonstrated improvement.
- Earlier Luna-low repeated campaigns did show score gains larger than their observed decision thresholds, but at **13.74–14.48×** baseline cost. These results apply only to their four-task suite and are confounded across versions by a Codex client change.
- Cost is driven by complete orchestration sessions, especially implementation, review, verification, and fixing—not by loading the skill text. In the latest phase audit, verification plus fixing consumed **57.54% of dollars** and **63.11% of tokens**.
- The next cost experiment should change workflow behavior, not merely shorten skill text: constrain dispatch/fan-out or remove expensive review/repair sessions, then use the completed environment-qualified bare arm and today's treatments as immutable comparators.

The practical optimization target is therefore: **cut treatment cost materially while preserving bare-model quality**. A quality increase is welcome, but should not be assumed or required for the simplified-skills iteration.

## Evidence hierarchy

This report uses, in descending order of authority:

1. Canonical local campaign and matrix reports under `.agent-layer/state/benchmarks/deepswe/`.
2. Phase-cost analysis under `.agent-layer/tmp/`.
3. Recent Codex JSONL conversations under `.codex/sessions/2026/07/` for chronology, decisions, failed attempts, and limitations.
4. Published-data simulations only as design input. They are not local benchmark results.

The JSONL review was summarized by a dispatched `gpt-5.6-luna` agent at xhigh reasoning, then checked against the local canonical artifacts. One stale transcript conclusion was corrected during that check: the multi-arm matrix was stopped temporarily, but later completed and now has a canonical expanded report.

## Completed quantitative evidence

Scores below are the benchmark's calibrated composite scores unless noted otherwise. Costs are midpoint estimates that account for cached-input pricing; canonical reports also retain minimum and maximum accounting bounds.

| Experiment | Bare | Agent Layer | Observed difference | Cost multiple | Interpretation |
|---|---:|---:|---:|---:|---|
| Luna low, repeated four-task campaign, “Simplified skills” | 55.09%, $1.61 | 66.22%, $23.24 | +11.14 pp; threshold ±8.67 pp | 14.48× | Classified better on this suite, but extremely costly. |
| Luna low, same campaign, “Implementation Readiness and Batched Repair” | 55.09%, $1.61 | 66.40%, $22.05 | +11.31 pp; threshold ±8.53 pp | 13.74× | Classified better; baseline used Codex 0.144.6 and treatment used 0.146.0. |
| Luna medium, repeated three-task campaign | 60.35%, $2.48 | 58.10%, $15.74 | −2.25 pp; threshold ±21.50 pp | 6.33× | Inconclusive. EICrud also exposed an environment mismatch. |
| Luna high, eight-task matrix | 56.33%, $15.02 | 54.83%, $63.75 | −1.50 pp; `p = 0.478` | 4.24× | No demonstrated score difference; large cost penalty. |
| Sol medium, four-task matrix | 54.31%, $15.85 | 57.59%, $55.38 | +3.28 pp | 3.49× | Descriptive only: one run per task and no local repeated-arm uncertainty. |
| Luna high, Effect SSE single-task probe | 60.23%, $3.18 | 62.24%, $6.52 | +2.00 calibrated pp; +4.26 raw F2P pp | 2.05× | One task only; useful as a cheap smoke test, not an aggregate conclusion. |
| Luna low, eight-task matrix, current simplified templates | 20.43%, $1.58 | 36.46%, $20.92 | +16.03 pp; `p = 0.000037` | 13.23× | Higher score, but cost increased rather than decreased. One local run per task; inference uses published variance. |
| Luna low, eight-task matrix, system instructions only | 20.43%, $1.58 | 15.08%, $2.14 | −5.35 pp; `p = 0.0159` | 1.35× | Cheap relative to skills, but worse than bare in this run. |

Canonical sources:

- Repeated Luna-low campaign: `.agent-layer/state/benchmarks/deepswe/campaigns/d54e11fa1206fea80a26c918bb984b22e813040914d82eff39748294b78f7fd1/report/report.json`
- Repeated Luna-medium campaign: `.agent-layer/state/benchmarks/deepswe/campaigns/f47682553f3a448db56e32bd7fd46abdd8a6d1057b83d5ffabe3e10f3d5bdac7/report/report.json`
- Five-arm Luna matrix and pairwise tests: `.agent-layer/state/benchmarks/deepswe/matrices/e33ae9ff37727c17e13b65325eaaa7f87ac91c443979a0c2548f0e92861d4282/report/full-report.json`
- Sol-medium matrix: `.agent-layer/state/benchmarks/deepswe/matrices/c323b56188d878d4f20d1a450acdefd5d1b80793f5b52a1eecb038b9a01eb120/report/report.json`
- Effect SSE probe: `.agent-layer/state/benchmarks/deepswe/matrices/307eb23b954c0eda5afc6bdfc8701ed0e0011dbe9c49e211a70d0acdd09b75ee/report/report.json`

### What the expanded Luna matrix adds

The completed eight-task matrix also compared reasoning levels:

| Arm | Score | Cost |
|---|---:|---:|
| Bare Luna low, legacy harness | 7.85% | $1.49 |
| Bare Luna low, environment-qualified harness | 20.43% | $1.58 |
| Agent Layer Luna low | 29.59% | $15.23 |
| Agent Layer Luna low, current simplified skills | 36.46% | $20.92 |
| Agent Layer Luna low, system instructions only | 15.08% | $2.14 |
| Bare Luna medium | 36.47% | $7.08 |
| Bare Luna high | 56.33% | $15.02 |
| Agent Layer Luna high | 54.83% | $63.75 |

The historical Agent Layer Luna-low arm outperformed its legacy bare Luna-low arm but cost **10.21×** as much. The current simplified arm also outperformed its compatible fresh baseline but cost **13.23×** as much. It produced essentially the same score as bare Luna medium (`p = 0.998`) while costing **2.95×** as much. At high reasoning, Agent Layer and bare were not distinguishable (`p = 0.478`), while Agent Layer cost **4.24×** as much. This supports the hypothesis that spending on a stronger bare model/reasoning level can be more efficient than adding the current workflow.

The two bare Luna-low results are retained separately because the harness now fingerprints certified task-environment identities. The legacy **7.85%** result is not a compatible baseline for the new treatments; the environment-qualified **20.43%** rerun is. Both used Codex 0.146.0, so their spread also demonstrates why one run per task should not be treated as a stable model estimate.

The matrix uses one local run per task and published four-trial per-task variance for its Welch–Satterthwaite comparisons. Its inferential results therefore depend on transfer of published variance to the local Codex harness.

## Where treatment cost goes

The audited 12-cell “Implementation Readiness and Batched Repair” treatment cost **$22.05** midpoint and processed about **119.7 million** input-plus-output tokens. Its phase breakdown was:

| Phase | Dollar share | Token share |
|---|---:|---:|
| Plan writing | 3.00% | 1.68% |
| Plan review | 18.91% | 13.33% |
| Implementation | 20.55% | 21.88% |
| Verification | 25.70% | 24.89% |
| Fixing | 31.85% | 38.22% |

Verification and fixing together consumed **57.54% of dollars** and **63.11% of tokens**. The initial requests that loaded skills/instructions were previously measured at only about **$0.95** across 117 invocations. The expensive behavior is repeated full sessions, fan-out, continuation, and repair—not prompt bytes alone.

Source: `.agent-layer/tmp/latest-treatment-phase-costs.json` and `.agent-layer/tmp/latest-treatment-role-costs.json`.

This makes the current simplification direction well targeted: remove redundant review/verification stages, reduce fan-out and polling, and keep repair bounded. The previously discussed **$10.5–$12.5** target for a complete treatment was a projection, not a measured result.

## Headroom and task-selection problem

Two opposite regimes repeatedly appeared:

- Frontier models at higher reasoning often saturate individual tasks. In the six-task Luna-high headroom pilot, five raw fail-to-pass scores were approximately **96.7–100%**, while OPA template-string reconstruction scored **0%**. This leaves little useful upward headroom on most tasks and a floor effect on the outlier. The conversation briefly misidentified Termenv as the zero; the canonical matrix records Termenv at **97.14%** and OPA at zero.
- Lower reasoning creates headroom, but can make Agent Layer look useful simply by spending an order of magnitude more computation than the bare arm.

Published DeepSWE scores did not reliably predict local Codex-harness headroom. The local harness and agent behavior can raise raw task scores enough to erase the published headroom. Task selection must therefore use local pilot evidence, not published means alone.

The modified DeepSWE calibration and optimizer remain useful for choosing and weighting tasks, but their simulated noise bands, projected costs, and published-model contrasts are **projections**. They must not be presented as observed Agent Layer effects.

## Harness state and lessons already incorporated

The current code has addressed or exposed the major campaign hazards:

- Campaign identity hashes the exact plan, environment-qualified task checksums, model, and reasoning configuration.
- A completed immutable baseline is shared by fingerprinted treatment versions; changing the template bundle creates a new treatment version.
- `--check` validates readiness without paid calls, and `--max-new-runs 1` supports a bounded paid probe.
- Baseline and treatment use the same readiness contracts and service startup requirements.
- Reports regenerate offline from immutable evidence and preserve cost bounds and provider-client versions.
- Matrix runs accept explicit dispatch targets for plan reviewers, implementer, code reviewer, and fixer.

Known interpretation cautions:

- A prior batch passed host preflight but failed all 12 cells because `codex` was absent inside the container. Preflight was strengthened afterward; a shared infrastructure failure must abort the batch rather than create many misleading task failures.
- Dispatch-conformance scans falsely reported `0/N` when they included preflight JSON files. Raw dispatch evidence proved the actual calls were conformant. Conformance diagnostics do not determine score eligibility.
- An MCP-era cost report double-counted cumulative resumed-session usage and omitted built-in subagent sessions. Its **$33.19** total is invalid; exact cost for that run is unknown.
- DeepSWE's binary `reward` can be zero while `f2p_score` is high. Current reports intentionally use fail-to-pass score rather than the all-or-nothing reward.
- Provider-client version changes remain visible comparability warnings, not silent invalidation.

## What is established—and what is not

Established:

- The historical template workflow is expensive relative to a bare agent.
- Its largest costs come after plan writing, especially in verification and fixing.
- At high reasoning, current evidence shows no quality benefit large enough to justify the observed 4.24× cost.
- At lower reasoning, Agent Layer can improve scores on selected suites, but can be less cost-efficient than raising the bare model's reasoning level.
- Local harness/task headroom must be measured empirically.
- The current simplified skills did **not** meet the intended cost-reduction goal on this matrix: **$20.92**, versus **$15.23** for the historical Agent Layer Luna-low arm and **$1.58** for the compatible bare arm.
- System instructions without skills are comparatively cheap but did not preserve bare-model quality in this run.

Not established:

- That skills never help, across models, tasks, or workflows.
- That the current simplified treatment's apparent score gain or the instructions-only regression will replicate. Each task has one local run and the inferential comparison imports published per-task variance.
- A stable general-purpose effect size. Several experiments have one run per task, use published variance, or have client/harness confounds.
- That the modified DeepSWE score is directly comparable to the official DeepSWE leaderboard or SkillsBench.

## Next experiment after the simplified-skills run

The completed simplified treatment disproved the immediate cost hypothesis: reducing the skill files did not reduce end-to-end spend. It made 55 paid invocations and cost **$20.92**, so the remaining optimization target is orchestration behavior.

Recommended sequence:

1. Use this same eight-task selection and reuse the certified baseline arm `60febd232faf…`; do not rerun it unless compatibility checks require a new identity.
2. Change one behavioral cost driver at a time: dispatch count, review/fix fan-out, continuation, or repair limits.
3. Run one bounded treatment cell first and inspect total cost, invocation count, dispatch graph, and score before expanding.
4. Treat **$2.14** as the observed instructions-only cost floor, **$15.23** as the historical Agent Layer Luna-low reference, and **$20.92** as the current simplified-workflow reference.
5. Prefer a bare reasoning-level arm if the treatment approaches its cost: bare Luna medium achieved the same score for **$7.08**, and bare Luna high scored **56.33%** for **$15.02**.

The canonical combined report is `.agent-layer/state/benchmarks/deepswe/matrices/e33ae9ff37727c17e13b65325eaaa7f87ac91c443979a0c2548f0e92861d4282/report/full-report.html`; its adjacent JSON contains task-level scores, cost bounds, invocation counts, and pairwise tests.

## JSONL source index

The detailed conversation review covered these sessions:

- `019fb05a-db5a-7411-a494-e0b81bd53190`: diagnostic campaign, failed container preflight, and fail-loud requirements.
- `019fb3a4-3a82-7d92-a9a1-4a75ba1ea187`: Luna-medium campaign, phase-cost investigation, readiness fixes, and workflow simplification decisions.
- `019fb646-3940-7172-9df8-b97dc6a1a374`: multi-arm design, statistical/noise comparisons, and the temporarily stopped matrix.
- `019fb8f1-8cb0-7611-abdc-0697bf04278b`: corrected campaign reporting, headroom pilots, MCP accounting investigation, and single-task probe.
- `019fb9b0-a2ab-7230-8a4e-82e377472aa7`: Sol-medium cost-reduction experiment and decision to simplify skills further.

Files are under `.codex/sessions/2026/07/{29,30,31}/rollout-*<session-id>.jsonl`. The dispatched extraction is preserved at `.agent-layer/tmp/runs/a8a0352c-7630-4112-9d02-833165ec6d83/result.md`.
