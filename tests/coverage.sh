#!/usr/bin/env bash
set -euo pipefail

# Coverage for parent root discovery logic (bashcov + bats).

if ! command -v bashcov > /dev/null 2>&1; then
  echo "ERROR: bashcov not found. Install with: gem install bashcov" >&2
  exit 1
fi
if ! command -v bats > /dev/null 2>&1; then
  echo "ERROR: bats not found. Install bats-core first." >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$ROOT_DIR"

rm -rf coverage
bashcov --root "$ROOT_DIR" -- bats tests/paths.bats

ruby - << 'RUBY'
require "json"

result_path = File.join(Dir.pwd, "coverage", ".resultset.json")
unless File.exist?(result_path)
  warn "coverage results not found at #{result_path}"
  exit 1
end

result = JSON.parse(File.read(result_path))
data = result.values.first
coverage = data.fetch("coverage", {})

targets = [
  "src/lib/parent-root.sh",
  "src/lib/temp-parent-root.sh"
]

def coverage_for(coverage, target)
  coverage.keys.find do |key|
    key == target || key.end_with?(File::SEPARATOR + target)
  end
end

targets.each do |target|
  key = coverage_for(coverage, target)
  unless key
    warn "coverage missing for #{target}"
    exit 1
  end
  lines = coverage[key]
  total = lines.count { |v| !v.nil? }
  covered = lines.count { |v| v && v > 0 }
  percent = total.zero? ? 0.0 : (covered.to_f / total * 100.0)
  if percent < 100.0
    warn format("coverage for %s below 100%%: %.2f%%", target, percent)
    exit 1
  end
end

puts "coverage ok: 100% for parent-root.sh and temp-parent-root.sh"
RUBY
