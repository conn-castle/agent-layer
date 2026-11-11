# Security contracts for the declarative release pipeline.

release_workflow_job() {
  local workflow="$1"
  local job="$2"
  awk -v job="$job" '
    $0 == "  " job ":" { capture = 1 }
    capture {
      if ($0 != "  " job ":" && /^  [^[:space:]]/) exit
      print
    }
  ' "$workflow"
}

release_workflow_step() {
  local job="$1"
  local step="$2"
  awk -v step="$step" '
    $0 == "      - name: " step { capture = 1 }
    capture {
      if ($0 != "      - name: " step && /^      - /) exit
      print
    }
  ' <<<"$job"
}

release_step_line() {
  local job="$1"
  local step="$2"
  awk -v step="$step" '$0 == "      - name: " step { print NR; exit }' <<<"$job"
}

release_workflow_run_script() {
  release_workflow_step "$1" "$2" | awk '
    /^        run: \|$/ { capture = 1; next }
    capture && /^          / { print substr($0, 11); next }
    capture && /^[[:space:]]*$/ { print ""; next }
    capture { exit }
  '
}

run_release_readiness_lookup_tests() {
  local workflow="$1" readiness_job fixture mock_bin head_sha other_sha
  readiness_job=$(release_workflow_job "$workflow" "release-readiness")
  fixture=$(mktemp -d "$tmp_dir/readiness.XXXXXX")
  mock_bin="$fixture/bin"
  head_sha="1111111111111111111111111111111111111111"
  other_sha="2222222222222222222222222222222222222222"
  mkdir -p "$mock_bin"

  cat >"$mock_bin/git" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$*" == "rev-parse HEAD" ]]; then
  echo "$TEST_HEAD_SHA"
else
  echo "unexpected git invocation: $*" >&2
  exit 2
fi
EOF
  # Model the server filters before projecting --json fields and evaluating the
  # actual workflow's --jq. A lagged run is absent from every status index.
  cat >"$mock_bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1 $2" == "run list" ]] || { echo "unexpected gh invocation: $*" >&2; exit 2; }
shift 2
workflow="" branch="" commit="" event="" status="" limit=20 fields="" jq_expr=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --workflow) workflow="$2" ;;
    --branch) branch="$2" ;;
    --commit) commit="$2" ;;
    --event) event="$2" ;;
    --status) status="$2" ;;
    --limit) limit="$2" ;;
    --json) fields="$2" ;;
    --jq) jq_expr="$2" ;;
    *) echo "unexpected gh run list argument: $1" >&2; exit 2 ;;
  esac
  shift 2
done
jq --arg workflow "$workflow" --arg branch "$branch" --arg commit "$commit" \
  --arg event "$event" --arg status "$status" --argjson limit "$limit" \
  --arg fields "$fields" '
    map(select(
      ($workflow == "" or .workflow == $workflow) and
      ($branch == "" or .headBranch == $branch) and
      ($commit == "" or .headSha == $commit) and
      ($event == "" or .event == $event) and
      ($status == "" or ((.statusIndexLag != true) and
        (.status == $status or .conclusion == $status)))
    ))[:$limit]
    # Deliberately inconsistent response to exercise the client SHA check even
    # when the server accepted the commit filter.
    | map(if has("responseHeadSha") then .headSha = .responseHeadSha else . end)
    | ($fields | split(",")) as $keys
    | map(with_entries(select(.key as $key | $keys | index($key))))
  ' "$TEST_RUNS" | jq -r "$jq_expr"
EOF
  chmod +x "$mock_bin/git" "$mock_bin/gh"

  local kind step success_run scenario runs expected expected_message result
  for kind in ci rehearsal; do
    if [[ "$kind" == ci ]]; then
      step="Verify exact-commit CI"
      expected_message="Using exact-commit ci.yml: https://example.invalid/runs/534"
    else
      step="Verify exact-commit release rehearsal before publication"
      expected_message="Using signed release rehearsal: https://example.invalid/runs/534"
    fi
    release_workflow_run_script "$readiness_job" "$step" >"$fixture/step.sh"
    if [[ ! -s "$fixture/step.sh" ]]; then
      fail "release-readiness: $kind run script is missing"
      continue
    fi
    success_run=$(jq -cn --arg sha "$head_sha" --arg kind "$kind" '{
      workflow: (if $kind == "ci" then "ci.yml" else "release.yml" end),
      headBranch: "main", headSha: $sha, event: "workflow_dispatch",
      status: "completed", conclusion: "success",
      displayTitle: "Release rehearsal v9.8.7", url: "https://example.invalid/runs/534"
    }')
    for scenario in lagging-index no-runs failure cancelled in-progress wrong-workflow \
      wrong-branch wrong-commit response-sha-mismatch newest-failed-older-success \
      other-version publication-title wrong-event; do
      expected=1
      case "$scenario" in
        lagging-index)
          runs='[$run | .statusIndexLag = true]'; expected=0 ;;
        no-runs) runs='[]' ;;
        failure) runs='[$run | .conclusion = "failure"]' ;;
        cancelled) runs='[$run | .conclusion = "cancelled"]' ;;
        in-progress) runs='[$run | .status = "in_progress" | .conclusion = ""]' ;;
        wrong-workflow) runs='[$run | .workflow = "unrelated.yml"]' ;;
        wrong-branch) runs='[$run | .headBranch = "feature"]' ;;
        wrong-commit) runs='[$run | .headSha = $other]' ;;
        response-sha-mismatch) runs='[$run | .responseHeadSha = $other]' ;;
        newest-failed-older-success)
          runs='[($run | .conclusion = "failure" | .url = "https://example.invalid/failed"),
            ($run | .headSha = $other),
            ($run | .workflow = "unrelated.yml"),
            ($run | .headBranch = "feature"), $run,
            ($run | .url = "https://example.invalid/older")]'
          if [[ "$kind" == rehearsal ]]; then
            runs='[($run | .displayTitle = "Release rehearsal v9.8.6"),
              ($run | .displayTitle = "Release v9.8.7"),
              ($run | .event = "push")] + ('"$runs"')'
          fi
          expected=0 ;;
        other-version)
          [[ "$kind" == rehearsal ]] || continue
          runs='[$run | .displayTitle = "Release rehearsal v9.8.6"]' ;;
        publication-title)
          [[ "$kind" == rehearsal ]] || continue
          runs='[$run | .displayTitle = "Release v9.8.7"]' ;;
        wrong-event)
          [[ "$kind" == rehearsal ]] || continue
          runs='[$run | .event = "push"]' ;;
      esac
      jq -n --argjson run "$success_run" --arg other "$other_sha" "$runs" >"$fixture/runs.json"
      result=0
      PATH="$mock_bin:$PATH" TEST_HEAD_SHA="$head_sha" TEST_RUNS="$fixture/runs.json" \
        RELEASE_TAG=v9.8.7 GH_TOKEN=fixture-token \
        /bin/bash "$fixture/step.sh" >"$fixture/out" 2>"$fixture/err" || result=$?
      if [[ "$result" -ne "$expected" || -s "$fixture/err" ]]; then
        fail "release-readiness: $kind $scenario (exit $result, expected $expected)"
        cat "$fixture/out" "$fixture/err"
      elif [[ "$expected" -eq 0 ]]; then
        if grep -Fxq "$expected_message" "$fixture/out"; then
          pass "release-readiness: $kind $scenario selects the correct successful run URL"
        else
          fail "release-readiness: $kind $scenario returned the wrong evidence URL"
          cat "$fixture/out"
        fi
      elif [[ "$kind" == ci ]] &&
        grep -Fxq "::error::No successful ci.yml exists for exact source commit ${head_sha}." "$fixture/out" &&
        grep -Fxq "::error::Run make release-certify AL_VERSION=v9.8.7 on clean, pushed main before tagging." "$fixture/out"; then
        pass "release-readiness: $kind $scenario rejects missing evidence with certification guidance"
      elif [[ "$kind" == rehearsal ]] &&
        grep -Fxq "::error::No successful exact-commit rehearsal exists for v9.8.7. Run make release-certify AL_VERSION=v9.8.7 before tagging." "$fixture/out"; then
        pass "release-readiness: $kind $scenario rejects missing evidence with certification guidance"
      else
        fail "release-readiness: $kind $scenario omitted the expected error and certification guidance"
        cat "$fixture/out"
      fi
    done
  done
  rm -rf "$fixture"
}

run_release_workflow_security_tests() {
  section "Release Workflow Security Tests"

  local workflow="$ROOT_DIR/.github/workflows/release.yml"
  if [[ ! -f "$workflow" ]]; then
    fail "release-security: release workflow is missing"
    return
  fi

  local readiness_job build_job

  if [[ ! -f "$ROOT_DIR/.github/workflows/release-catalog-certification.yml" ]] &&
     ! grep -Eq 'release-catalog|benchmark readiness|public\.ecr\.aws|docker pull' "$workflow" "$ROOT_DIR/.github/workflows/ci.yml" &&
     grep -q 'run: make benchmark-contract-check' "$ROOT_DIR/.github/workflows/ci.yml"; then
    pass "release-security: core workflows exclude live catalog pulls and retain companion fixture contracts"
  else
    fail "release-security: live catalog access returned or companion contracts are missing"
  fi

  readiness_job=$(release_workflow_job "$workflow" "release-readiness")
  build_job=$(release_workflow_job "$workflow" "build-release")
  if [[ -z "$readiness_job" || -z "$build_job" ]]; then
    fail "release-security: release-readiness or build-release job is missing"
    return
  fi

  if grep -q '^    needs: release-readiness$' <<<"$build_job"; then
    pass "release-security: release build requires exact-commit CI"
  else
    fail "release-security: build-release must require release-readiness"
  fi

  local ci_check_step
  ci_check_step=$(release_workflow_step "$readiness_job" "Verify exact-commit CI")
  if grep -Eq '^[[:space:]]+release_commit="\$\(git rev-parse HEAD\)"$' <<<"$ci_check_step" &&
     grep -Eq '^[[:space:]]+for workflow in ci.yml; do$' <<<"$ci_check_step" &&
     grep -Eq '^[[:space:]]+--workflow "\$\{workflow\}" \\$' <<<"$ci_check_step" &&
     grep -Eq '^[[:space:]]+--branch main \\$' <<<"$ci_check_step" &&
     grep -Eq '^[[:space:]]+--commit "\$\{release_commit\}" \\$' <<<"$ci_check_step"; then
    pass "release-security: readiness requires successful CI for the exact source commit"
  else
    fail "release-security: readiness must bind successful CI to the exact source commit"
  fi

  if ! grep -q -- '--status' <<<"$readiness_job"; then
    pass "release-readiness: lookups avoid the lagging server status index"
  else
    fail "release-readiness: lookups must not use --status anywhere in the job"
  fi
  local rehearsal_check_step
  rehearsal_check_step=$(release_workflow_step "$readiness_job" "Verify exact-commit release rehearsal before publication")
  if grep -q "^        if: env.REHEARSAL != 'true'$" <<<"$rehearsal_check_step" &&
     grep -Fq -- '--workflow release.yml --event workflow_dispatch' <<<"$rehearsal_check_step" &&
     grep -Fq -- '--branch main --commit "${release_commit}"' <<<"$rehearsal_check_step" &&
     grep -Eq '^[[:space:]]+release_commit="\$\(git rev-parse HEAD\)"$' <<<"$rehearsal_check_step"; then
    pass "release-readiness: publication rehearsal keeps workflow, event, branch, and checked-out commit binding"
  else
    fail "release-readiness: publication rehearsal must retain its publication guard and source binding"
  fi
  run_release_readiness_lookup_tests "$workflow"

  local web_job step
  web_job=$(release_workflow_job "$workflow" "publish-website-and-tap")
  for step in "Publish release" "Commit and push" "Verify release assets are available" "Create PR in tap repo"; do
    if grep -q "^        if: env.REHEARSAL != 'true'$" <<<"$(release_workflow_step "$build_job"$'\n'"$web_job" "$step")"; then
      pass "release-rehearsal: $step is disabled in rehearsal"
    else fail "release-rehearsal: $step must be disabled in rehearsal"; fi
  done
  if grep -q 'default: true' <<<"$(sed -n '/      rehearsal:/,/^permissions:/p' "$workflow")" &&
     grep -q "REHEARSAL: \${{ github.event_name == 'workflow_dispatch' && inputs.rehearsal }}" "$workflow" &&
     [[ "$(grep -c 'ref: \${{ env.RELEASE_REF }}' "$workflow")" -eq 3 ]] &&
     grep -q 'inputs.rehearsal && github.sha || format' "$workflow" &&
     grep -q 'agent-layer-web-rehearsal-{0}' <<<"$web_job" &&
     grep -q '^  contents: read$' "$workflow" && ! grep -Eq '^[[:space:]]+contents: write$' <<<"$readiness_job$web_job" &&
     grep -q '^      contents: write$' <<<"$build_job" &&
     [[ "$(grep -c "persist-credentials: \${{ env.REHEARSAL != 'true' }}" <<<"$web_job")" -eq 2 ]]; then
    pass "release-rehearsal: safe default uses dispatched SHA, isolated concurrency, and no persisted external credentials"
  else fail "release-rehearsal: manual default, source, concurrency, or credentials are unsafe"; fi
  if grep -q -- '--event workflow_dispatch' <<<"$(release_workflow_step "$readiness_job" "Verify exact-commit release rehearsal before publication")" &&
     grep -q 'Release rehearsal \${RELEASE_TAG}' <<<"$readiness_job" &&
     [[ "$(grep -c 'permission-contents: write' <<<"$web_job")" -eq 2 ]] &&
     grep -q 'permission-pull-requests: write' <<<"$web_job" &&
     grep -q 'run: ruby -c homebrew-tap/Formula/agent-layer.rb' <<<"$(release_workflow_step "$web_job" "Validate rendered formula syntax")"; then
    pass "release-rehearsal: publication requires version-bound evidence while both modes validate app permissions and formula syntax"
  else fail "release-rehearsal: evidence, app permission validation, or formula check is missing"; fi

  local certificate_step build_step scan_step
  certificate_step=$(release_workflow_step "$build_job" "Import Developer ID cert")
  build_step=$(release_workflow_step "$build_job" "Build release artifacts")
  scan_step=$(release_workflow_step "$build_job" "Scan release binaries for known vulnerable symbols")

  if grep -Eq '^        uses: Apple-Actions/import-codesign-certs@[0-9a-f]{40}([[:space:]]+#.*)?$' <<<"$certificate_step" &&
     grep -Eq '^          p12-file-base64: \$\{\{ secrets\.MACOS_CERT_P12_BASE64 \}\}$' <<<"$certificate_step" &&
     grep -Eq '^          p12-password: \$\{\{ secrets\.MACOS_CERT_P12_PASSWORD \}\}$' <<<"$certificate_step"; then
    pass "release-security: certificate import is commit-pinned and secret-backed"
  else
    fail "release-security: certificate import must be commit-pinned and secret-backed"
  fi

  if grep -Eq '^          AL_REQUIRE_CODESIGN: "1"$' <<<"$build_step" &&
     grep -Eq '^          AL_CODESIGN_IDENTITY: ".+"$' <<<"$build_step" &&
     grep -Eq '^[[:space:]]+make release-dist AL_VERSION="\$\{RELEASE_TAG\}" DIST_DIR=dist$' <<<"$build_step"; then
    pass "release-security: release artifacts require code signing"
  else
    fail "release-security: release artifacts must require a signing identity"
  fi

  local validate_line build_line scan_line notarize_line upload_line publish_line
  validate_line=$(release_step_line "$build_job" "Validate stable release tag format")
  build_line=$(release_step_line "$build_job" "Build release artifacts")
  scan_line=$(release_step_line "$build_job" "Scan release binaries for known vulnerable symbols")
  notarize_line=$(release_step_line "$build_job" "Notarize darwin binaries")
  upload_line=$(release_step_line "$build_job" "Upload dist artifacts for downstream jobs")
  publish_line=$(release_step_line "$build_job" "Publish release")

  if [[ -n "$validate_line" && -n "$publish_line" ]] && (( validate_line < publish_line )); then
    pass "release-security: stable tag validation precedes publication"
  else
    fail "release-security: stable tag validation must precede publication"
  fi

  if [[ -n "$build_line" && -n "$scan_line" && -n "$notarize_line" && -n "$upload_line" && -n "$publish_line" ]] &&
     (( build_line < scan_line && scan_line < notarize_line && scan_line < upload_line && scan_line < publish_line )) &&
     grep -Eq '^[[:space:]]+(run:[[:space:]]+)?make release-vuln-check DIST_DIR=dist[[:space:]]*$' <<<"$scan_step"; then
    pass "release-security: vulnerability scanning follows the build and gates every artifact publication path"
  else
    fail "release-security: vulnerability scanning must follow the build and gate notarization, upload, and publication"
  fi
}
