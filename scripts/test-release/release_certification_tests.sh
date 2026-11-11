run_release_certification_script_tests() {
  section "Release Certification Helper Tests"

  local fixture mock_bin log head_sha scenario
  fixture="$(mktemp -d)"
  mock_bin="$fixture/bin"
  log="$fixture/gh.log"
  head_sha="1111111111111111111111111111111111111111"
  mkdir -p "$fixture/scripts" "$mock_bin"
  cp "$ROOT_DIR/scripts/certify-release.sh" "$fixture/scripts/"

  cat >"$mock_bin/git" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  "branch --show-current") echo main ;;
  "status --porcelain") if [[ "${TEST_SCENARIO}" == rehearsal-changed && -f "${TEST_FIXTURE}/rehearsal.seen" ]]; then echo ' M release-source'; fi ;;
  "rev-parse HEAD") echo "${TEST_HEAD_SHA}" ;;
  "ls-remote --exit-code origin refs/heads/main") printf '%s\trefs/heads/main\n' "${TEST_REMOTE_SHA}" ;;
  *) echo "unexpected git invocation: $*" >&2; exit 2 ;;
esac
EOF
  cat >"$mock_bin/sleep" <<'EOF'
#!/usr/bin/env bash
printf 'sleep %s\n' "$*" >>"${TEST_GH_LOG}"
EOF
  # Exact-commit run lists come from TEST_SCENARIO and are filtered through the
  # script's own --jq expression, so the run-state classification is exercised.
  cat >"$mock_bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${TEST_GH_LOG}"
next_count() {
  local file="${TEST_FIXTURE}/$1.count" count=0
  [[ -f "${file}" ]] && count="$(<"${file}")"
  count=$((count + 1))
  printf '%s' "${count}" >"${file}"
  echo "${count}"
}
case "$*" in
  "run list "*"--json databaseId,status,conclusion"*)
    count="$(next_count state)"
    workflow="" branch="" commit="" event="" limit=20 fields="" jq_expr=""
    shift 2
    while [[ $# -gt 0 ]]; do
      case "$1" in
        --workflow) workflow="$2" ;;
        --branch) branch="$2" ;;
        --commit) commit="$2" ;;
        --event) event="$2" ;;
        --limit) limit="$2" ;;
        --json) fields="$2" ;;
        --jq) jq_expr="$2" ;;
        *) echo "unexpected gh run list argument: $1" >&2; exit 2 ;;
      esac
      shift 2
    done
    [[ "${TEST_SCENARIO}" != list-error ]] || { echo "HTTP 502" >&2; exit 1; }
    if [[ "$workflow" == ci.yml ]]; then
      run_event=push
      case "${TEST_SCENARIO}" in
        ci-missing) runs='[]' ;;
        ci-late) if [[ "${count}" -le 3 ]]; then runs='[]'; else runs='[{"databaseId":61,"status":"completed","conclusion":"success"}]'; fi ;;
        ci-failed) runs='[{"databaseId":60,"status":"completed","conclusion":"failure"}]' ;;
        ci-active|ci-watch-failed) runs='[{"databaseId":60,"status":"in_progress","conclusion":""}]' ;;
        ci-pr-success) runs='[{"databaseId":60,"status":"completed","conclusion":"success","event":"pull_request"}]' ;;
        ci-pr-active) runs='[{"databaseId":60,"status":"in_progress","conclusion":"","event":"pull_request"}]' ;;
        ci-response-event-success) runs='[{"databaseId":60,"status":"completed","conclusion":"success","responseEvent":"pull_request"}]' ;;
        ci-response-event-active) runs='[{"databaseId":60,"status":"in_progress","conclusion":"","responseEvent":"pull_request"}]' ;;
        ci-response-branch-success) runs='[{"databaseId":60,"status":"completed","conclusion":"success","responseHeadBranch":"feature"}]' ;;
        ci-response-sha-active) runs='[{"databaseId":60,"status":"in_progress","conclusion":"","responseHeadSha":"2222222222222222222222222222222222222222"}]' ;;
        *) runs='[{"databaseId":61,"status":"completed","conclusion":"success"}]' ;;
      esac
    elif [[ "$workflow" == release.yml ]]; then
      run_event=workflow_dispatch
      runs='[{"databaseId":70,"status":"completed","conclusion":"success","displayTitle":"Release rehearsal v9.8.7"}]'
      if [[ "${TEST_SCENARIO}" == rehearsal-other && ! -f "${TEST_FIXTURE}/release.dispatched" ]]; then
        runs='[{"databaseId":70,"status":"completed","conclusion":"success","displayTitle":"Release rehearsal v9.8.6"},{"databaseId":71,"status":"completed","conclusion":"success","displayTitle":"Release v9.8.7"}]'
      elif [[ "${TEST_SCENARIO}" == rehearsal-failed || -f "${TEST_FIXTURE}/release.dispatched" ]]; then
        runs='[{"databaseId":72,"status":"in_progress","conclusion":"","displayTitle":"Release rehearsal v9.8.7"}]'
      else
        case "${TEST_SCENARIO}" in
          rehearsal-response-event-success) runs="$(jq 'map(.responseEvent = "push")' <<<"$runs")" ;;
          rehearsal-response-branch-active) runs="$(jq 'map(.status = "in_progress" | .conclusion = "" | .responseHeadBranch = "feature")' <<<"$runs")" ;;
          rehearsal-response-sha-success) runs="$(jq 'map(.responseHeadSha = "2222222222222222222222222222222222222222")' <<<"$runs")" ;;
        esac
      fi
    else
      echo "unexpected workflow: $workflow" >&2; exit 2
    fi
    if [[ "${TEST_SCENARIO}" == certified-after-20 ]]; then
      runs="$(jq '[range(25) | . as $i | {databaseId: (100 + $i), status: "completed", conclusion: "failure", displayTitle: "Release rehearsal v9.8.7"}] + .' <<<"$runs")"
    fi
    jq --arg sha "$TEST_HEAD_SHA" --arg run_event "$run_event" \
      --arg branch "$branch" --arg commit "$commit" --arg event "$event" \
      --argjson limit "$limit" --arg fields "$fields" '
        map(.headSha = (.headSha // $sha) | .headBranch = (.headBranch // "main") |
          .event = (.event // $run_event))
        | map(select(($branch == "" or .headBranch == $branch) and
          ($commit == "" or .headSha == $commit) and ($event == "" or .event == $event)))[:$limit]
        | map(if has("responseEvent") then .event = .responseEvent else . end)
        | map(if has("responseHeadBranch") then .headBranch = .responseHeadBranch else . end)
        | map(if has("responseHeadSha") then .headSha = .responseHeadSha else . end)
        | ($fields | split(",")) as $keys
        | map(with_entries(select(.key as $key | $keys | index($key))))
      ' <<<"$runs" | jq -r "$jq_expr"
    ;;
  "workflow run release.yml --ref main -f release_tag=v9.8.7 -f rehearsal=true") touch "${TEST_FIXTURE}/release.dispatched" ;;
  "run watch "*" --exit-status") ;;
  "run view "*" --json conclusion --jq .conclusion") if [[ "${TEST_SCENARIO}" == ci-watch-failed || "${TEST_SCENARIO}" == rehearsal-failed ]]; then echo failure; else echo success; fi ;;
  "run view "*" --json url --jq .url") [[ "$3" != 70 ]] || touch "${TEST_FIXTURE}/rehearsal.seen"; echo "https://example.invalid/runs/$3" ;;
  *) echo "unexpected gh invocation: $*" >&2; exit 2 ;;
esac
EOF
  chmod +x "$mock_bin/git" "$mock_bin/gh" "$mock_bin/sleep"

  # Exercise macOS system Bash 3.2 in hosted CI as well as Linux Bash.
  run_certify() {
    scenario="$1"
    rm -f "$fixture"/*.count "$fixture"/*.dispatched "$fixture"/*.seen
    : >"$log"
    PATH="$mock_bin:$PATH" TEST_HEAD_SHA="$head_sha" TEST_REMOTE_SHA="${2:-$head_sha}" \
      TEST_GH_LOG="$log" TEST_FIXTURE="$fixture" TEST_SCENARIO="$scenario" AL_VERSION="${3-v9.8.7}" \
      /bin/bash "$fixture/scripts/certify-release.sh" >"$fixture/$scenario.out" 2>"$fixture/$scenario.err"
  }

  if run_certify certified && \
     ! grep -q -e '^workflow run' -e '^run watch' -e 'catalog' "$log" && \
     grep -q "Ready to tag v9.8.7 at $head_sha: full CI and signed release rehearsal passed" "$fixture/$scenario.out"; then
    pass "release-certification: exact-SHA CI and version-matched rehearsal suffice without querying any catalog"
  else
    fail "release-certification: must ignore absent or failed catalog runs"
  fi

  if grep -Fq -- "--workflow ci.yml --branch main --commit ${head_sha} --event push" "$log" &&
     grep -Fq -- "--workflow release.yml --branch main --commit ${head_sha} --event workflow_dispatch" "$log"; then
    pass "release-certification: server queries bind CI to push and rehearsal to workflow_dispatch"
  else
    fail "release-certification: server event filters must match the required evidence"
  fi

  if run_certify certified-after-20 && ! grep -q -e '^workflow run' -e '^run watch' "$log" &&
     grep -q "Ready to tag v9.8.7 at $head_sha" "$fixture/$scenario.out"; then
    pass "release-certification: reuses valid CI and rehearsal beyond 20 newer unsuccessful runs"
  else
    fail "release-certification: must find valid evidence beyond the default 20-run window"
  fi

  for scenario in ci-pr-success ci-pr-active ci-response-event-success ci-response-event-active \
    ci-response-branch-success ci-response-sha-active; do
    if run_certify "$scenario" || grep -q -e '^run watch' -e '^workflow run' -e '--workflow release.yml' "$log" ||
       ! grep -q 'CI workflow has no successful run' "$fixture/$scenario.err" ||
       [[ "$(grep -c '^sleep 5$' "$log")" -ne 12 ]]; then
      fail "release-certification: $scenario must neither certify nor await unrelated CI evidence"
    else
      pass "release-certification: $scenario rejects unrelated success or active evidence"
    fi
  done

  for scenario in rehearsal-response-event-success rehearsal-response-branch-active rehearsal-response-sha-success; do
    if run_certify "$scenario" && grep -q '^workflow run release.yml ' "$log" &&
       grep -q '^run watch 72 --exit-status$' "$log" && ! grep -q -e '^run watch 70 ' -e '^run view 70 ' "$log"; then
      pass "release-certification: $scenario dispatches valid evidence instead of accepting or awaiting unrelated rehearsal"
    else
      fail "release-certification: $scenario must reject unrelated rehearsal evidence"
    fi
  done

  if run_certify list-error; then
    fail "release-certification: succeeded although the run list failed"
  elif grep -q '^workflow run' "$log"; then
    fail "release-certification: must not dispatch when the run list fails"
  else
    pass "release-certification: stops without dispatching when the run list fails"
  fi

  if run_certify unpushed "2222222222222222222222222222222222222222"; then
    fail "release-certification: accepted an unpushed main commit"
  elif [[ -s "$log" ]] || ! grep -q 'is not the pushed origin/main commit' "$fixture/$scenario.err"; then
    fail "release-certification: must reject an unpushed commit before GitHub workflow calls"
  else
    pass "release-certification: rejects an unpushed commit before GitHub workflow calls"
  fi

  for scenario in ci-missing ci-failed ci-watch-failed; do
    if run_certify "$scenario" || grep -q -- '--workflow release.yml' "$log" || ! grep -q 'CI' "$fixture/$scenario.err"; then
      fail "release-certification: $scenario must block before any rehearsal workflow call"
    elif [[ "$scenario" == ci-missing && "$(grep -c '^sleep 5$' "$log")" -ne 12 ]]; then
      fail "release-certification: missing CI must exhaust the bounded appearance wait"
    else pass "release-certification: $scenario blocks release certification"; fi
  done
  if run_certify ci-active && awk '/^run watch 60 / {watch=NR} /^run view 60 / {view=NR} /--workflow release.yml/ && !rehearsal {rehearsal=NR} END {exit !(watch && view>watch && rehearsal>view)}' "$log"; then
    pass "release-certification: awaits pending exact-SHA CI and verifies success before release rehearsal"
  else
    fail "release-certification: must await and verify pending CI before release rehearsal"
  fi

  if run_certify ci-late && [[ "$(grep -c '^sleep 5$' "$log")" -eq 3 ]] && ! grep -q '^workflow run ci.yml' "$log"; then
    pass "release-certification: waits for late push CI without dispatching it"
  else fail "release-certification: must await late push CI"; fi

  if run_certify rehearsal-other && grep -q '^workflow run release.yml --ref main -f release_tag=v9.8.7 -f rehearsal=true$' "$log" && grep -q 'run watch 72 --exit-status' "$log" && grep -q "Ready to tag v9.8.7 at $head_sha" "$fixture/$scenario.out"; then
    pass "release-rehearsal: other versions and publication runs cannot satisfy exact-SHA rehearsal; dispatches and awaits the intended version"
  else fail "release-rehearsal: must dispatch and await the intended version instead of accepting unrelated success"; fi
  if run_certify rehearsal-failed || ! grep -q 'release.yml certification 72 concluded failure' "$fixture/$scenario.err"; then
    fail "release-rehearsal: must stop when the awaited rehearsal fails"
  else pass "release-rehearsal: failed rehearsal blocks tagging readiness"; fi
  if run_certify no-version "$head_sha" "" || [[ -s "$log" ]] || ! grep -q 'AL_VERSION must explicitly name' "$fixture/$scenario.err"; then
    fail "release-rehearsal: must reject a missing intended version before workflow calls"
  else pass "release-rehearsal: intended version is required, never inferred"; fi
  if run_certify rehearsal-changed || ! grep -q 'main changed during certification' "$fixture/$scenario.err"; then
    fail "release-rehearsal: must reject source changes after successful certification"
  else pass "release-rehearsal: dirty working tree invalidates successful evidence"; fi

  rm -rf "$fixture"

}
