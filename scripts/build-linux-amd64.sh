#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output_dir="${1:?Usage: scripts/build-linux-amd64.sh OUTPUT_DIR}"
expected_go="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["go"])' "$repo_root/infra/nanos/versions.json")"

export GOTOOLCHAIN="${GOTOOLCHAIN:-go${expected_go}}"
actual_go="$(go env GOVERSION)"
if [[ "$actual_go" != "go${expected_go}" ]]; then
	printf 'Expected Go %s from infra/nanos/versions.json, got %s\n' "$expected_go" "$actual_go" >&2
	exit 1
fi

mkdir -p "$output_dir"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
	go build -trimpath -ldflags='-s -w' \
	-o "$output_dir/server-linux-amd64" "$repo_root/cmd/server"
