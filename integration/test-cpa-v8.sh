#!/usr/bin/env bash
set -euo pipefail

# Run only against CPA v8.0.8's real host; never modify a deployed CPA instance.
root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
commit=fd48ea6840f5572deb53aeb5657740937ac9daaa

git clone --quiet --depth 1 --branch v8.0.8 \
  https://github.com/router-for-me/CLIProxyAPI.git "$work/host"
test "$(git -C "$work/host" rev-parse HEAD)" = "$commit"
mkdir -p "$work/plugins"
(cd "$root" && CGO_ENABLED=1 go build -trimpath -buildmode=c-shared \
  -ldflags '-X main.pluginVersion=0.0.0-integration' -o "$work/plugins/pi-usage-cpa.so" .)
cp "$root/integration/cpa_host_test.go" "$work/host/internal/pluginhost/pi_usage_cpa_integration_test.go"
# A c-shared Go runtime has its own environment snapshot: set these before startup.
(cd "$work/host" && PI_USAGE_CPA_TEST_PLUGIN_DIR="$work/plugins" \
  PI_USAGE_CPA_MANAGEMENT_KEY=synthetic-admin MANAGEMENT_PASSWORD= \
  PI_USAGE_CPA_MANAGEMENT_ORIGIN=http://127.0.0.1:1 PI_USAGE_CPA_GROUP_MAP= CGO_ENABLED=1 \
  go test -race -tags cpa_host_integration ./internal/pluginhost -run '^TestPiUsageCPAHostRegistration$' -count=1 -v)
