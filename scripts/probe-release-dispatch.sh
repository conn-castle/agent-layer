#!/usr/bin/env bash
# Run parallel live Agent Dispatch compatibility probes (fresh start + continue)
# for every locally enabled, available provider using a scratch candidate build,
# then restore normal local projections and print structured evidence.
set -euo pipefail

die() {
  echo "ERROR: $*" >&2
  exit 1
}

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "${root_dir}"

command -v python3 >/dev/null 2>&1 || die "python3 is required"
command -v make >/dev/null 2>&1 || die "make is required"

probe_bin_dir="$(mktemp -d "${TMPDIR:-/tmp}/al-release-probe.XXXXXX")" || exit 1
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/al-release-probe-work.XXXXXX")" || {
  rm -rf -- "${probe_bin_dir}"
  exit 1
}

echo "Building scratch release probe executable in ${probe_bin_dir}..."
if ! make al-dev-build AL_DEV_BIN_DIR="${probe_bin_dir}"; then
  echo "Build failed; scratch directory retained: ${probe_bin_dir}" >&2
  rm -rf -- "${work_dir}"
  exit 1
fi

probe_bin="${probe_bin_dir}/al"
if command -v sha256sum >/dev/null 2>&1; then
  probe_sha="$(sha256sum "${probe_bin}" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  probe_sha="$(shasum -a 256 "${probe_bin}" | awk '{print $1}')"
else
  probe_sha="unavailable"
fi

echo "Release probe executable: ${probe_bin} (sha256: ${probe_sha})"
echo "Checkout commit: $(git rev-parse HEAD)"

# Record installed provider CLI versions.
versions_file="${work_dir}/versions.txt"
: > "${versions_file}"
for cli in claude codex agy grok muse; do
  if command -v "${cli}" >/dev/null 2>&1; then
    ver="$("${cli}" --version 2>&1 | head -n 1 || true)"
  else
    ver="not installed"
  fi
  printf '%s: %s\n' "${cli}" "${ver}" >> "${versions_file}"
done

probe_status=0
(
  unset AL_RUN_DIR AL_RUN_ID AL_DISPATCH_CALLER_AGENT AL_DISPATCH_ACTIVE AL_DISPATCH_MAX_DEPTH AL_SHIM_ACTIVE
  unset AL_INSTALLED_EXECUTABLE
  unset CODEX_HOME CLAUDE_CONFIG_DIR AGY_CLI_DISABLE_AUTO_UPDATE GROK_HOME
  export PATH="${probe_bin_dir}:${PATH}"
  export AL_DEV_BYPASS_VERSION_DISPATCH=1
  export AL_DEV_EXECUTABLE="${probe_bin}"
  export AL_DEV_SOURCE_ROOT="${root_dir}"

  options="$(al dispatch options)" || exit 20
  printf '%s\n' "${options}" > "${work_dir}/options.json"

  providers=()
  while IFS= read -r provider; do
    [[ -n "${provider}" ]] && providers+=("${provider}")
  done < <(python3 -c 'import json, sys
for agent in json.load(sys.stdin)["agents"]:
    if agent["available"]:
        print(agent["agent"])' < "${work_dir}/options.json")

  if [[ "${#providers[@]}" -eq 0 ]]; then
    echo "No available providers to probe; retain scratch until projections are restored." >&2
    exit 20
  fi

  # 0 = completed, 10 = confirmed provider failure, 20 = unsafe to proceed.
  wait_for_termination() {
    local handle="$1" out_file="$2" result
    result="$(al dispatch wait "${handle}" --condition termination_confirmed)" || return 20
    printf '%s\n' "${result}" > "${out_file}"
    python3 -c 'import json, sys
result = json.load(sys.stdin)
confirmed = result.get("condition_met") is True and result.get("termination_confirmed") is True
state = result.get("state")
sys.exit(0 if confirmed and state == "completed" else 10 if confirmed and state in ("failed", "cancelled", "interrupted") else 20)' < "${out_file}"
  }

  probe_provider() {
    local provider="$1"
    local pdir="${work_dir}/provider-${provider}"
    mkdir -p "${pdir}"

    local started handle status
    started="$(al dispatch start --agent "${provider}" --prompt 'Reply with exactly release-probe-fresh. Do not use tools or modify files.')" || return 20
    printf '%s\n' "${started}" > "${pdir}/start.json"

    handle="$(python3 -c 'import json, sys
handle = json.load(sys.stdin)["handle"]
if not isinstance(handle, str) or not handle:
    raise ValueError("missing dispatch handle")
print(handle)' < "${pdir}/start.json")" || return 20

    status=0
    wait_for_termination "${handle}" "${pdir}/wait-fresh.json" || status=$?
    if [[ "${status}" -ne 0 ]]; then
      printf '%s\n' "${status}" > "${pdir}/status"
      return "${status}"
    fi

    local continued
    continued="$(al dispatch continue "${handle}" --prompt 'Reply with exactly release-probe-continue. Do not use tools or modify files.')" || return 20
    printf '%s\n' "${continued}" > "${pdir}/continue.json"

    status=0
    wait_for_termination "${handle}" "${pdir}/wait-continue.json" || status=$?
    printf '%s\n' "${status}" > "${pdir}/status"
    return "${status}"
  }

  echo "Probing ${#providers[@]} enabled providers in parallel: ${providers[*]}"
  pids=()
  for provider in "${providers[@]}"; do
    probe_provider "${provider}" > "${work_dir}/provider-${provider}.log" 2>&1 &
    pids+=("$!")
  done

  probe_failed=0
  unsafe_failure=0
  for i in "${!pids[@]}"; do
    p_rc=0
    wait "${pids[$i]}" || p_rc=$?
    provider="${providers[$i]}"
    case "${p_rc}" in
      0)
        echo "  [OK] ${provider}: fresh + continue completed with termination confirmed"
        ;;
      10)
        echo "  [FAILED] ${provider}: confirmed provider failure" >&2
        probe_failed=1
        ;;
      *)
        echo "  [UNSAFE] ${provider}: CLI/response/termination error (rc=${p_rc})" >&2
        cat "${work_dir}/provider-${provider}.log" >&2 || true
        unsafe_failure=1
        ;;
    esac
  done

  if [[ "${unsafe_failure}" -ne 0 ]]; then
    exit 20
  fi
  exit "${probe_failed}"
) || probe_status=$?

# Only success or confirmed failures permit projection restoration and scratch cleanup.
if [[ "${probe_status}" -ne 0 && "${probe_status}" -ne 1 ]]; then
  echo "Unsafe probe stop (exit ${probe_status}); scratch retained at ${probe_bin_dir} and work at ${work_dir}" >&2
  echo "Confirm termination of all started invocations, run 'make al-sync', then remove scratch directories." >&2
  exit "${probe_status}"
fi

echo "Restoring local projections with make al-sync..."
if ! make al-sync; then
  echo "Projection restoration failed; scratch retained at ${probe_bin_dir}" >&2
  exit 30
fi
rm -rf -- "${probe_bin_dir}"

# Validate all probe artifacts against docs/RELEASE.md criteria and print markdown summary.
eval_status=0
python3 - "${root_dir}" "${work_dir}" "${probe_sha}" <<'PY' || eval_status=$?
import json, pathlib, sys

root = pathlib.Path(sys.argv[1])
work = pathlib.Path(sys.argv[2])
probe_sha = sys.argv[3]
options = json.loads((work / "options.json").read_text())
versions = (work / "versions.txt").read_text().strip()

def load_dispatch_record(inv_id):
    for base in (root / ".agent-layer/state/runs", root / ".agent-layer/tmp/runs"):
        p = base / inv_id / "dispatch.json"
        if p.exists():
            return json.loads(p.read_text()), base / inv_id
    return None, None

def load_result_text(run_dir):
    if not run_dir:
        return ""
    p = run_dir / "result.md"
    return p.read_text().strip() if p.exists() else ""

print("\n=== Agent Dispatch Release Probe Evidence ===")
print(f"- Executable SHA-256: `{probe_sha}`")
print("```text")
print(versions)
print("```")
print("| Provider | Configured target | Fresh invocation | Continue invocation | Provider session ID | Result |")
print("| --- | --- | --- | --- | --- | --- |")

any_failed = False
for agent in options["agents"]:
    name = agent["agent"]
    model = (agent.get("model") or {}).get("configured") or "default"
    effort = (agent.get("reasoning_effort") or {}).get("configured") or "-"
    target = f"`{model}` / `{effort}`" if effort != "-" else f"`{model}`"
    if not agent.get("available"):
        reason = agent.get("unavailable_reason") or "unavailable"
        print(f"| `{name}` | {target} | untested | untested | — | untested ({reason}) |")
        continue

    pdir = work / f"provider-{name}"
    start_j = json.loads((pdir / "start.json").read_text())
    fresh_wait = json.loads((pdir / "wait-fresh.json").read_text())
    fresh_id = start_j["invocation_id"]
    fresh_rec, fresh_dir = load_dispatch_record(fresh_id)
    fresh_sid = (fresh_rec or {}).get("provider_session_id", "")
    fresh_ans = load_result_text(fresh_dir)

    cont_path = pdir / "continue.json"
    cont_wait_path = pdir / "wait-continue.json"
    cont_id = "—"
    cont_sid = ""
    cont_ans = ""
    cont_ok = False
    if cont_path.exists() and cont_wait_path.exists():
        cont_j = json.loads(cont_path.read_text())
        cont_wait = json.loads(cont_wait_path.read_text())
        cont_id = cont_j["invocation_id"]
        cont_rec, cont_dir = load_dispatch_record(cont_id)
        cont_sid = (cont_rec or {}).get("provider_session_id", "")
        cont_ans = load_result_text(cont_dir)
        cont_ok = (
            cont_wait.get("condition_met") is True
            and cont_wait.get("termination_confirmed") is True
            and cont_wait.get("state") == "completed"
            and "release-probe-continue" in cont_ans
        )

    fresh_ok = (
        fresh_wait.get("condition_met") is True
        and fresh_wait.get("termination_confirmed") is True
        and fresh_wait.get("state") == "completed"
        and "release-probe-fresh" in fresh_ans
    )
    sid_ok = bool(fresh_sid) and fresh_sid == cont_sid

    extra_err = ""
    if name == "antigravity" and fresh_ok and cont_ok and sid_ok:
        # Verify structured ACP terminal evidence and usage exist in events.jsonl.
        for label, rdir in (("fresh", fresh_dir), ("continue", cont_dir)):
            ev_path = rdir / "events.jsonl" if rdir else None
            if not ev_path or not ev_path.exists():
                extra_err = f"missing {label} events.jsonl"
                break
            ev_text = ev_path.read_text()
            if fresh_sid not in ev_text or '"usage"' not in ev_text:
                extra_err = f"missing structured ACP session/usage in {label} events"
                break

    if fresh_ok and cont_ok and sid_ok and not extra_err:
        status_str = f"PASS (`{fresh_ans}` / `{cont_ans}`)"
    else:
        any_failed = True
        reasons = []
        if not fresh_ok:
            reasons.append(f"fresh={fresh_wait.get('state')}")
        if not cont_ok:
            reasons.append("continue failed/missing")
        if not sid_ok:
            reasons.append(f"session mismatch ({fresh_sid!r} != {cont_sid!r})")
        if extra_err:
            reasons.append(extra_err)
        status_str = "FAIL (" + "; ".join(reasons) + ")"

    print(f"| `{name}` | {target} | `{fresh_id}` | `{cont_id}` | `{fresh_sid or '—'}` | {status_str} |")

if any_failed:
    sys.exit(1)
PY

rm -rf -- "${work_dir}"
if [[ "${probe_status}" -ne 0 || "${eval_status}" -ne 0 ]]; then
  exit 1
fi
