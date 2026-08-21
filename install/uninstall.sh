#!/usr/bin/env bash
set -Eeuo pipefail

PURGE=0
DRY_RUN=0
COMPONENT=all
SYSTEMD_DIR="${ROUTEGLASS_SYSTEMD_DIR:-/etc/systemd/system}"
INSTALL_BIN="${ROUTEGLASS_INSTALL_BIN:-/usr/local/bin/routeglass}"

run() { if (( DRY_RUN )); then printf '+ '; printf '%q ' "$@"; printf '\n'; else "$@"; fi; }
warn() { printf '! %s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Usage: uninstall.sh [server|agent|all] [--purge] [--dry-run]

By default service definitions are removed and data/configuration are retained.
--purge additionally deletes RouteGlass data, configuration, and service users.
Only firewall/proxy entries recorded or marked as RouteGlass-managed are removed.
EOF
}

if (($#)) && [[ "$1" != --* ]]; then COMPONENT=$1; shift; fi
while (($#)); do
  case "$1" in --purge) PURGE=1 ;; --dry-run) DRY_RUN=1 ;; -h|--help) usage; exit 0 ;; *) die "unknown option: $1" ;; esac
  shift
done
[[ "$COMPONENT" == server || "$COMPONENT" == agent || "$COMPONENT" == all ]] || die "component must be server, agent, or all"
if (( ! DRY_RUN )) && [[ ${EUID:-$(id -u)} -ne 0 ]]; then die "run as root"; fi

remove_firewall_state() {
  local file=$1 line kind rule
  [[ -f "$file" ]] || return 0
  while IFS= read -r line; do
    kind=${line%%:*}; rule=${line#*:}
    case "$kind" in
      ufw) command -v ufw >/dev/null && run ufw --force delete allow "$rule" || true ;;
      firewalld)
        if command -v firewall-cmd >/dev/null; then run firewall-cmd --permanent --remove-port="$rule" || true; fi
        ;;
    esac
  done <"$file"
  command -v firewall-cmd >/dev/null && run firewall-cmd --reload || true
  run rm -f "$file"
}

remove_unit() {
  local unit=$1
  run systemctl disable --now "$unit" 2>/dev/null || true
  run rm -f "$SYSTEMD_DIR/$unit"
  run rm -rf "$SYSTEMD_DIR/$unit.d"
}

if [[ "$COMPONENT" == server || "$COMPONENT" == all ]]; then
  remove_unit routeglass.service
  remove_firewall_state /var/lib/routeglass/install-firewall.state
  if [[ -f /etc/nginx/conf.d/routeglass.conf ]] && grep -q '^# Managed by RouteGlass' /etc/nginx/conf.d/routeglass.conf; then
    run rm -f /etc/nginx/conf.d/routeglass.conf
    command -v nginx >/dev/null && nginx -t && run systemctl reload nginx || warn "check nginx configuration before reload"
  fi
  if [[ -f /etc/caddy/conf.d/routeglass.caddy ]]; then
    run rm -f /etc/caddy/conf.d/routeglass.caddy
    command -v caddy >/dev/null && caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile && run systemctl reload caddy || true
  fi
  if (( PURGE )); then
    run rm -rf /etc/routeglass /var/lib/routeglass
    run rm -rf /usr/share/routeglass
    id routeglass >/dev/null 2>&1 && run userdel routeglass || true
  else
    printf '• Preserved /etc/routeglass and /var/lib/routeglass\n'
  fi
fi

if [[ "$COMPONENT" == agent || "$COMPONENT" == all ]]; then
  remove_unit routeglass-agent.service
  remove_firewall_state /var/lib/routeglass-agent/install-firewall.state
  if (( PURGE )); then
    run rm -rf /etc/routeglass-agent /var/lib/routeglass-agent
    id routeglass-agent >/dev/null 2>&1 && run userdel routeglass-agent || true
  else
    printf '• Preserved /etc/routeglass-agent and /var/lib/routeglass-agent\n'
  fi
fi

run systemctl daemon-reload
if ! systemctl is-enabled --quiet routeglass.service 2>/dev/null && ! systemctl is-enabled --quiet routeglass-agent.service 2>/dev/null; then
  run rm -f "$INSTALL_BIN"
fi
printf '✓ RouteGlass %s service removal complete\n' "$COMPONENT"
