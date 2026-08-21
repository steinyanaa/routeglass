#!/usr/bin/env bash
set -Eeuo pipefail

PROGRAM=routeglass
DEFAULT_REPO="${ROUTEGLASS_REPO:-steinyanaa/routeglass}"
INSTALL_BIN="${ROUTEGLASS_INSTALL_BIN:-/usr/local/bin/routeglass}"
SYSTEMD_DIR="${ROUTEGLASS_SYSTEMD_DIR:-/etc/systemd/system}"
SERVER_CONFIG="${ROUTEGLASS_SERVER_CONFIG:-/etc/routeglass/server.json}"
AGENT_CONFIG="${ROUTEGLASS_AGENT_CONFIG:-/etc/routeglass-agent/agent.json}"
SERVER_STATE="${ROUTEGLASS_SERVER_STATE:-/var/lib/routeglass}"
AGENT_STATE="${ROUTEGLASS_AGENT_STATE:-/var/lib/routeglass-agent}"
SERVER_UNIT=routeglass.service
AGENT_UNIT=routeglass-agent.service
DRY_RUN=0
CONFIGURE_FIREWALL=0
CONFIGURE_PROXY=""
VERSION="${ROUTEGLASS_VERSION:-latest}"
MANIFEST_URL=""
SERVER_ORIGIN="${ROUTEGLASS_SERVER_ORIGIN:-}"
DOMAIN=""
LISTEN="127.0.0.1:8765"
AGENT_LISTEN=":9443"
JOIN_TOKEN=""
NODE_NAME=""
AGENT_ENDPOINT=""
TLS_CERT=""
TLS_KEY=""
TLS_MODE=auto
ACME_CHALLENGE=auto
TLS_PROVIDER=""
ACME_EMAIL=""
ACME_CA_URL=""
ACME_HTTP_ADDRESS=""
ACME_NAMES=()
TMPDIR_RG=""
ADMIN_PASSWORD=""

say() { printf '%s\n' "$*"; }
info() { printf '• %s\n' "$*"; }
ok() { printf '✓ %s\n' "$*"; }
warn() { printf '! %s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
quote_cmd() { printf '%q ' "$@"; printf '\n'; }
run() {
  if (( DRY_RUN )); then printf '+ '; quote_cmd "$@"; else "$@"; fi
}

cleanup() {
  if [[ -n "$TMPDIR_RG" && -d "$TMPDIR_RG" ]]; then rm -rf -- "$TMPDIR_RG"; fi
}
trap cleanup EXIT

usage() {
  cat <<'EOF'
RouteGlass installer

Usage:
  install.sh server [options]
  install.sh agent --server URL --join TOKEN [options]
  install.sh update [server|agent] [--manifest URL]

Common options:
  --version VERSION          Install a tagged release instead of latest
  --repo OWNER/REPO          GitHub repository (or ROUTEGLASS_REPO)
  --dry-run                  Print changes without applying them
  --configure-firewall       Add only required UFW/firewalld rules

Server options:
  --domain DOMAIN            Public origin name; proxy configuration is still opt-in
  --listen ADDRESS           Default: 127.0.0.1:8765
  --configure-proxy TYPE     TYPE is nginx or caddy; installs a separate managed snippet

Agent options:
  --server URL               Canonical RouteGlass Server origin
  --join TOKEN               One-time node join token
  --listen ADDRESS           Default: :9443
  --name NAME                Node display name (default: host name)
  --endpoint URL             Browser-facing HTTPS endpoint
  --tls MODE                 auto, ip, domain, or files
  --tls-cert PATH            Trusted certificate chain for files mode
  --tls-key PATH             Matching private key for files mode
  --acme-name NAME           IP/domain identifier (repeatable)
  --acme-email EMAIL         ACME account contact
  --acme-ca URL              Override ACME directory (for staging/tests)
  --acme-challenge MODE      auto, http-01, proxy-http-01, or tls-alpn-01

Environment for packaging/tests:
  ROUTEGLASS_BINARY=/path    Install a local binary and skip release download
  ROUTEGLASS_UNIT_SOURCE=DIR Override systemd template directory
EOF
}

need_root() {
  if (( ! DRY_RUN )) && [[ ${EUID:-$(id -u)} -ne 0 ]]; then
    die "run as root (for curl pipelines, place sudo before bash)"
  fi
}

require_command() { command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"; }

detect_platform() {
  local os_release=${ROUTEGLASS_OS_RELEASE:-/etc/os-release}
  [[ -r "$os_release" ]] || die "missing $os_release"
  # shellcheck disable=SC1091
  . "$os_release"
  case "${ID:-}" in debian|ubuntu) DISTRO=$ID ;; *) die "supported distributions: Debian and Ubuntu (found ${ID:-unknown})" ;; esac
  command -v systemctl >/dev/null 2>&1 || die "systemd is required"
  case "$(uname -m)" in
    x86_64|amd64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) die "supported architectures: amd64 and arm64 (found $(uname -m))" ;;
  esac
}

listener() {
  local port=$1
  if command -v ss >/dev/null 2>&1; then
    ss -H -ltnp 2>/dev/null | awk -v p=":$port" '$4 ~ p "$" {print; found=1} END {exit !found}'
  elif command -v lsof >/dev/null 2>&1; then
    lsof -nP -iTCP:"$port" -sTCP:LISTEN 2>/dev/null
  else
    return 1
  fi
}

inventory() {
  say "RouteGlass preflight"
  info "System       $DISTRO / $ARCH"
  local p out
  for p in 80 443 8765 9443; do
    out=$(listener "$p" 2>/dev/null || true)
    if [[ -n "$out" ]]; then info "TCP/$p       in use: ${out%%$'\n'*}"; else info "TCP/$p       free"; fi
  done
  local service
  for service in nginx caddy apache2 xray; do
    if systemctl is-active --quiet "$service" 2>/dev/null; then info "Service      $service active"; fi
  done
  if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q '^Status: active'; then
    FIREWALL=ufw
  elif command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --state >/dev/null 2>&1; then
    FIREWALL=firewalld
  elif command -v nft >/dev/null 2>&1; then
    FIREWALL=nftables
  else
    FIREWALL=none
  fi
  info "Firewall     $FIREWALL"
}

make_temp() { TMPDIR_RG=$(mktemp -d "${TMPDIR:-/tmp}/routeglass.XXXXXXXX"); }

resolve_version() {
  if [[ "$VERSION" != latest ]]; then
    [[ "$VERSION" == v* ]] || VERSION="v$VERSION"
    [[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] \
      || die "invalid release version: $VERSION"
    return
  fi
  require_command curl
  local effective
  effective=$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/$DEFAULT_REPO/releases/latest") \
    || die "failed to resolve latest GitHub release"
  VERSION=${effective##*/}
  [[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] \
    || die "unexpected latest release tag: $VERSION"
}

install_release_binary() {
  if [[ -n "${ROUTEGLASS_BINARY:-}" ]]; then
    [[ -f "$ROUTEGLASS_BINARY" ]] || die "ROUTEGLASS_BINARY is not a file"
    run install -Dm0755 "$ROUTEGLASS_BINARY" "$INSTALL_BIN.new"
    run mv -f "$INSTALL_BIN.new" "$INSTALL_BIN"
    return
  fi

  resolve_version
  make_temp
  local base="https://github.com/$DEFAULT_REPO/releases/download/$VERSION"
  local archive="routeglass_${VERSION}_linux_${ARCH}.tar.gz"
  info "Release      $VERSION"
  curl -fL --retry 3 --retry-delay 2 -o "$TMPDIR_RG/$archive" "$base/$archive"
  curl -fL --retry 3 --retry-delay 2 -o "$TMPDIR_RG/SHA256SUMS" "$base/SHA256SUMS"
  local expected actual
  expected=$(awk -v file="$archive" '$2 == file || $2 == "*" file {print $1}' "$TMPDIR_RG/SHA256SUMS")
  [[ "$expected" =~ ^[0-9a-fA-F]{64}$ ]] || die "release checksum entry missing for $archive"
  actual=$(sha256sum "$TMPDIR_RG/$archive" | awk '{print $1}')
  [[ "${actual,,}" == "${expected,,}" ]] || die "SHA256 mismatch for $archive"
  tar -xzf "$TMPDIR_RG/$archive" -C "$TMPDIR_RG"
  [[ -f "$TMPDIR_RG/routeglass" ]] || die "release archive does not contain routeglass"
  "$TMPDIR_RG/routeglass" version >/dev/null || die "downloaded binary did not pass version check"
  run install -Dm0755 "$TMPDIR_RG/routeglass" "$INSTALL_BIN.new"
  run mv -f "$INSTALL_BIN.new" "$INSTALL_BIN"
  ok "Binary       $INSTALL_BIN"
}

create_user_and_dirs() {
  local user=$1 state=$2 config_dir=$3
  if ! id "$user" >/dev/null 2>&1; then
    run useradd --system --home-dir "$state" --create-home --shell /usr/sbin/nologin "$user"
  fi
  run install -d -m0750 -o "$user" -g "$user" "$state"
  run install -d -m0750 -o root -g "$user" "$config_dir"
}

json_escape() {
  local s=$1
  s=${s//\\/\\\\}; s=${s//\"/\\\"}; s=${s//$'\n'/\\n}; s=${s//$'\r'/\\r}; s=${s//$'\t'/\\t}
  printf '%s' "$s"
}

write_server_config() {
  [[ -e "$SERVER_CONFIG" ]] && { info "Config       preserving $SERVER_CONFIG"; return; }
  local origin=""
  local insecure=false
  if [[ -n "$DOMAIN" ]]; then origin="https://$DOMAIN"; else origin="http://127.0.0.1:8765"; insecure=true; fi
  local content
  content=$(cat <<EOF
{
  "listen": "$(json_escape "$LISTEN")",
  "data_dir": "$(json_escape "$SERVER_STATE")",
  "public_origin": "$(json_escape "$origin")",
  "trusted_proxies": [],
  "insecure_cookies": $insecure
}
EOF
)
  if (( DRY_RUN )); then info "Would write    $SERVER_CONFIG"; else
    printf '%s\n' "$content" >"$SERVER_CONFIG"
    chown root:routeglass "$SERVER_CONFIG"; chmod 0640 "$SERVER_CONFIG"
  fi
}

write_agent_config() {
  [[ -e "$AGENT_CONFIG" ]] && { info "Config       preserving $AGENT_CONFIG"; return; }
  local content names_json="[" sep="" n
  for n in "${ACME_NAMES[@]}"; do names_json+="$sep\"$(json_escape "$n")\""; sep=","; done
  names_json+="]"
  content=$(cat <<EOF
{
  "listen": "$(json_escape "$AGENT_LISTEN")",
  "data_dir": "$(json_escape "$AGENT_STATE")",
  "server_url": "$(json_escape "$SERVER_ORIGIN")",
  "join_token": "$(json_escape "$JOIN_TOKEN")",
  "name": "$(json_escape "$NODE_NAME")",
  "endpoint": "$(json_escape "$AGENT_ENDPOINT")",
  "allowed_origin": "$(json_escape "$SERVER_ORIGIN")",
  "tls_cert": "$(json_escape "$TLS_CERT")",
  "tls_key": "$(json_escape "$TLS_KEY")",
  "tls_provider": "$(json_escape "$TLS_PROVIDER")",
  "acme_email": "$(json_escape "$ACME_EMAIL")",
  "acme_names": $names_json,
  "acme_http_address": "$(json_escape "$ACME_HTTP_ADDRESS")",
  "acme_ca_url": "$(json_escape "$ACME_CA_URL")"
}
EOF
)
  if (( DRY_RUN )); then info "Would write    $AGENT_CONFIG with redacted join token"; else
    printf '%s\n' "$content" >"$AGENT_CONFIG"
    chown routeglass-agent:routeglass-agent "$AGENT_CONFIG"; chmod 0600 "$AGENT_CONFIG"
  fi
}

initialize_server() {
  [[ -f "$SERVER_STATE/secrets.json" ]] && return 0
  (( DRY_RUN )) && { info "Would initialize database and one-time admin credential"; return 0; }
  local captured="$TMPDIR_RG/server-first-start.log" rc=0
  install -m0600 /dev/null "$captured"
  # First boot creates SQLite, secrets, and a mode-0600 one-time credential file.
  # Run it outside systemd so initialization diagnostics never enter journald.
  timeout --signal=TERM 4s runuser -u routeglass -- \
    "$INSTALL_BIN" server --config "$SERVER_CONFIG" >"$captured" 2>&1 || rc=$?
  if [[ $rc -ne 0 && $rc -ne 124 && $rc -ne 143 ]]; then
    cat "$captured" >&2; die "server initialization failed"
  fi
  rm -f "$captured"
  local password_file="$SERVER_STATE/initial-admin-password"
  [[ -r "$password_file" ]] || die "server initialized without creating a temporary admin credential"
  ADMIN_PASSWORD=$(<"$password_file")
  rm -f -- "$password_file"
  [[ -n "$ADMIN_PASSWORD" ]] || die "temporary admin credential was empty"
}

unit_source_dir() {
  if [[ -n "${ROUTEGLASS_UNIT_SOURCE:-}" ]]; then printf '%s' "$ROUTEGLASS_UNIT_SOURCE"; return; fi
  local here="" script=${BASH_SOURCE[0]:-}
  if [[ -n "$script" && -f "$script" ]]; then here=$(cd -- "$(dirname -- "$script")" && pwd); fi
  if [[ -n "$here" && -d "$here/../packaging/systemd" ]]; then printf '%s' "$here/../packaging/systemd"; return; fi
  printf '%s' "$TMPDIR_RG/packaging/systemd"
}

install_unit() {
  local name=$1 source
  source="$(unit_source_dir)/$name"
  if [[ ! -f "$source" ]]; then
    [[ -n "$TMPDIR_RG" ]] || make_temp
    local raw="https://raw.githubusercontent.com/$DEFAULT_REPO/$VERSION/packaging/systemd/$name"
    curl -fL --retry 3 -o "$TMPDIR_RG/$name" "$raw"
    source="$TMPDIR_RG/$name"
  fi
  run install -Dm0644 "$source" "$SYSTEMD_DIR/$name"
}

install_agent_bootstrap() {
  local here="" source="" script=${BASH_SOURCE[0]:-}
  if [[ -n "$script" && -f "$script" ]]; then here=$(cd -- "$(dirname -- "$script")" && pwd); source="$here/agent.sh"; fi
  if [[ ! -f "$source" ]]; then
    [[ -n "$TMPDIR_RG" ]] || make_temp
    curl -fL --retry 3 -o "$TMPDIR_RG/agent.sh" \
      "https://raw.githubusercontent.com/$DEFAULT_REPO/$VERSION/install/agent.sh"
    source="$TMPDIR_RG/agent.sh"
  fi
  run install -Dm0755 "$source" /usr/share/routeglass/agent.sh
}

install_agent_tools() {
  local packages=()
  command -v traceroute >/dev/null 2>&1 || packages+=(traceroute)
  command -v mtr >/dev/null 2>&1 || packages+=(mtr-tiny)
  command -v ip >/dev/null 2>&1 || packages+=(iproute2)
  ((${#packages[@]})) || return 0
  info "Packages      ${packages[*]}"
  run env DEBIAN_FRONTEND=noninteractive apt-get update
  run env DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "${packages[@]}"
}

discover_public_ip() {
  local ip
  while read -r ip; do
    [[ -n "$ip" ]] || continue
    case "$ip" in 10.*|127.*|169.254.*|192.168.*|100.6[4-9].*|100.[7-9][0-9].*|100.1[01][0-9].*|100.12[0-7].*) continue ;; esac
    if [[ "$ip" =~ ^172\.([0-9]+)\. ]] && (( BASH_REMATCH[1] >= 16 && BASH_REMATCH[1] <= 31 )); then continue; fi
    printf '%s' "$ip"; return 0
  done < <(ip -o -4 addr show scope global 2>/dev/null | awk '{sub(/\/.*/,"",$4); print $4}')
  while read -r ip; do
    [[ -n "$ip" ]] || continue
    case "${ip,,}" in fc*|fd*|fe8*|fe9*|fea*|feb*) continue ;; esac
    printf '%s' "$ip"; return 0
  done < <(ip -o -6 addr show scope global 2>/dev/null | awk '{sub(/\/.*/,"",$4); print $4}')
  return 1
}

prepare_agent_tls() {
  local identifier host
  case "$TLS_MODE" in
    files)
      TLS_PROVIDER=files
      ;;
    auto|ip)
      if ((${#ACME_NAMES[@]})); then identifier=${ACME_NAMES[0]}; else identifier=$(discover_public_ip || true); fi
      if [[ -z "$identifier" ]]; then
        [[ "$TLS_MODE" == auto ]] && { warn "No public interface IP detected; configure a trusted node certificate in Admin"; return; }
        die "IP TLS mode requires a public interface IP or --acme-name"
      fi
      if [[ "$ACME_CHALLENGE" == auto ]] && listener 80 >/dev/null 2>&1; then
        [[ "$TLS_MODE" == auto ]] && { warn "TCP/80 is occupied; automatic public-IP TLS is pending an explicit challenge proxy"; return; }
        die "TCP/80 is occupied; use --acme-challenge proxy-http-01 with an existing proxy route"
      fi
      TLS_PROVIDER=acme-ip
      ACME_NAMES=("$identifier")
      TLS_CERT="$AGENT_STATE/tls/fullchain.pem"
      TLS_KEY="$AGENT_STATE/tls/private-key.pem"
      [[ "$ACME_CHALLENGE" == auto ]] && ACME_CHALLENGE=http-01
      if [[ "$identifier" == *:* ]]; then host="[$identifier]"; else host=$identifier; fi
      [[ -n "$AGENT_ENDPOINT" ]] || AGENT_ENDPOINT="https://$host:${AGENT_LISTEN##*:}"
      ;;
    domain)
      ((${#ACME_NAMES[@]})) || die "domain TLS mode requires --acme-name node.example.com"
      TLS_PROVIDER=acme-domain
      TLS_CERT="$AGENT_STATE/tls/fullchain.pem"
      TLS_KEY="$AGENT_STATE/tls/private-key.pem"
      [[ "$ACME_CHALLENGE" == auto ]] && ACME_CHALLENGE=http-01
      [[ -n "$AGENT_ENDPOINT" ]] || AGENT_ENDPOINT="https://${ACME_NAMES[0]}:${AGENT_LISTEN##*:}"
      ;;
  esac
  case "$ACME_CHALLENGE" in
    http-01) ACME_HTTP_ADDRESS=":80" ;;
    proxy-http-01) ACME_HTTP_ADDRESS="127.0.0.1:9180" ;;
    tls-alpn-01) die "this release's embedded ACME provider uses HTTP-01; select http-01 or proxy-http-01" ;;
  esac
  if [[ "$ACME_HTTP_ADDRESS" == :80 ]] && listener 80 >/dev/null 2>&1; then
    die "TCP/80 is occupied; use proxy-http-01 with an explicit existing-proxy challenge route"
  fi
}

configure_firewall_port() {
  local port=$1 state_file=$2
  (( CONFIGURE_FIREWALL )) || { warn "Firewall unchanged; allow TCP/$port if your policy blocks it"; return; }
  case "$FIREWALL" in
    ufw)
      if ! ufw status | grep -Eq "^${port}/tcp .*RouteGlass"; then
        run ufw allow "$port/tcp" comment RouteGlass
        (( DRY_RUN )) || printf 'ufw:%s/tcp\n' "$port" >>"$state_file"
      fi
      ;;
    firewalld)
      if ! firewall-cmd --permanent --query-port="$port/tcp" >/dev/null; then
        run firewall-cmd --permanent --add-port="$port/tcp"
        run firewall-cmd --reload
        (( DRY_RUN )) || printf 'firewalld:%s/tcp\n' "$port" >>"$state_file"
      fi
      ;;
    nftables)
      warn "Native nftables policy detected; no generic rules were changed. Allow TCP/$port in your existing input chain."
      ;;
    none) info "Firewall     no active supported frontend" ;;
  esac
}

configure_proxy_server() {
  [[ -n "$CONFIGURE_PROXY" ]] || return 0
  [[ -n "$DOMAIN" ]] || die "--configure-proxy requires --domain"
  case "$CONFIGURE_PROXY" in
    nginx)
      require_command nginx
      local file=/etc/nginx/conf.d/routeglass.conf backup=""
      [[ -e "$file" ]] && { backup="$file.routeglass-backup.$(date +%s)"; run cp -a "$file" "$backup"; }
      local tmp; tmp=$(mktemp)
      cat >"$tmp" <<EOF
# Managed by RouteGlass. Remove with install/uninstall.sh.
server {
    listen 80;
    listen [::]:80;
    server_name $DOMAIN;
    location / {
        proxy_pass http://127.0.0.1:8765;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
EOF
      run install -m0644 "$tmp" "$file"; rm -f "$tmp"
      if (( ! DRY_RUN )) && ! nginx -t; then
        [[ -n "$backup" ]] && mv -f "$backup" "$file" || rm -f "$file"
        die "nginx validation failed; previous configuration restored"
      fi
      if (( DRY_RUN )); then run systemctl reload nginx
      elif ! systemctl reload nginx; then
        [[ -n "$backup" ]] && mv -f "$backup" "$file" || rm -f "$file"
        nginx -t >/dev/null 2>&1 && systemctl reload nginx || true
        die "nginx reload failed; previous configuration restored"
      else
        [[ -n "$backup" ]] && rm -f "$backup"
      fi
      ok "Proxy        nginx HTTP route installed; add trusted HTTPS before entering credentials"
      ;;
    caddy)
      require_command caddy
      local dir=/etc/caddy/conf.d file=/etc/caddy/conf.d/routeglass.caddy caddy_backup=""
      grep -Eq '^[[:space:]]*import[[:space:]]+conf\.d/\*' /etc/caddy/Caddyfile \
        || die "Caddyfile does not import conf.d/*; add the shown reverse_proxy block manually"
      run install -d -m0755 "$dir"
      [[ -e "$file" ]] && { caddy_backup="$file.routeglass-backup.$(date +%s)"; run cp -a "$file" "$caddy_backup"; }
      local tmp; tmp=$(mktemp)
      printf '%s {\n    reverse_proxy 127.0.0.1:8765\n}\n' "$DOMAIN" >"$tmp"
      run install -m0644 "$tmp" "$file"; rm -f "$tmp"
      if (( ! DRY_RUN )) && ! caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile; then
        [[ -n "$caddy_backup" ]] && mv -f "$caddy_backup" "$file" || rm -f "$file"
        die "Caddy validation failed; previous configuration restored"
      fi
      if (( DRY_RUN )); then run systemctl reload caddy
      elif ! systemctl reload caddy; then
        [[ -n "$caddy_backup" ]] && mv -f "$caddy_backup" "$file" || rm -f "$file"
        caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1 && systemctl reload caddy || true
        die "Caddy reload failed; previous configuration restored"
      else
        [[ -n "$caddy_backup" ]] && rm -f "$caddy_backup"
      fi
      ok "Proxy        Caddy HTTPS site installed"
      ;;
    *) die "--configure-proxy must be nginx or caddy" ;;
  esac
}

wait_server() {
  local url="http://127.0.0.1:8765/healthz" i
  for i in {1..30}; do curl -fsS "$url" >/dev/null 2>&1 && return 0; sleep 1; done
  return 1
}

install_server() {
  if [[ -n "$DOMAIN" ]]; then
    [[ "$DOMAIN" =~ ^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$ && "$DOMAIN" == *.* ]] \
      || die "--domain must be a valid fully qualified host name"
  fi
  listener 8765 >/dev/null 2>&1 && ! systemctl is-active --quiet "$SERVER_UNIT" 2>/dev/null \
    && die "TCP/8765 is already owned by another process"
  install_release_binary
  create_user_and_dirs routeglass "$SERVER_STATE" "$(dirname "$SERVER_CONFIG")"
  write_server_config
  [[ -n "$TMPDIR_RG" ]] || make_temp
  initialize_server
  install_unit "$SERVER_UNIT"
  install_agent_bootstrap
  run systemctl daemon-reload
  run systemctl enable --now "$SERVER_UNIT"
  if (( ! DRY_RUN )); then
    if wait_server; then ok "Service      running"; else
      journalctl -u "$SERVER_UNIT" -n 30 --no-pager >&2 || true
      die "server health check failed; inspect: journalctl -u $SERVER_UNIT"
    fi
  fi
  configure_proxy_server
  if [[ "$LISTEN" != 127.0.0.1:* && "$LISTEN" != localhost:* && "$LISTEN" != \[::1\]:* ]]; then
    configure_firewall_port "${LISTEN##*:}" "$SERVER_STATE/install-firewall.state"
  fi
  if [[ -n "$CONFIGURE_PROXY" ]]; then
    configure_firewall_port 80 "$SERVER_STATE/install-firewall.state"
    [[ "$CONFIGURE_PROXY" == caddy ]] && configure_firewall_port 443 "$SERVER_STATE/install-firewall.state"
  fi
  say; say "RouteGlass Server"; ok "Installed     $INSTALL_BIN"; ok "Database      initialized by server"
  if [[ -n "$DOMAIN" && "$CONFIGURE_PROXY" == caddy ]]; then
    info "Admin         https://$DOMAIN/admin"
  else
    info "Local admin   http://127.0.0.1:8765/admin"
    info "SSH tunnel    ssh -L 8765:127.0.0.1:8765 root@SERVER"
  fi
  if [[ -n "$ADMIN_PASSWORD" ]]; then
    info "Username      admin"
    info "Temporary password: $ADMIN_PASSWORD"
    warn "Change this password at first login; it will not be printed again"
  fi
}

install_agent() {
  [[ -n "$SERVER_ORIGIN" ]] || die "agent installation requires --server URL"
  [[ "$SERVER_ORIGIN" == https://* ]] || die "--server must use https://"
  [[ -n "$JOIN_TOKEN" ]] || die "agent installation requires --join TOKEN"
  if [[ -z "$NODE_NAME" ]]; then NODE_NAME=$(hostname); NODE_NAME=${NODE_NAME%%.*}; fi
  if [[ "$TLS_MODE" == files ]]; then
    [[ -f "$TLS_CERT" && -f "$TLS_KEY" ]] || die "files TLS mode requires --tls-cert and --tls-key"
    [[ "$AGENT_ENDPOINT" == https://* ]] || die "files TLS mode requires an https:// --endpoint"
  fi
  install_agent_tools
  prepare_agent_tls
  listener "${AGENT_LISTEN##*:}" >/dev/null 2>&1 && ! systemctl is-active --quiet "$AGENT_UNIT" 2>/dev/null \
    && die "Agent listen port is already owned by another process"
  install_release_binary
  create_user_and_dirs routeglass-agent "$AGENT_STATE" "$(dirname "$AGENT_CONFIG")"
  run chown routeglass-agent:routeglass-agent "$(dirname "$AGENT_CONFIG")"
  write_agent_config
  install_unit "$AGENT_UNIT"
  if [[ "$ACME_HTTP_ADDRESS" == :80 ]] && ! listener 80 >/dev/null 2>&1; then
    local src="$(unit_source_dir)/routeglass-agent-acme-http.conf"
    run install -d -m0755 "$SYSTEMD_DIR/$AGENT_UNIT.d"
    run install -m0644 "$src" "$SYSTEMD_DIR/$AGENT_UNIT.d/acme-http.conf"
    configure_firewall_port 80 "$AGENT_STATE/install-firewall.state"
  elif [[ "$ACME_HTTP_ADDRESS" == :80 ]]; then
    die "TCP/80 is occupied; select proxy-http-01 or configure a domain certificate"
  fi
  configure_firewall_port "${AGENT_LISTEN##*:}" "$AGENT_STATE/install-firewall.state"
  run systemctl daemon-reload
  run systemctl enable --now "$AGENT_UNIT"
  if (( ! DRY_RUN )); then
    sleep 2
    if ! systemctl is-active --quiet "$AGENT_UNIT"; then
      journalctl -u "$AGENT_UNIT" -n 30 --no-pager >&2 || true
      die "agent did not stay active"
    fi
    # A successful join rewrites the config without the one-time token. If an older
    # binary leaves it in place, remove only that JSON field after stopping service.
  fi
  JOIN_TOKEN=""
  say; say "RouteGlass Agent"; ok "Server        $SERVER_ORIGIN"; ok "Service       active"
  if [[ -n "$AGENT_ENDPOINT" ]]; then
    info "Endpoint      $AGENT_ENDPOINT"
  else
    info "Endpoint      pending trusted TLS discovery; inspect the node TLS status in Admin"
  fi
  info "Diagnostics   $INSTALL_BIN doctor --config $AGENT_CONFIG"
}

update_installation() {
  local component=${1:-}
  if [[ -z "$component" ]]; then
    if systemctl is-enabled --quiet "$SERVER_UNIT" 2>/dev/null; then component=server
    elif systemctl is-enabled --quiet "$AGENT_UNIT" 2>/dev/null; then component=agent
    else die "no managed RouteGlass service detected"
    fi
  fi
  [[ "$component" == server || "$component" == agent ]] || die "update component must be server or agent"
  [[ -x "$INSTALL_BIN" ]] || die "RouteGlass binary not installed at $INSTALL_BIN"
  [[ -n "$MANIFEST_URL" ]] || MANIFEST_URL="https://github.com/$DEFAULT_REPO/releases/latest/download/release.json"
  (( DRY_RUN )) && { run "$INSTALL_BIN" update --component "$component" --manifest "$MANIFEST_URL"; return; }

  make_temp
  local target_version current_version oldest
  curl -fsSL --retry 3 -o "$TMPDIR_RG/release.json" "$MANIFEST_URL" || die "failed to download release manifest"
  target_version=$(sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$TMPDIR_RG/release.json" | head -n1)
  current_version=$("$INSTALL_BIN" version | awk '{print $2}')
  [[ "$target_version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "release manifest has an invalid version"
  if [[ "$target_version" == "$current_version" ]]; then ok "Update        already on $current_version"; return; fi
  oldest=$(printf '%s\n%s\n' "$target_version" "$current_version" | sort -V | head -n1)
  [[ "$oldest" == "$current_version" ]] || die "release manifest would downgrade $current_version to $target_version"

  local was_server=0 was_agent=0 failed=0
  systemctl is-active --quiet "$SERVER_UNIT" 2>/dev/null && was_server=1
  systemctl is-active --quiet "$AGENT_UNIT" 2>/dev/null && was_agent=1
  cp -a "$INSTALL_BIN" "$TMPDIR_RG/routeglass.previous"
  if [[ -f "$SERVER_STATE/routeglass.db" ]]; then
    (( was_server )) && systemctl stop "$SERVER_UNIT"
    cp -a "$SERVER_STATE/routeglass.db" "$TMPDIR_RG/routeglass.db.previous"
    [[ -f "$SERVER_STATE/routeglass.db-wal" ]] && cp -a "$SERVER_STATE/routeglass.db-wal" "$TMPDIR_RG/routeglass.db-wal.previous"
    [[ -f "$SERVER_STATE/routeglass.db-shm" ]] && cp -a "$SERVER_STATE/routeglass.db-shm" "$TMPDIR_RG/routeglass.db-shm.previous"
  fi
  (( was_agent )) && systemctl stop "$AGENT_UNIT"
  # A single binary serves both components; stop both before atomic replacement.
  (( was_server )) && systemctl stop "$SERVER_UNIT" 2>/dev/null || true

  "$INSTALL_BIN" update --component "$component" --manifest "$MANIFEST_URL" || failed=1
  if (( ! failed )); then
    (( was_server )) && systemctl start "$SERVER_UNIT"
    (( was_agent )) && systemctl start "$AGENT_UNIT"
    if (( was_server )) && ! wait_server; then failed=1; fi
    if (( was_agent )) && ! systemctl is-active --quiet "$AGENT_UNIT"; then failed=1; fi
  fi
  if (( failed )); then
    warn "Update health check failed; restoring previous binary and database"
    systemctl stop "$SERVER_UNIT" "$AGENT_UNIT" 2>/dev/null || true
    install -m0755 "$TMPDIR_RG/routeglass.previous" "$INSTALL_BIN"
    if [[ -f "$TMPDIR_RG/routeglass.db.previous" ]]; then
      rm -f "$SERVER_STATE/routeglass.db" "$SERVER_STATE/routeglass.db-wal" "$SERVER_STATE/routeglass.db-shm"
      install -o routeglass -g routeglass -m0600 "$TMPDIR_RG/routeglass.db.previous" "$SERVER_STATE/routeglass.db"
      [[ -f "$TMPDIR_RG/routeglass.db-wal.previous" ]] && install -o routeglass -g routeglass -m0600 "$TMPDIR_RG/routeglass.db-wal.previous" "$SERVER_STATE/routeglass.db-wal"
      [[ -f "$TMPDIR_RG/routeglass.db-shm.previous" ]] && install -o routeglass -g routeglass -m0600 "$TMPDIR_RG/routeglass.db-shm.previous" "$SERVER_STATE/routeglass.db-shm"
    fi
    (( was_server )) && systemctl start "$SERVER_UNIT"
    (( was_agent )) && systemctl start "$AGENT_UNIT"
    die "update rolled back"
  fi
  rm -f "$INSTALL_BIN.bak"
  ok "Update        $component update completed and health checked"
}

MODE=${1:-}
[[ -n "$MODE" ]] || { usage; exit 2; }
shift
UPDATE_COMPONENT=""
if [[ "$MODE" == update && $# -gt 0 && "$1" != --* ]]; then UPDATE_COMPONENT=$1; shift; fi
while (($#)); do
  case "$1" in
    --repo) DEFAULT_REPO=${2:?missing value}; shift 2 ;;
    --version) VERSION=${2:?missing value}; shift 2 ;;
    --domain) DOMAIN=${2:?missing value}; shift 2 ;;
    --listen) if [[ "$MODE" == agent ]]; then AGENT_LISTEN=${2:?missing value}; else LISTEN=${2:?missing value}; fi; shift 2 ;;
    --server) SERVER_ORIGIN=${2:?missing value}; shift 2 ;;
    --join) JOIN_TOKEN=${2:?missing value}; shift 2 ;;
    --name) NODE_NAME=${2:?missing value}; shift 2 ;;
    --endpoint) AGENT_ENDPOINT=${2:?missing value}; shift 2 ;;
    --tls) TLS_MODE=${2:?missing value}; shift 2 ;;
    --tls-cert) TLS_CERT=${2:?missing value}; shift 2 ;;
    --tls-key) TLS_KEY=${2:?missing value}; shift 2 ;;
    --acme-name) ACME_NAMES+=("${2:?missing value}"); shift 2 ;;
    --acme-email) ACME_EMAIL=${2:?missing value}; shift 2 ;;
    --acme-ca) ACME_CA_URL=${2:?missing value}; shift 2 ;;
    --acme-challenge) ACME_CHALLENGE=${2:?missing value}; shift 2 ;;
    --configure-proxy) CONFIGURE_PROXY=${2:?missing value}; shift 2 ;;
    --configure-firewall) CONFIGURE_FIREWALL=1; shift ;;
    --manifest) MANIFEST_URL=${2:?missing value}; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown option: $1" ;;
  esac
done

case "$TLS_MODE" in auto|ip|domain|files) ;; *) die "invalid --tls mode" ;; esac
case "$ACME_CHALLENGE" in auto|http-01|proxy-http-01|tls-alpn-01) ;; *) die "invalid --acme-challenge" ;; esac
need_root
detect_platform
inventory
case "$MODE" in
  server) install_server ;;
  agent) install_agent ;;
  update) update_installation "$UPDATE_COMPONENT" ;;
  *) usage; die "mode must be server, agent, or update" ;;
esac
