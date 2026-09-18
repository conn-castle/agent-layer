---
name: benchmark
description: Run A/B tests of skills, instructions, agents, harnesses, and other coding-agent system changes with Agent Layer's DeltaSelect benchmark workflow. Use to select tasks, run or resume a benchmark, interpret score and cost results, or investigate why results changed.
---

# Benchmark

Help the user A/B test a change to an agent system—typically a skill, instructions, agent configuration, or harness behavior, but potentially any deliberate system change. One arm is the baseline and the other contains the change being evaluated. Match the amount of analysis and rigor the user wants; this skill does not prescribe an experimentation method.

A score or cost difference always has a cause. Reporting the difference is not enough: investigate why it changed relative to the baseline and explain the root cause in simple language. Consider the whole system, including the agent, task, verifier, benchmark implementation, dependencies, environment, configuration, and cost accounting. Do not assume the benchmark is correct or stop at a symptom such as “the task failed.” Follow the evidence until the underlying cause is clear. If retained evidence cannot establish it, identify the missing evidence and gather it when possible rather than guessing.

## 1. Obtain a task selection

The benchmark starts with a DeltaSelect `selection.json`. Either route is valid:

- Use JSON supplied by the user.
- Use the [DeltaSelect tool](https://agent-layer.dev/deltaselect-tool) yourself from the user's inputs, or walk the user through it.

In the online tool, choose the model and reasoning effort, budget, repetitions, optional minimum headroom, optional comparison model, and any manual task exclusions. Select **Export benchmark plan**, then copy or download `selection.json`.

If operating the site requires browser automation that is unavailable, ask the user to export the selection. Prefer the tool's exact output because it carries the selected tasks, weights, calibration data, repetitions, and snapshot identity.

Once initialized, the study uses that locked task selection. To benchmark a different selection, initialize another study.

## 2. Initialize and inspect the study

From an initialized Agent Layer project, confirm that Git, Docker, `uvx`, and the selected provider CLI and authentication are available. Save the selection as a JSON file, then run:

```bash
al benchmark init selection.json --directory benchmark-study
```

This creates `benchmark-study/study.toml`, a treatment configuration, prompt, and snapshots of the selection and benchmark inputs. Review `study.toml` and the generated treatment files if the user wants to customize the experiments; otherwise use the generated setup.

Check the installed command surface before relying on less common flags:

```bash
al benchmark --help
al benchmark run --help
```

## 3. Prepare and run

These commands progressively prepare the same study:

```bash
# Optional: isolate environment, image, and task-preparation problems.
al benchmark readiness --study benchmark-study/study.toml

# Validate and prepare without running model inference.
al benchmark run benchmark-study/study.toml --dry-run

# Run missing benchmark cells.
al benchmark run benchmark-study/study.toml
```

A normal run may make paid provider calls. Confirm the intended run and budget with the user before starting it.

Benchmark preparation can take 30 minutes or more when images must be built or pulled, and the study itself can run much longer. Follow the CLI's stage, progress, cost, active-task, timeout, and heartbeat output. Use event-driven waits rather than frequent polling, and keep the user informed during a long run.

Completed cells are retained and reused. If a run stops, rerun the same command with the same `study.toml` to continue the missing work. Use `--task` when supported to limit work to named tasks. If the CLI reports a retained failed-stage directory or an ambiguous paid invocation, inspect it before deciding whether to run anything again.

## 4. View the results

At completion, the CLI prints the canonical report JSON path. The HTML report is next to it:

```text
.../report/report.json
.../report/report.html
```

Open `report.html` for the score-versus-cost chart, experiment summaries, comparisons, per-task evidence, warnings, statistical notes, limitations, and reproduction details. Use `report.json` for exact values or programmatic inspection.

Read [references/results-and-artifacts.md](references/results-and-artifacts.md) when interpreting the report or investigating why a result occurred.

## 5. Report back

Tell the user:

- where the HTML and JSON reports are;
- whether all expected runs completed and which cells are missing or failed;
- the observed score and cost results, with any warnings shown by the report;
- the evidence-backed root cause of important score and cost changes; and
- what remains uncertain, without imposing a decision rule the user did not request.
