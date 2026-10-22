# Agent Dispatch v1.1.0 compatibility evidence

Probes ran 2026-10-22 UTC on Linux/amd64 at clean commit `8db868b761f67beeea3a6c8f899fee8e27fc225c`, built with Go 1.27.3. The source development executable SHA256 was `c9dfa3108479238efde201be8f2b0245892f975b982658876f5433e71a70939c`.

Subsequent release-preparation changes update documentation and versioned template ownership/migration manifests; dispatch implementation, dependencies, and configured probe targets are unchanged. The final release commit must separately pass the release artifact preflight and exact-commit hosted certification.

## Setup

All five locally enabled providers used the existing release checkout's `.agent-layer/config.toml`, `.agent-layer/.env`, normal local provider homes and sign-ins, configured models/efforts, and permissions. No configuration, credentials, or sign-in was changed to make a probe pass. The CLI was built with `make al-dev-build`; source selection and environment setup followed `docs/RELEASE.md`.

Each fresh prompt requested `release-probe-fresh`, and each continuation requested `release-probe-continue`, with no tools or file modifications. No failed probe was repeated. All ten invocations have durable termination confirmation.

| Provider | Recorded CLI version | Effective target | Result |
| --- | --- | --- | --- |
| antigravity | `1.4.4` | `gemini-4-argon-medium` / `medium` | Fresh passed; continuation failed on explicit individual quota, reset reported in 44h48m31s. Both invocations terminated. |
| claude | `2.1.305` | `claude-opus-5-5` / `high` | Fresh and continuation passed; same provider session and confirmed termination. |
| codex | `0.163.0` | `gpt-6.1-sol` / `high` | Fresh and continuation passed; same provider session and confirmed termination. |
| grok | `1.0.25` | `grok-4.7` / `xhigh` | Fresh and continuation passed; same provider session and confirmed termination. |
| muse | `1.4.11` | `muse-wonder-1.0` / `high` | Fresh and continuation passed; same provider session and confirmed termination. |

## Invocation evidence

### antigravity

- fresh: handle `bulky-bold-transformer`, invocation `024110e9-c417-43c9-9d54-e4c4a31d2d46`, provider session `19dfbce1-b3b2-4976-b66d-174cf6775614`, answer `release-probe-fresh` followed by a newline.
- continuation: invocation `1e705b17-e9e4-49ad-9161-6a905d1c39e6`; failed with explicit individual quota exhaustion and confirmed termination. No successful final answer was published.

### claude

- fresh: handle `huge-sleek-transformer`, invocation `205b5bdd-d041-4eec-9918-b1f161064af5`, provider session `e700348b-9c67-4857-88e7-4daf988380b9`, answer `release-probe-fresh`.
- continue: handle `huge-sleek-transformer`, invocation `7c3cd97c-d9d7-4efe-b598-7a8f4e82ba79`, provider session `e700348b-9c67-4857-88e7-4daf988380b9`, answer `release-probe-continue`.

### codex

- fresh: handle `broad-scarlet-zener`, invocation `dfc8b9e9-28a2-4b31-85a4-6a3e68f22b39`, provider session `01a16199-1886-74a0-a776-83d7d38a9a3e`, answer `release-probe-fresh`.
- continue: handle `broad-scarlet-zener`, invocation `a2c2cff5-c2d4-430e-94f7-c4c1e548e3a4`, provider session `01a16199-1886-74a0-a776-83d7d38a9a3e`, answer `release-probe-continue`.

### grok

- fresh: handle `narrow-quirky-microphone`, invocation `cd2f035e-7a52-4693-ba89-77da60328e63`, provider session `f3983773-5c10-4958-a67f-fe05d67b309e`, answer `release-probe-fresh`.
- continue: handle `narrow-quirky-microphone`, invocation `d3770ad1-84ff-4174-96cb-a450619dbeb2`, provider session `f3983773-5c10-4958-a67f-fe05d67b309e`, answer `release-probe-continue`.

### muse

- fresh: handle `compact-tapered-inverter`, invocation `dd647571-a01e-4e9b-83e0-55eeb09a3e65`, provider session `317d055a-db89-4384-96a5-f33f3cefb103`, answer `release-probe-fresh`.
- continue: handle `compact-tapered-inverter`, invocation `41724e1b-821c-4e2c-a538-71516021717d`, provider session `317d055a-db89-4384-96a5-f33f3cefb103`, answer `release-probe-continue`.

## Exact installed CLI version output

- `claude --version`: `2.1.305 (Claude Code)`.
- `codex --version`: `codex-cli 0.163.0`.
- `agy --version`: `1.4.4`.
- `grok --version`: `grok 1.0.25 (f7e67d6988e2) [stable]`.
- `muse --version`: `Muse Code 1.4.11 (1.4.11-R6667.1)`.

## Limitations and known issues

Antigravity continuation remains unverified for this candidate because its quota was exhausted. Its fresh structured result included conversation identity, answer, and usage; the unsuccessful continuation was rejected with its quota diagnostic rather than exposed as successful output. This is a provider availability limit, not passing continuation compatibility evidence.

Current Grok model discovery and both generation probes passed, but these checks do not establish expired-credential refresh behavior; [#421](https://github.com/conn-castle/agent-layer/issues/421) remains open. None of these short probes establishes the cause of prior unclassified shutdown failures in [#430](https://github.com/conn-castle/agent-layer/issues/430), or resolves anonymous registry availability in [#427](https://github.com/conn-castle/agent-layer/issues/427).

Raw probe reports and CLI output are retained locally in `.agent-layer/tmp/release-ready.tOOykAYc/`; invocation records live under `.agent-layer/tmp/runs/<invocation_id>/`. These are local evidence locators, not uploaded artifacts. No signing, notarization, final-commit catalog certification, or publication is claimed by these provider probes.
