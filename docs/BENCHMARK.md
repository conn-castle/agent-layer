# DeltaSelect Benchmark Guide

Agent Layer can compare a bare coding agent with the same agent using your project's Agent Layer instructions and skills. The CLI handles study scaffolding, task readiness, Docker capacity checks, safe concurrency, image cleanup, resumable execution, and report generation.

## Before you start

You need:

- an initialized Agent Layer project;
- Git, Docker, and `uvx` on `PATH`;
- the provider CLI and authentication required by the selected model;
- a `selection.json` exported from [DeltaSelect](https://agent-layer.dev/deltaselect-tool).

Run all commands from the project root.

New experiment model identities may use `<provider>/<model>` (for example,
`codex/<native-model-id>`), with `reasoning` supplied separately. The provider
is `claude`, `codex`, `grok`, or `antigravity`. This avoids a maintained runtime
model allowlist. Historical short names remain readable solely to preserve old
study identities; parsing an identity does not verify current model support.
Published comparison evidence must still correspond to the selected identity.

Antigravity and Grok runtime preflights capture model lists from the pinned
harness inside the benchmark container. The host validates those bytes using
the same Go parsers as Wizard, Doctor, and Dispatch, including authentication
failure detection. Container transport does not maintain a separate model
parser or fallback list. These checks run during runtime preflight, not on
every paid task invocation.

## Recommended workflow

### 1. Create the study

```bash
al benchmark init selection.json --directory benchmark-study
```

This creates an otherwise-empty `benchmark-study/` directory containing:

- `selection.json`, copied without changing the selected tasks;
- `study.toml`, with bare and Agent Layer experiments using the selection's model and reasoning;
- `treatment/config.toml`, a minimal benchmark-safe config for the selected provider;
- `treatment/official-instructions/`, containing core rules fetched at the same Git commit as development skills;
- `treatment/official-skills/`, containing the embedded workflow skills;
- `treatment/project-instructions/` and `treatment/project-skills/`, preserved only as unreferenced audit snapshots (both configured instruction tiers, with numeric order in `project-instructions-order.json`);
- `treatment/prompt.md`, with the required `{{task}}` placeholder.

The existing `skills-source.json` provenance records the instruction source path and hash alongside that same commit. The generated Agent Layer experiment pins the benchmark-safe core rules and official workflow skills fetched at one frozen Git commit, while preserving the project's instructions and installed skills separately as unreferenced audit snapshots. It requires one independent plan review, one dispatched implementation, and one independent code review. Its runtime instruction supplies the selected provider/model/reasoning target under the exact `plan_reviewers`, `implementer`, and `code_reviewer` names required by the implementation skill, plus the exact `role` value each `dispatch_start` call must record. Prompt text alone is not role evidence. A direct single-agent implementation is workflow-noncompliant. The generated config deliberately excludes host-only settings such as status lines. The command refuses to overwrite a non-empty destination directory.

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

The dry run validates and snapshots all declared inputs, checks tools and provider authentication, prepares the treatment, certifies missing task environments, and reports cached versus missing cells. Successful task-and-experiment runtime preflights are stored as content-addressed receipts and reused by later identical dry runs. It never performs provider inference.

### 4. Run or resume

```bash
al benchmark run benchmark-study/study.toml
```

This command authorizes paid provider calls for missing cells. Completed cells are immutable and reused on the next invocation, so rerunning the same command safely resumes interrupted work. When the study completes, the CLI prints the canonical JSON report path; an HTML report is generated beside it.

## Safety defaults

The ordinary workflow requires no tuning:

- Provider cells run serially. This avoids multiplying provider rate-limit pressure and lets the CLI retain one task image across its arms and repetitions, then reclaim it once the task's durable results are written.
- Readiness uses one worker when automatic image reclamation is enabled.
- Before pulling, the CLI checks Docker storage using a conservative 4 GiB budget per simultaneously retained task image. If the plan cannot fit, it stops with required-versus-available capacity instead of filling Docker's disk.
- Certification-only images are removed automatically; durable readiness receipts remain.
- Successful runtime preflight receipts are recorded after any required task-image reclamation succeeds and reused when the Docker host architecture, pinned Pier version, task environment, provider client, adapter, model, reasoning, and treatment inputs still match. Authentication is checked on every invocation.
- DeepSWE task containers are certified for `linux/amd64`. On a non-amd64 host, the CLI warns that Docker must emulate them and that initial pulls, builds, and preflights can take 30 minutes or longer.
- Grok runs inside Pier's disposable task container with Grok's built-in `devbox` sandbox profile, avoiding a host Bubblewrap dependency while retaining the container boundary. Dry-run and paid command construction use the same profile. Before each paid cell, Agent Layer requires the canonical repo-local OAuth credential to retain more than 30 minutes of lifetime; an expiring credential pauses before inference with an actionable login command instead of failing after paid work starts.
- Before Pier starts a paid cell, Agent Layer records its restricted staging directory in durable study state. Pier places provider logs and `model.patch` in that host directory before starting a separate verifier. If verification, cleanup, or artifact promotion fails, the CLI retains and prints the staging path and refuses another provider call for that cell.
- When the checkpoint proves that provider execution completed cleanly and failure began in the verifier, a later identical run replays only the verifier from the retained `model.patch`; the original provider event remains the canonical cost, duration, and stream evidence, and its receipt records verifier-replay provenance. A timeout is finalized as an explicit zero-score `test_timeout` outcome when retained evidence proves Pier was executing the task-owned verifier test script or when preserved structured framework output proves that the candidate suite exhausted its own test timeout. Once the task-owned script starts, its setup, base-test, and candidate-test work share the task's test-execution timeout attribution. The generic `VerifierTimeoutError` is insufficient because it also covers environment setup and artifact transfer. Setup, build, interrupted, and otherwise unclassified verifier failures keep the checkpoint and retained stage for verifier-only replay instead of becoming a failed receipt. Agent-phase failures are never eligible for verifier replay. Go verifier contexts also preserve their unfiltered structured build events before reporter compatibility filters, so compiler diagnostics remain actionable without changing reporter input.
- The byte-exact provider patch is preserved at `<stage>/agent-layer-replay/model.patch` before any sanitization pass. Sanitization rewrites only evidence copies, replay applies the exact copy, and that replay input is never promoted into study evidence. Provider completion is proven from the final stage after Pier exits; the live progress watcher only reports phases.
- A cell failure stops assignment of new paid work, but it does not cancel sibling cells that were already running. Those cells finish and persist their evidence before the invocation reports the failure.
- Readiness prints task percentages. Study runs print preparation stages, task/arm preflight and cell percentages, cumulative observed cost, and the active environment/provider/verifier phase with its configured timeout budget. The pinned Pier adapter permits one verifier attempt: verifier environment startup and tests share that task-declared timeout, so a deterministic candidate timeout is not run twice. Infrastructure failures remain eligible for a later verifier-only replay, while evidence-backed candidate test timeouts finalize at zero. Pier cleanup overhead is not a bounded task timeout, so the CLI does not mislabel this budget as a hard deadline or ETA. During a silent long-running operation, the CLI prints one heartbeat after 60 seconds of inactivity and at most once per additional silent minute; heartbeats include phase elapsed time, and genuine progress resets the timer.
- Before paid inference, every experiment prints its effective workflow. Constrained skills experiments list each mandatory role and exact provider/model/reasoning target. An explicitly unconstrained custom skills study is labeled `single-agent execution is allowed`; the official scaffold never creates that contract.
- Cancellation allows Pier two minutes to run its shielded teardown. On Linux, fallback cleanup repairs `/logs` ownership for each attributed container before removing it, through `docker exec` for a running container and through a temporary root container that mounts the stopped container's volumes for a stopped one, so verifier-owned files remain readable for sanitization and promotion.
- Docker disk exhaustion is identified explicitly in both the task result and final error.

## Advanced controls

If an interrupted event cannot prove clean provider completion (for example, its staging directory is missing or incomplete), Agent Layer fails closed and prints the checkpoint and staging path. After archiving that evidence, an operator who deliberately authorizes a new paid call can remove only that event's `execution-checkpoint.json`; do not remove another event directory or the study root. The next run then follows the ordinary immutable-receipt rules.

Use these only when you have a specific reason:

```bash
# Certify selected catalog tasks without a study manifest.
al benchmark readiness --task first-task --task second-task

# Retain readiness images. Capacity preflight scales with every selected task.
al benchmark readiness --study benchmark-study/study.toml --remove-task-images=false

# Run only one task from the immutable study membership.
al benchmark run benchmark-study/study.toml --task first-task

# Canonicalize retained terminal test timeouts or regenerate a completed report
# without provider or verifier execution.
al benchmark run benchmark-study/study.toml --recover-only

# Override the automatic worker choice.
al benchmark run benchmark-study/study.toml --task-concurrency 2
```

`--recover-only` first inspects retained checkpoints and finalizes only evidence-backed terminal candidate test timeouts; it skips every ordinary missing cell and every replayable infrastructure/interrupted verifier state. It can also regenerate a report for a compatible completed study from that study's immutable manifest, treatment pins, and cell evidence without restaging current treatment inputs. If neither kind of compatible retained study exists, the command fails instead of certifying task images or creating a new study. `--task` and worker count affect only the current invocation; they do not change study identity or report membership. A run concurrency greater than one is an explicit throughput override and disables task-image reclamation because concurrent cells may still be using the same image.

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

Treatment bundles exclude project memory and personal context. Declared benchmark instructions must therefore be self-contained; instructions that reference excluded project-state documents such as `CONTEXT.md`, `DECISIONS.md`, or `MEMORY.md`, or paths beneath `guides/`, fail before provider execution instead of producing treatment-only failed reads.

## Release catalog registry access

Push-triggered catalog certification keeps one active run per branch and cancels
superseded main-commit push runs. Scheduled and manually dispatched runs each use
a unique run-ID concurrency group, preserving explicit certification requests
when later pushes or requests arrive. Scheduled runs and dispatches with
`force=true` still run the full catalog, even if a later push changes no
catalog-critical paths. Each run retains all sixteen shards with at most four
running concurrently; independent runs can overlap. Certification still belongs
to the exact commit that was checked out, so cancelled or different-commit runs
cannot satisfy a release gate.

For ordinary pushes and non-forced dispatches, classification first looks for a
full run created within seven days on this repository's main branch. Only this
workflow's push, schedule, or manual runs qualify, and all sixteen readiness
shards must have actually succeeded. Successful jobs from earlier failed-job
rerun attempts count, but rerunning does not extend the original creation-time
expiry. The source must be an ancestor with unchanged catalog inputs. A newer
matching failed or timed-out shard prevents reuse of an older success, as
does a cancelled shard unless its whole run was cancelled.
Re-running all jobs always requires full certification so reclassification
cannot hide that run's own earlier failure. Skipped classification
runs cannot become proof or extend its lifetime. Missing, expired, malformed,
or unavailable GitHub evidence requires a fresh full run. The search is bounded
to the latest 100 completed workflow runs and 100 jobs per run; incomplete job
responses also require a full run.

The explicit readiness input boundary includes non-test files in
`internal/benchmark/`, `internal/gitenv/`, and `internal/fsutil/`, plus
`cmd/al/benchmark.go`, `go.mod`, `go.sum`, `.agent-layer/repo-profile.toml`,
this workflow, and both
`scripts/catalog-certification-scope.*` helpers. Go `*_test.go` files and
`testdata/` are excluded. Renames, deletions, and executable-mode changes count.
This explicit boundary focuses on catalog readiness logic and setup inputs;
it does not fingerprint every transitive application dependency. Weekly full
runs detect external image and runner drift. The exact candidate
commit still receives a workflow result whose summary links the original full
run and reports its age. Release gates continue to require that candidate's
successful workflow result.

Reuse needs an initial successful full run for the current inputs. To establish
one without configuring AWS access, dispatch a manual full run on main:

```bash
gh workflow run release-catalog-certification.yml --ref main -f force=true
```

Avoid pushing to main during bootstrap: each push can start competing downloads
until a successful full result exists. Inspect all failed shard logs. If every failed shard is blocked solely by ECR
quota, use `gh run rerun <run-id> --failed` at most three times, checking the logs
after each attempt. Successful shards carry forward; use the same manual run,
whose concurrency group is independent of later pushes. Stop on a real task
failure or after the retry limit. Quota availability can still prevent initial
or weekly certification; reuse reduces repeated downloads but does not replenish
AWS's shared allowance. After a successful full run, a manual dispatch with
`force=false` on the same commit can verify reuse in the classification summary.

The observed `toomanyrequests: Data limit exceeded` response is a data allowance
failure, not evidence that a task failed. Anonymous ECR Public downloads share
an allowance by source IP; slower scheduling cannot replenish an exhausted
allowance. See [AWS's ECR pricing and quota explanation](https://aws.amazon.com/ecr/pricing/).

To use authenticated pulls, configure the repository variable
`ECR_PUBLIC_ROLE_ARN` with an IAM role trusted for GitHub OIDC from
`repo:conn-castle/agent-layer:ref:refs/heads/main` and audience `sts.amazonaws.com`.
Its pull-token permissions are `ecr-public:GetAuthorizationToken` and
`sts:GetServiceBearerToken` (resource `*`; the latter can be restricted to service
`ecr-public.amazonaws.com`). The workflow obtains temporary credentials in
`us-east-1`, logs Docker into ECR Public, and removes the Docker login at job end.
AWS credential variables are unset before task readiness execution. See
[AWS's public registry authentication contract](https://docs.aws.amazon.com/AmazonECR/latest/public/public-registry-auth.html).

Without that variable, the workflow explicitly reports anonymous access and its
source-IP quota. A configured role/authentication failure stops its shard rather
than silently switching back to anonymous pulls. AWS account quotas and data
transfer billing still apply; configure the role only for an approved account.
The PR does not create AWS resources or set repository variables, and cannot
claim that a shared anonymous quota has been repaired by scheduling alone.
