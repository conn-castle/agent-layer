# DeepSWE Benchmark Guide

Agent Layer can compare a bare coding agent with the same agent using your project's Agent Layer instructions and skills. The CLI handles study scaffolding, task readiness, Docker capacity checks, safe concurrency, image cleanup, resumable execution, and report generation.

## Before you start

You need:

- an initialized Agent Layer project;
- Git, Docker, and `uvx` on `PATH`;
- the provider CLI and authentication required by the selected model;
- a `selection.json` exported from the [DeepSWE benchmark planner](https://agent-layer.dev/deepswe-planner).

Run all commands from the project root.

## Recommended workflow

### 1. Create the study

```bash
al benchmark init selection.json --directory benchmark-study
```

This creates an otherwise-empty `benchmark-study/` directory containing:

- `selection.json`, copied without changing the selected tasks;
- `study.toml`, with bare and Agent Layer experiments using the selection's model and reasoning;
- `treatment/config.toml`, a minimal benchmark-safe config for the selected provider;
- `treatment/instructions/`, copied from `.agent-layer/instructions/`;
- `treatment/skills/`, copied from the current `.agents/skills/` projection;
- `treatment/prompt.md`, with the required `{{task}}` placeholder.

The generated config deliberately excludes host-only settings such as status lines. The command refuses to overwrite a non-empty destination directory.

### 2. Certify only this study's tasks

```bash
al benchmark readiness --study benchmark-study/study.toml
```

Readiness performs static task validation and Docker environment certification without invoking a provider model. Successful certifications are stored as content-addressed receipts, so later commands can reuse them even after task images are removed.

This step is optional because `benchmark run` performs the same required task preparation. Running it separately is useful when you want to isolate Docker or task-environment problems before provider authentication and paid execution.

### 3. Validate the complete study

```bash
al benchmark run benchmark-study/study.toml --dry-run
```

The dry run validates and snapshots all declared inputs, checks tools and provider authentication, prepares the treatment, certifies missing task environments, and reports cached versus missing cells. It never performs provider inference.

### 4. Run or resume

```bash
al benchmark run benchmark-study/study.toml
```

This command authorizes paid provider calls for missing cells. Completed cells are immutable and reused on the next invocation, so rerunning the same command safely resumes interrupted work. When the study completes, the CLI prints the canonical JSON report path; an HTML report is generated beside it.

## Safety defaults

The ordinary workflow requires no tuning:

- Provider cells run serially. This avoids multiplying provider rate-limit pressure and lets the CLI reclaim each task image after its durable result is written.
- Readiness uses one worker when automatic image reclamation is enabled.
- Before pulling, the CLI checks Docker storage using a conservative 4 GiB budget per simultaneously retained task image. If the plan cannot fit, it stops with required-versus-available capacity instead of filling Docker's disk.
- Certification-only images are removed automatically; durable readiness receipts remain.
- Readiness prints task percentages. Study runs print preparation stages, cell percentages, cumulative observed cost, and a heartbeat every 30 seconds during long operations.
- Docker disk exhaustion is identified explicitly in both the task result and final error.

## Advanced controls

Use these only when you have a specific reason:

```bash
# Certify selected catalog tasks without a study manifest.
al benchmark readiness --task first-task --task second-task

# Retain readiness images. Capacity preflight scales with every selected task.
al benchmark readiness --study benchmark-study/study.toml --remove-task-images=false

# Run only one task from the immutable study membership.
al benchmark run benchmark-study/study.toml --task first-task

# Override the automatic worker choice.
al benchmark run benchmark-study/study.toml --task-concurrency 2
```

`--task` and worker count affect only the current invocation; they do not change study identity or report membership. A run concurrency greater than one is an explicit throughput override and disables per-cell task-image reclamation because concurrent cells may still be using the same image.

Readiness also supports deterministic sharding and per-task timeouts:

```bash
al benchmark readiness \
  --study benchmark-study/study.toml \
  --task-shard-index 1 \
  --task-shard-count 4 \
  --task-timeout 10m
```

Do not combine `--study` with `--task`. Use `al benchmark <command> --help` for the complete current flag list.

## Common failures

### Insufficient Docker disk

The error reports estimated required capacity and detected Docker capacity before pulling. Keep automatic image removal enabled, reduce the study/task scope, or free/increase Docker storage.

### Provider authentication fails during a dry run

Dry runs verify authentication because a successful dry run is meant to prove that paid execution can start. Fix the provider's repo-local authentication, then rerun the same command.

### Study directory already contains files

Choose a new `--directory` or deliberately move the existing study. `benchmark init` does not merge with or overwrite an existing non-empty directory.

### A task fails readiness

The task line distinguishes a task-specific failure from Docker capacity, registry limits, or timeout failures. Fix the named cause and rerun; already certified tasks reuse their receipts.

## Reproducibility boundary

`study.toml` and every path it references must remain within the study directory. The runner snapshots those bytes before staging or execution, then content-addresses the selection, experiments, task trees, certified environments, treatment bundle, and evidence. Editing the study or its declared inputs intentionally creates a different experiment identity rather than mutating prior results.
