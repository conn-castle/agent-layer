#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/check-upgrade-docs.sh --tag vX.Y.Z [--upgrades-file PATH] [--changelog-file PATH]

Description:
  Validates upgrade-contract documentation for a target release tag.
  - Fails if site/docs/upgrades.mdx has no migration-table row for the release tag.
  - Fails if CHANGELOG indicates breaking/manual migration impact while the matching row still uses placeholder text.
  - Fails if upgrade CTA syntax drifts in core docs/message surfaces.

Options:
  --tag TAG               Release tag (required), for example: v0.7.0
  --upgrades-file PATH    Path to upgrades doc (default: site/docs/upgrades.mdx)
  --changelog-file PATH   Path to changelog (default: CHANGELOG.md)
  --help                  Show this help
EOF
}

die() {
  echo "ERROR: $*" >&2
  exit 1
}

release_tag=""
upgrades_file="site/docs/upgrades.mdx"
changelog_file="CHANGELOG.md"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --tag)
      [[ $# -ge 2 ]] || die "--tag requires a value"
      release_tag="$2"
      shift 2
      ;;
    --upgrades-file)
      [[ $# -ge 2 ]] || die "--upgrades-file requires a value"
      upgrades_file="$2"
      shift 2
      ;;
    --changelog-file)
      [[ $# -ge 2 ]] || die "--changelog-file requires a value"
      changelog_file="$2"
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

[[ -n "$release_tag" ]] || die "missing required --tag (example: --tag v0.7.0)"
[[ "$release_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || die "invalid tag format: $release_tag"
[[ -f "$upgrades_file" ]] || die "upgrades file not found: $upgrades_file"
[[ -f "$changelog_file" ]] || die "changelog file not found: $changelog_file"

row_pattern="| \`$release_tag\` |"
row="$(grep -F "$row_pattern" "$upgrades_file" || true)"
[[ -n "$row" ]] || die "missing migration-table row for $release_tag in $upgrades_file"

escaped_tag="$(printf '%s' "$release_tag" | sed 's/[][\\/.*^$+?(){}|-]/\\&/g')"
tag_re="^## ${escaped_tag}($| )"
changelog_section="$(awk -v tag_re="$tag_re" '
  $0 ~ tag_re {
    in_section = 1
    next
  }
  in_section && $0 ~ /^## v[0-9]/ {
    exit
  }
  in_section {
    print
  }
' "$changelog_file")"

[[ -n "$changelog_section" ]] || die "no changelog section found for $release_tag in $changelog_file"

has_breaking_or_manual="0"
if printf '%s\n' "$changelog_section" | grep -Eqi 'breaking/manual|breaking[[:space:]-]+changes?|breaking:[[:space:]]*|manual (step|steps|action|actions|required)'; then
  has_breaking_or_manual="1"
fi

placeholder_pattern='No additional migration rules yet|None currently|Update this row when release-specific rules are defined|TBD|TODO'
if [[ "$has_breaking_or_manual" == "1" ]] && printf '%s\n' "$row" | grep -Eqi "$placeholder_pattern"; then
  die "row for $release_tag in $upgrades_file contains placeholder text, but $changelog_file indicates breaking/manual migration impact"
fi

"$script_dir/check-upgrade-ctas.sh"

echo "Upgrade docs check passed for $release_tag"
