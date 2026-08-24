#!/usr/bin/env bash
set -euo pipefail

repo_root="$PWD"
bin_dir="$(mktemp -d)"

go build -o "$bin_dir/sinq" "$repo_root/cmd/sinq"
(cd "$repo_root/demo" && go build -o server/demo-server ./server)

export PATH="$bin_dir:$PATH"

"$repo_root/demo/server/demo-server" &
disown

set +euo pipefail
