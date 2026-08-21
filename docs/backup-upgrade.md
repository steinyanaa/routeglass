# Backup, upgrade, and rollback

## Backup contents

Back up these Server paths as one protected set:

```text
/etc/routeglass/server.json
/var/lib/routeglass/
```

The state directory contains SQLite, signing and access secrets, Agent identities, sessions, and settings. Encrypt off-host backups and restrict restore access.

For a cold backup:

```bash
sudo systemctl stop routeglass
sudo tar -C / -czf /root/routeglass-$(date +%F).tgz etc/routeglass var/lib/routeglass
sudo systemctl start routeglass
sudo routeglass doctor --config /etc/routeglass/server.json
```

Agent state can be backed up from `/etc/routeglass-agent` and `/var/lib/routeglass-agent`; a lost Agent identity may instead be replaced through a new one-time join.

## Update flow

```bash
sudo routeglass update --component server
sudo routeglass update --component agent
```

The stable updater:

1. takes an exclusive update lock;
2. retrieves `release.json` and the architecture-specific archive;
3. validates version, architecture, size, and SHA256;
4. stages the binary on the destination filesystem;
5. preserves the old binary and creates a consistent Server database backup;
6. checks migration compatibility, stops the service, atomically replaces the binary, and migrates;
7. starts the service and waits for readiness;
8. restores the binary and database backup if readiness fails.

Keep at least one independent backup before a major upgrade. Default operation rejects downgrades because an older binary may not understand a newer schema.

## Release verification

```bash
sha256sum -c SHA256SUMS
gh attestation verify --repo steinyanaa/routeglass routeglass_VERSION_linux_amd64.tar.gz
```

SHA256 detects incomplete or corrupted downloads. GitHub attestation additionally ties an artifact to the repository workflow and commit that built it.
