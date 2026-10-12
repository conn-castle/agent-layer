#!/usr/bin/env bash
set -euo pipefail

die() {
  echo "ERROR: $*" >&2
  exit 1
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd "${script_dir}/.." && pwd -P)"

release_tag="${AL_VERSION:-}"
[[ "${release_tag}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "AL_VERSION must explicitly name a stable version (vX.Y.Z)"

command -v git >/dev/null 2>&1 || die "git is required"
command -v gh >/dev/null 2>&1 || die "GitHub CLI (gh) is required"

cd "${repo_root}"
[[ "$(git branch --show-current)" == "main" ]] || die "release catalog certification must run from main"
[[ -z "$(git status --porcelain)" ]] || die "release catalog certification requires a clean working tree"

head_sha="$(git rev-parse HEAD)"
remote_sha="$(git ls-remote --exit-code origin refs/heads/main | awk 'NR == 1 { print $1 }')"
[[ -n "${remote_sha}" ]] || die "could not resolve origin/main"
[[ "${head_sha}" == "${remote_sha}" ]] || die "HEAD ${head_sha} is not the pushed origin/main commit ${remote_sha}"

# Push-triggered CI and certification can take several seconds to appear.
# Wait this long for each before failing CI or dispatching certification.
push_run_wait_seconds=60
push_run_poll_seconds=5

# Sets run_state to "success", "active", "completed", or "none" for the runs of
# HEAD, and run_id to the successful or active run.
refresh_run_state() {
  local state
  state="$(
    gh run list \
      --workflow "${workflow}" \
      --branch main \
      --commit "${head_sha}" \
      --limit 20 \
      --json databaseId,status,conclusion,displayTitle \
      --jq "
        map(select(\"${workflow}\" != \"release.yml\" or .displayTitle == \"Release rehearsal ${release_tag}\"))
        | ([.[] | select(.conclusion == \"success\")][0].databaseId) as \$success
        | ([.[] | select(.status != \"completed\")][0].databaseId) as \$active
        | if \$success != null then \"success \(\$success)\"
          elif \$active != null then \"active \(\$active)\"
          elif length > 0 then \"completed\"
          else \"none\" end"
  )"
  read -r run_state run_id <<<"${state}"
}

for workflow in ci.yml release-catalog-certification.yml release.yml; do
  refresh_run_state
  if [[ "${run_state}" == "none" && "${workflow}" != "release.yml" ]]; then
    echo "Waiting up to ${push_run_wait_seconds}s for the push-triggered ${workflow} run for ${head_sha}."
    for ((waited = 0; waited < push_run_wait_seconds; waited += push_run_poll_seconds)); do
      sleep "${push_run_poll_seconds}"
      refresh_run_state
      [[ "${run_state}" == "none" ]] || break
    done
  fi

  if [[ "${workflow}" == "ci.yml" ]]; then
    case "${run_state}" in
      success) ;;
      active)
        echo "Waiting for full CI run ${run_id} for ${head_sha}."
        gh run watch "${run_id}" --exit-status
        conclusion="$(gh run view "${run_id}" --json conclusion --jq '.conclusion')"
        [[ "${conclusion}" == "success" ]] || die "CI verification ${run_id} concluded ${conclusion}"
        ;;
      *) die "CI workflow has no successful run for ${head_sha} (${run_state})" ;;
    esac
    echo "Full CI verified for ${head_sha}."
    continue
  fi

  if [[ "${run_state}" == "success" ]]; then
    echo "${workflow} already certified for ${head_sha} (${release_tag}):"
    gh run view "${run_id}" --json url --jq '.url'
    continue
  fi

  active_run=""
  [[ "${run_state}" == "active" ]] && active_run="${run_id}"
  if [[ -z "${active_run}" ]]; then
    dispatch_inputs=()
    [[ "${workflow}" != "release.yml" ]] || dispatch_inputs=(-f "release_tag=${release_tag}" -f rehearsal=true)
    echo "Dispatching ${workflow} for ${head_sha} (${release_tag})."
    gh workflow run "${workflow}" --ref main ${dispatch_inputs[@]+"${dispatch_inputs[@]}"}
    for _ in $(seq 1 30); do
      refresh_run_state
      if [[ "${run_state}" == "active" || "${run_state}" == "success" ]]; then
        active_run="${run_id}"
        break
      fi
      sleep 1
    done
  fi

  [[ -n "${active_run}" ]] || die "could not find an active or successful ${workflow} run for ${head_sha} (${release_tag})"
  gh run watch "${active_run}" --exit-status
  conclusion="$(gh run view "${active_run}" --json conclusion --jq '.conclusion')"
  [[ "${conclusion}" == "success" ]] || die "${workflow} certification ${active_run} concluded ${conclusion}"
  echo "${workflow} certified for ${head_sha} (${release_tag})."
  gh run view "${active_run}" --json url --jq '.url'
done
[[ -z "$(git status --porcelain)" && "$(git rev-parse HEAD)" == "${head_sha}" && "$(git ls-remote --exit-code origin refs/heads/main | awk 'NR == 1 { print $1 }')" == "${head_sha}" ]] || die "main changed during certification; rerun for the unchanged, clean, pushed release commit"
echo "Ready to tag ${release_tag} at ${head_sha}: full CI, catalog, and signed release rehearsal passed."
