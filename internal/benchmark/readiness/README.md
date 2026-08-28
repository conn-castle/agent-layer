# DeepSWE task environment readiness

Each runnable task must provide exactly one contract at:

```text
<DeepSWE commit>/<task>/contract.json
<DeepSWE commit>/<task>/check.sh
```

The contract pins the task's published image by digest. `check.sh` is the
authoritative task-owned setup and readiness program for the agent environment.
It may prepare tools outside `/app`, but it must not modify the repository or
run hidden verifier code. It must fail when any runtime, executable, browser,
configuration, or development prerequisite needed to implement and validate
the task is unavailable.

Before provider execution, the harness runs the program without network access
in the digest-pinned image. A successful result is cached only for the same
DeepSWE commit, task-tree checksum, contract content, and image digest. Pier is
then forced to use that digest and reruns the program inside every actual agent
container before starting the provider. Baseline and treatment therefore share
the same task environment contract and immutable base-image identity.

The certification receipt is durable; the Docker image is not. Public CLI
workflows remove certification-only images by default and can reuse the receipt
without retaining every catalog image.

When an upstream image omits an agent-side system dependency, the contract may
name `agent_image_overlay` and `agent_check` files. The harness fingerprints and
builds that overlay for both arms and runs the additional check only in agent
containers. The verifier continues to use the original digest-pinned image, so
tool provisioning cannot change scoring behavior.

Adding a task to a benchmark plan without this contract is a harness error.
