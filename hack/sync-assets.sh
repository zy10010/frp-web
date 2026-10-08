#!/usr/bin/env bash
#
# Regenerate the frp-manager assets that are derived from upstream frp source:
#   1. cmd/frp-manager/descriptions_gen.go  (field descriptions from doc comments)
#   2. cmd/frp-manager/examples/*.toml      (copies of conf/ example configs)
#
# Run after merging upstream so the web UI stays in sync with the frp version.
#
set -euo pipefail

cd "$(dirname "$0")/.."

go run ./hack/gen-desc

cp conf/frps_full_example.toml cmd/frp-manager/examples/frps_full_example.toml
cp conf/frpc_full_example.toml cmd/frp-manager/examples/frpc_full_example.toml

echo "frp-manager assets synced."
