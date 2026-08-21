# Deployment guide

## Preflight and defaults

The installer supports systemd-based Debian/Ubuntu on Linux amd64 and arm64. It inventories TCP 80, 443, 8765, and 9443; active Nginx, Caddy, Apache, and Xray services; and UFW, firewalld, or nftables.

Run a preview first on hosts with an established proxy stack:

```bash
curl -fsSL https://raw.githubusercontent.com/steinyanaa/routeglass/main/install/install.sh -o /tmp/routeglass-install.sh
sudo bash /tmp/routeglass-install.sh server --dry-run
```

Server defaults to `127.0.0.1:8765`. Agent defaults to `:9443`. No default action claims TCP/80 or TCP/443.

## Existing Nginx

The installer can add an isolated RouteGlass HTTP reverse-proxy site after explicit opt-in:

```bash
sudo bash /tmp/routeglass-install.sh server \
  --domain lg.example.com --configure-proxy nginx
```

It backs up an existing RouteGlass snippet, writes `/etc/nginx/conf.d/routeglass.conf`, runs `nginx -t`, and reloads only after validation. Use your normal certificate automation to add TLS to that virtual host, then set `public_origin` to the final HTTPS origin.

Equivalent essential proxy behavior:

```nginx
location / {
    proxy_pass http://127.0.0.1:8765;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
}
```

Add only the Nginx socket CIDR/address to `trusted_proxies`.

## Existing Caddy

When `/etc/caddy/Caddyfile` imports `conf.d/*`, explicit configuration writes a separate site:

```bash
sudo bash /tmp/routeglass-install.sh server \
  --domain lg.example.com --configure-proxy caddy
```

The generated block is:

```caddyfile
lg.example.com {
    reverse_proxy 127.0.0.1:8765
}
```

Caddy validation must pass before reload. Automatic HTTPS remains Caddy's responsibility.

## Reality/Xray host

Keep Reality/Xray on its existing 443 listener. RouteGlass remains on loopback 8765 and Agent HTTPS on 9443. Use an existing reverse proxy, a separate public IP, or a separate node domain. The installer does not stop, mask, or edit Xray.

## Firewall

`--configure-firewall` adds Agent 9443/tcp through active UFW/firewalld. Standalone public-IP HTTP-01 also adds 80/tcp. Rules are recorded in `/var/lib/routeglass-agent/install-firewall.state` for exact uninstall.

For native nftables, add an accept rule in the correct existing input chain according to local policy. The installer reports the required port and leaves the ruleset unchanged.

## Validation

```bash
systemctl status routeglass --no-pager
routeglass doctor --config /etc/routeglass/server.json
curl -fsS http://127.0.0.1:8765/healthz

systemctl status routeglass-agent --no-pager
routeglass doctor --config /etc/routeglass-agent/agent.json
```

From an external network, verify the Agent certificate and endpoint:

```bash
curl -v https://NODE_IP:9443/v1/probe
```

The unauthenticated probe request should receive 403 after a valid TLS handshake.
