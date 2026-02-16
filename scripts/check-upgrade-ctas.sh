#!/usr/bin/env bash
set -euo pipefail

die() {
  echo "ERROR: $*" >&2
  exit 1
}

if ! command -v rg >/dev/null 2>&1; then
  die "ripgrep (rg) is required for this check. Install rg and retry."
fi

cta_files=(
  "README.md"
  "site/pages/install.mdx"
  "site/pages/faq.mdx"
  "site/docs/upgrade-checklist.mdx"
  "site/docs/concepts.mdx"
  "site/docs/reference.mdx"
  "site/docs/troubleshooting.mdx"
  "internal/messages/cli.go"
)

for file in "${cta_files[@]}"; do
  [[ -f "$file" ]] || die "missing CTA surface file: $file"
done

required_patterns=(
  "al upgrade plan"
  "al upgrade --yes --apply-managed-updates"
  "al upgrade rollback <snapshot-id>"
)

for pattern in "${required_patterns[@]}"; do
  if ! rg -Fq "$pattern" "${cta_files[@]}"; then
    die "missing required upgrade CTA pattern in docs/messages: $pattern"
  fi
done

forbidden_patterns=(
  "al init --overwrite"
  "al init --force"
  "al upgrade --force"
  "al upgrade plan --json"
)

for pattern in "${forbidden_patterns[@]}"; do
  if matches="$(rg -n -F "$pattern" "${cta_files[@]}" 2>/dev/null)"; then
    printf '%s\n' "$matches" >&2
    die "found forbidden upgrade CTA pattern: $pattern"
  fi
done

# Check only the current Unreleased changelog section for forbidden CTA drift.
changelog_unreleased="$(
  awk '
    /^## Unreleased/ { in_unreleased=1; next }
    /^## / { if (in_unreleased) exit }
    in_unreleased { print }
  ' CHANGELOG.md
)"

if [[ -z "$changelog_unreleased" ]]; then
  die "failed to read CHANGELOG.md Unreleased section for CTA checks"
fi

for pattern in "${forbidden_patterns[@]}"; do
  if grep -Fq "$pattern" <<<"$changelog_unreleased"; then
    die "found forbidden upgrade CTA pattern in CHANGELOG.md Unreleased section: $pattern"
  fi
done

while IFS= read -r line; do
  [[ -n "$line" ]] || continue
  if [[ "$line" == *"--apply-managed-updates"* ]] || [[ "$line" == *"--apply-memory-updates"* ]] || [[ "$line" == *"--apply-deletions"* ]]; then
    continue
  fi
  die "invalid non-interactive upgrade CTA in CHANGELOG.md Unreleased section: $line"
done < <(grep -F "al upgrade --yes" <<<"$changelog_unreleased" || true)

bad_yes_lines=0
while IFS= read -r match; do
  file="${match%%:*}"
  rest="${match#*:}"
  line_no="${rest%%:*}"
  text="${rest#*:}"
  if [[ "$text" == *"--apply-managed-updates"* ]] || [[ "$text" == *"--apply-memory-updates"* ]] || [[ "$text" == *"--apply-deletions"* ]]; then
    continue
  fi
  echo "${file}:${line_no}: invalid non-interactive upgrade CTA (missing apply flag): ${text}" >&2
  bad_yes_lines=1
done < <(rg -n --no-heading -F "al upgrade --yes" "${cta_files[@]}" || true)

if [[ "$bad_yes_lines" -ne 0 ]]; then
  die "one or more non-interactive upgrade CTAs use --yes without apply flags"
fi

echo "Upgrade CTA syntax check passed"
