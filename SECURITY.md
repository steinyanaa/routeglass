# RouteGlass Security Policy

## Reporting a vulnerability

Use GitHub **Security → Report a vulnerability** to send a private report. Include the affected version, deployment mode, reproduction steps, impact, and any relevant sanitized logs. Do not include active access codes, join/invite tokens, Agent identities, private keys, signed test grants, or complete user IP histories.

Maintainers should acknowledge a complete report within five business days, validate severity, coordinate a fix and disclosure date, and publish an advisory for affected releases. Public issues are suitable for hardening ideas that do not reveal an exploitable flaw.

## Supported versions

Until the first stable release, only the newest tagged release receives security fixes. After v1, the latest minor line and the immediately preceding minor line receive fixes unless a release advisory states otherwise.

## Deployment security baseline

- Put the Server behind browser-trusted HTTPS before entering admin credentials.
- Keep the default loopback Server listener unless direct exposure is deliberate.
- Use a browser-trusted Agent certificate; production endpoints do not use self-signed TLS.
- Configure exact trusted proxy CIDRs. Forwarding headers from any other peer are ignored.
- Keep private access enabled and rotate the server root secret after suspected disclosure.
- Open only Agent TCP/9443 and the ACME challenge port actually in use.
- Run the supplied systemd units under their dedicated users; do not run either component as root.
- Restrict `/etc/routeglass*` and `/var/lib/routeglass*` backups as production secrets.
- Apply stable updates promptly and run `routeglass doctor` after host/proxy/firewall changes.

## Credential boundaries

RouteGlass keeps these credentials independent:

- Argon2id admin password and optional TOTP secret
- rotating diagnostic access-code root secret
- access and admin session identifiers
- one-time invite and Agent join tokens (hashes at rest)
- persistent Agent identity
- Server Ed25519 test-grant signing key
- ACME account and TLS private keys

Logs redact all of them. A join token is exchanged once and removed from Agent configuration after registration. The installer captures the initial admin password outside journald and deletes the temporary capture file after displaying it.

## Network abuse controls

- Public download/upload/probe endpoints require a signed, short-lived, node/IP/scope-bound grant.
- Nonces, byte caps, concurrency, cooldown, per-session limits, and daily quotas are enforced at the Agent.
- Return-route targets come from the Server-observed client IP; public users do not supply arbitrary targets.
- Network tools receive parsed IP addresses and fixed argument arrays through direct process execution with timeouts and output limits.
- CORS allows the configured Server origin, never `*`.

## Installer and supply chain

- Installers verify the release archive against `SHA256SUMS` before atomic replacement.
- GitHub Releases include build-provenance attestations. Verify them with `gh attestation verify --repo steinyanaa/routeglass ARTIFACT` when the GitHub CLI is available.
- Existing 80/443 listeners and proxy configurations are inventory inputs. Changes require an explicit proxy flag, live in marked snippets, and pass native syntax validation before reload.
- Firewall teardown removes only rules recorded as created by RouteGlass.
