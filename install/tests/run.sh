#!/usr/bin/env bash
set -Eeuo pipefail
cd "$(dirname "$0")/../.."

pass=0
check() { printf 'ok %d - %s\n' "$((++pass))" "$1"; }

bash -n install/install.sh install/agent.sh install/uninstall.sh
check "installer scripts parse as Bash"

grep -q 'NoNewPrivileges=true' packaging/systemd/routeglass.service
grep -q 'CapabilityBoundingSet=$' packaging/systemd/routeglass.service
grep -q 'AmbientCapabilities=CAP_NET_RAW' packaging/systemd/routeglass-agent.service
check "systemd capability boundaries are explicit"

if grep -REn 'iptables[[:space:]]+-F|nft[[:space:]]+flush|ufw[[:space:]]+disable|firewall-cmd[[:space:]]+--complete-reload' install packaging; then
  printf 'not ok - destructive firewall command found\n' >&2; exit 1
fi
check "no destructive firewall operation"

if grep -REn 'systemctl[[:space:]]+(stop|disable).*(nginx|caddy|xray)' install/install.sh; then
  printf 'not ok - installer stops an existing proxy service\n' >&2; exit 1
fi
check "installer does not stop existing proxy services"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
cat >"$tmp/routeglass" <<'EOF'
#!/usr/bin/env sh
exit 0
EOF
cat >"$tmp/systemctl" <<'EOF'
#!/usr/bin/env sh
exit 0
EOF
chmod +x "$tmp/routeglass" "$tmp/systemctl"

PATH="$tmp:$PATH" ROUTEGLASS_BINARY="$tmp/routeglass" \
ROUTEGLASS_UNIT_SOURCE="$PWD/packaging/systemd" \
bash install/install.sh server --dry-run >/dev/null
check "server dry-run completes without mutation"

PATH="$tmp:$PATH" ROUTEGLASS_BINARY="$tmp/routeglass" \
ROUTEGLASS_UNIT_SOURCE="$PWD/packaging/systemd" \
bash install/install.sh agent --server https://routeglass.example --join rgj_test --dry-run >/dev/null
check "agent dry-run completes without mutation"

if bash install/install.sh agent --dry-run >/dev/null 2>&1; then
  printf 'not ok - agent accepted missing join parameters\n' >&2; exit 1
fi
check "agent rejects missing join parameters"

printf '1..%d\n' "$pass"
