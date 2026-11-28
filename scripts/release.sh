#!/usr/bin/env bash
# Dispatch and monitor the single-run Release workflow on clean, pushed main.
#
# Environment variables:
#   AL_VERSION           - Required stable version (vX.Y.Z).
#   AL_RELEASE_REHEARSAL - Set to "1" for a nonpublishing signed dry-run (default: 0).
#   AL_RELEASE_REPO      - Override GitHub owner/repo (default: resolved from gh).
set -euo pipefail

die() {
  echo "ERROR: $*" >&2
  exit 1
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd "${script_dir}/.." && pwd -P)"

release_tag="${AL_VERSION:-}"
[[ "${release_tag}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "AL_VERSION must explicitly name a stable version (vX.Y.Z)"

rehearsal_mode="${AL_RELEASE_REHEARSAL:-0}"
if [[ "${rehearsal_mode}" == "1" || "${rehearsal_mode}" == "true" ]]; then
  rehearsal_flag="true"
  expected_title="Release rehearsal ${release_tag}"
else
  rehearsal_flag="false"
  expected_title="Release ${release_tag}"
fi

command -v git >/dev/null 2>&1 || die "git is required"
command -v gh >/dev/null 2>&1 || die "GitHub CLI (gh) is required"
command -v jq >/dev/null 2>&1 || die "jq is required"

cd "${repo_root}"
[[ "$(git branch --show-current)" == "main" ]] || die "release must run from main"
[[ -z "$(git status --porcelain)" ]] || die "release requires a clean working tree"

head_sha="$(git rev-parse HEAD)"
remote_sha="$(git ls-remote --exit-code origin refs/heads/main | awk 'NR == 1 { print $1 }')"
[[ -n "${remote_sha}" ]] || die "could not resolve origin/main"
[[ "${head_sha}" == "${remote_sha}" ]] || die "HEAD ${head_sha} is not the pushed origin/main commit ${remote_sha}"

if [[ -n "$(git ls-remote --tags origin "refs/tags/${release_tag}")" ]]; then
  die "release tag ${release_tag} already exists on origin; recover partial publication by re-running failed jobs on the existing workflow run rather than starting a new build"
fi

repo="${AL_RELEASE_REPO:-}"
if [[ -z "${repo}" ]]; then
  repo="$(gh repo view --json nameWithOwner --jq '.nameWithOwner')"
fi
[[ -n "${repo}" ]] || die "could not resolve GitHub repository nameWithOwner"

# Verify full CI passed (exact push CI on main OR clean-merge exact-tree same-repo PR CI).
"${script_dir}/verify-release-ci.sh" \
  --commit "${head_sha}" \
  --repo "${repo}" \
  --wait-active \
  --push-wait-seconds 60

refresh_release_run() {
  local state
  state="$(
    gh run list \
      --workflow release.yml \
      --branch main \
      --commit "${head_sha}" \
      --event workflow_dispatch \
      --limit 100 \
      --json databaseId,status,conclusion,displayTitle,event,headBranch,headSha \
      --jq "
        map(select(.event == \"workflow_dispatch\" and .headBranch == \"main\" and .headSha == \"${head_sha}\"
          and .displayTitle == \"${expected_title}\"))
        | ([.[] | select(.conclusion == \"success\")][0].databaseId) as \$success
        | ([.[] | select(.status != \"completed\")][0].databaseId) as \$active
        | if \$success != null then \"success \(\$success)\"
          elif \$active != null then \"active \(\$active)\"
          elif length > 0 then \"completed\"
          else \"none\" end"
  )"
  read -r run_state run_id <<<"${state}"
}

run_state=""
run_id=""
refresh_release_run

if [[ "${run_state}" == "success" ]]; then
  echo "release.yml (${expected_title}) already succeeded for ${head_sha}:"
  gh run view "${run_id}" --json url --jq '.url'
else
  active_run=""
  [[ "${run_state}" == "active" ]] && active_run="${run_id}"
  if [[ -z "${active_run}" ]]; then
    echo "Dispatching release.yml for ${head_sha} (${expected_title}, rehearsal=${rehearsal_flag})."
    gh workflow run release.yml --ref main -f "release_tag=${release_tag}" -f "rehearsal=${rehearsal_flag}"
    for _ in $(seq 1 30); do
      refresh_release_run
      if [[ "${run_state}" == "active" || "${run_state}" == "success" ]]; then
        active_run="${run_id}"
        break
      fi
      sleep 1
    done
  fi

  [[ -n "${active_run}" ]] || die "could not find an active or successful release.yml run for ${head_sha} (${expected_title})"
  gh run watch "${active_run}" --exit-status
  conclusion="$(gh run view "${active_run}" --json conclusion --jq '.conclusion')"
  [[ "${conclusion}" == "success" ]] || die "release.yml run ${active_run} concluded ${conclusion}"
  echo "release.yml (${expected_title}) succeeded for ${head_sha}:"
  gh run view "${active_run}" --json url --jq '.url'
fi

[[ -z "$(git status --porcelain)" && "$(git rev-parse HEAD)" == "${head_sha}" && "$(git ls-remote --exit-code origin refs/heads/main | awk 'NR == 1 { print $1 }')" == "${head_sha}" ]] \
  || die "main changed during release workflow execution"

if [[ "${rehearsal_flag}" == "true" ]]; then
  echo "Signed release rehearsal passed for ${release_tag} at ${head_sha}. Run make release AL_VERSION=${release_tag} to publish."
else
  echo "Published ${release_tag} at ${head_sha}: https://github.com/${repo}/releases/tag/${release_tag}"
fi
