# Agent TLS

## Public-IP certificates in 2026

Let's Encrypt made IPv4 and IPv6 certificates generally available on January 15, 2026. They use the `shortlived` ACME profile and are valid for 160 hours. Official references:

- [GA announcement](https://letsencrypt.org/2026/01/15/6day-and-ip-general-availability.html)
- [Certificate profiles](https://letsencrypt.org/docs/profiles/)
- [Challenge types](https://letsencrypt.org/docs/challenge-types/)
- [Rate limits and ARI renewal exemptions](https://letsencrypt.org/docs/rate-limits/)

IP identifiers support HTTP-01 on public TCP/80 and TLS-ALPN-01 on public TCP/443. DNS-01 validates DNS names, not IP identifiers. Agent traffic may remain on 9443; validation uses the standard challenge port.

## Provider selection

`auto` prefers a public-IP certificate when a stable global address and a safe challenge path are present. IPv4 and IPv6 certificates are managed independently. This keeps a broken address family from taking down the other endpoint.

Operational modes:

| Mode | Identifier | Validation/renewal owner |
|---|---|---|
| `ip` | Public IPv4/IPv6 | RouteGlass ACME, `shortlived` |
| `domain` | Node DNS name | RouteGlass/operator ACME |
| `files` | Certificate SAN | Operator-provided files |

The Go ACME implementation uses `github.com/go-acme/lego/v5`, which supports IP identifiers, ACME Profiles, HTTP-01, TLS-ALPN-01, and ACME Renewal Information.

## Port coexistence

- Free port 80: use standalone HTTP-01; the systemd drop-in adds only `CAP_NET_BIND_SERVICE`.
- Existing web server on port 80: explicitly proxy only `/.well-known/acme-challenge/` to `127.0.0.1:9180`.
- Existing Reality/Xray on port 443: keep it in place; do not select standalone TLS-ALPN-01.
- Both standard ports controlled by unrelated services: use an operator-managed node domain/certificate or adapt the existing proxy explicitly.

## Renewal and failure state

ARI determines the preferred renewal time. The fallback starts with roughly 72 hours remaining and retries with bounded exponential backoff and jitter. A newly obtained certificate is parsed, its chain/SAN/private key are verified, and files are atomically replaced before the live TLS configuration swaps.

Admin status includes identifier, challenge, issuer, validity, renewal time, last success/error, and endpoint readiness. An expired or untrusted certificate removes that address from speedtest selection while Agent heartbeat remains online.

Persist `/var/lib/routeglass-agent` across reinstall. Deleting ACME account/order state during troubleshooting can consume issuance limits; use the Let's Encrypt staging directory for repeated validation tests.
