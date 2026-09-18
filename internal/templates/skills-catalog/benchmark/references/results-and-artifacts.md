# Results and run artifacts

Use the report for the overview, then follow individual task runs into their retained artifacts when the aggregate numbers need explanation.

## Read the report

`report.html` presents the main evidence:

- **Score versus cost** shows each experiment's measured outcome and observed spend.
- **Experiment summary** shows calibrated score, observed cost, completed runs, and comparability or workflow warnings.
- **Pairwise score evidence** shows a difference and uncertainty calculation when the study contains enough applicable evidence. An unavailable comparison means the report could not calculate it; it does not establish a tie.
- **Task evidence** shows the raw task result, calibrated contribution, repetitions, variation, weight, and cost. Use it to see which tasks drove the aggregate result.
- **Statistical method, limitations, and provenance** explain how the report was produced and qualify its scope.

`report.json` is the canonical machine-readable form. Its top-level sections include `execution`, `selection`, `experiments`, `comparisons`, `holm_family`, and `limitations`. Inspect the report's own schema rather than assuming every version has identical fields.

Interpret the measurements in plain language:

- A calibrated score summarizes performance on the selected tasks; higher is better.
- Observed cost is a separate measure. Look at score and cost together when that matters to the user.
- Per-task raw results and contributions reveal whether an aggregate change is broad or concentrated.
- Repetitions, variance, adjusted p-values, inference source, missing attempts, and warnings describe how much support the report has for a comparison. Explain them when present; do not invent certainty when they are absent.

## Locate the study evidence

The report path identifies the study directory. Benchmark state normally lives below:

```text
.agent-layer/state/benchmarks/deepswe/studies/<study-id>/
```

Useful paths within a study are:

```text
study-manifest.json
report/report.html
report/report.json
arms/<arm-id>/attempts/<n>/tasks/<task-id>/result.json
arms/<arm-id>/attempts/<n>/tasks/<task-id>/artifacts/<event-id>/execution-receipt.json
arms/<arm-id>/attempts/<n>/tasks/<task-id>/artifacts/<event-id>/jobs/<event-id>/<job>/
```

Treat the path printed by the CLI as authoritative if it differs from this layout.

## Trace one task run

Start with the task row in `report.json`, then inspect the matching attempt. A job directory can contain:

```text
result.json                          Final job result
artifacts/model.patch               Patch produced by the agent
verifier/reward.json                Verifier reward
verifier/ctrf.json                  Structured test results
verifier/run.log                    Verifier execution log
verifier/test-stdout.txt            Test output
verifier/reports/                   Baseline and candidate reports or logs
agent/trajectory.json               Main coordinator trajectory/chat history
agent/sessions/**/*.jsonl           Retained provider and child-session histories
agent/agent-layer-dispatch/*.json   Dispatch lifecycle metadata
agent/agent-layer-dispatch/*.stdout Dispatched-agent output
```

`execution-receipt.json` identifies the preserved provider execution. Use it to connect the task attempt to its job artifacts.

Inspect large trajectories selectively with `jq`, `rg`, or targeted reads instead of loading every session at once. A practical order is:

1. Read the task result and verifier reward.
2. Inspect `model.patch` and verifier logs to understand what passed or failed.
3. Read the main trajectory around the relevant decisions or errors.
4. Follow session JSONL and dispatch records when delegated work affected the outcome.

## Explain why results changed

Do not stop after describing a higher or lower score or cost. Compare the affected baseline and candidate runs and find where their behavior first diverged. Trace that difference through the task result, patch, verifier, trajectory, session history, timings, calls, and environment until it explains the measured change.

Keep multiple explanations open until the artifacts distinguish them. For example:

- The changed skill, instructions, agent, or harness caused different reasoning, implementation, delegation, or tool use.
- A task, verifier, benchmark calculation, or cost measurement was broken or misleading.
- A dependency, permission, configuration, environment, or infrastructure difference affected one arm.
- A timeout, retry, partial run, or other execution anomaly changed the recorded score or cost.

These are examples, not categories to constrain the investigation. Consider any explanation consistent with the evidence, including causes not anticipated here, and keep digging until the actual causal chain is understood.

Treat symptoms as leads, not final explanations. For example, “the task timed out” explains the score calculation but not why the timeout happened. Inspect the history and logs to determine whether it came from environment setup, repeated polling, a stuck command, excessive delegation, an agent mistake, or another concrete cause.

Explain the result as a short causal chain:

> The result changed because **[concrete event]**. That happened because **[underlying cause]**. The supporting evidence is **[specific report fields or artifacts]**.

Use ordinary language and distinguish evidence from inference. A better score does not prove the changed system worked as intended, and a worse score does not prove the agent was at fault; the task, benchmark, or measurement may be responsible.

When the runner retains a failed-stage directory, inspect its manifest, logs, execution receipt, and provider artifacts before another paid attempt. If the existing artifacts cannot identify the root cause, say exactly what is missing and what additional observation or rerun would distinguish the remaining explanations.
