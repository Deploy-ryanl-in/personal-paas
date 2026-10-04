# Operator instructions

Runtime credentials live in `/var/lib/paas-runtime` with mode 0600 and owner `paas-runtime`:

- `github-app.pem`: GitHub App RSA key; App requires Metadata read, Contents read, Actions read. Install for all Deploy-ryanl-in repositories to include future repositories.
- `docker/config.json`: GHCR credentials using a classic PAT with only `read:packages`; the issuing user must have access to the private application packages.
- `state.key`: random 32-byte state encryption key.
- `backup.agekey`: age recovery identity. Keep an independent offline copy and its public recipient outside Git.

Cloudflare DNS token is read only by `paas-acme`, scoped to Zone read + DNS edit for `ryanl.in`. Renewals are automatic. Set Cloudflare SSL to Full (strict) only after the valid origin wildcard certificate is loaded.

Trust policy is `/etc/personal-paas/policy.json`: immutable org ID 337720882, initially empty personal repository ID allowlist, approved exact reusable workflow refs/SHA and database/helper digests. Adding personal repositories requires an operator policy update. Platform workflow updates require a reviewed new pin and matching policy; do not use mutable `main` refs for trust.

Applications use their GitHub Actions operation workflow. Fixed operations: status/history/logs, redeploy, rollback by release ID, stop (keeps volumes), backup, encrypted download, restore by backup ID, database upgrade by approved same-major digest. Data restore requires `RESTORE <repository ID>`; code rollback does not rewind database writes.

Daily backups run at 02:00 Asia/Taipei and remain locally for seven days. PostgreSQL uses a logical dump, Redis a consistent snapshot, other volumes a cold archive. Backup files are age encrypted before being indexed or offered for download. Restore creates new volumes before switching; failed restore returns to previous volumes. Losing the VPS and all downloaded copies loses data; local backup cannot survive destruction of its only disk.

Use `journalctl --user -u personal-paas` under the runtime user for controller diagnostics. Application logs rotate at 3 × 5 MiB per container. Last three successful releases are kept for rollback; deployment history is retained 90 days. Only platform-labelled resources may be cleaned.
