#!/usr/bin/env bash
set -euo pipefail

die() {
  echo "ERROR: $*" >&2
  exit 1
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd "${script_dir}/.." && pwd -P)"
workflow="release-catalog-certification.yml"

command -v git >/dev/null 2>&1 || die "git is required"
command -v gh >/dev/null 2>&1 || die "GitHub CLI (gh) is required"

cd "${repo_root}"
[[ "$(git branch --show-current)" == "main" ]] || die "release catalog certification must run from main"
[[ -z "$(git status --porcelain)" ]] || die "release catalog certification requires a clean working tree"

head_sha="$(git rev-parse HEAD)"
remote_sha="$(git ls-remote --exit-code origin refs/heads/main | awk 'NR == 1 { print $1 }')"
[[ -n "${remote_sha}" ]] || die "could not resolve origin/main"
[[ "${head_sha}" == "${remote_sha}" ]] || die "HEAD ${head_sha} is not the pushed origin/main commit ${remote_sha}"

# A push to main starts certification, but the run can take several seconds to
# appear in the run list. Wait this long for it before dispatching another run.
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
      --json databaseId,status,conclusion \
      --jq '
        ([.[] | select(.conclusion == "success")][0].databaseId) as $success
        | ([.[] | select(.status != "completed")][0].databaseId) as $active
        | if $success != null then "success \($success)"
          elif $active != null then "active \($active)"
          elif length > 0 then "completed"
          else "none" end'
  )"
  read -r run_state run_id <<<"${state}"
}

refresh_run_state
if [[ "${run_state}" == "none" ]]; then
  echo "Waiting up to ${push_run_wait_seconds}s for the push-triggered certification run for ${head_sha}."
  for ((waited = 0; waited < push_run_wait_seconds; waited += push_run_poll_seconds)); do
    sleep "${push_run_poll_seconds}"
    refresh_run_state
    [[ "${run_state}" == "none" ]] || break
  done
fi

if [[ "${run_state}" == "success" ]]; then
  echo "Release catalog already certified for ${head_sha}:"
  gh run view "${run_id}" --json url --jq '.url'
  exit 0
fi

active_run=""
[[ "${run_state}" == "active" ]] && active_run="${run_id}"
if [[ -z "${active_run}" ]]; then
  previous_run="$(
    gh run list \
      --workflow "${workflow}" \
      --branch main \
      --event workflow_dispatch \
      --commit "${head_sha}" \
      --limit 1 \
      --json databaseId \
      --jq '.[0].databaseId // ""'
  )"
  echo "Dispatching release catalog certification for ${head_sha}."
  gh workflow run "${workflow}" --ref main
  for _ in $(seq 1 30); do
    active_run="$(
      gh run list \
        --workflow "${workflow}" \
        --branch main \
        --event workflow_dispatch \
        --commit "${head_sha}" \
        --limit 1 \
        --json databaseId \
        --jq '.[0].databaseId // ""'
    )"
    [[ -n "${active_run}" && "${active_run}" != "${previous_run}" ]] && break
    active_run=""
    sleep 1
  done
fi

[[ -n "${active_run}" ]] || die "could not find the dispatched certification run for ${head_sha}"
gh run watch "${active_run}" --exit-status

conclusion="$(gh run view "${active_run}" --json conclusion --jq '.conclusion')"
[[ "${conclusion}" == "success" ]] || die "catalog certification ${active_run} concluded ${conclusion}"
echo "Release catalog certified for ${head_sha}."
