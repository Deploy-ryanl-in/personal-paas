# Operator instructions

For clean VPS provisioning, account/organization rebinding and template regeneration, see [reinstall from zero](reinstall.md).

Runtime credentials live in `/var/lib/paas-runtime` with mode 0600 and owner `paas-runtime`:

- `github-app.pem`: optional legacy GitHub App RSA key, with Metadata/Contents/Actions read only. Current templates use the deploy job's ephemeral repository read token after OIDC verification; no runtime App installation is required for personal or organization repositories. Keep the optional App only for owners still using older workflows.
- `docker/config.json`: GHCR credentials using a classic PAT with only `read:packages`; the issuing user must have access to the private application packages.
- `state.key`: random 32-byte state encryption key.
- `backup.agekey`: age recovery identity. Keep an independent offline copy and its public recipient outside Git.

Cloudflare DNS token is read only by `paas-acme`, scoped to Zone read + DNS edit for `ryanl.in`. Renewals are automatic. Set Cloudflare SSL to Full (strict) only after the valid origin wildcard certificate is loaded.

Trust policy is `/etc/personal-paas/policy.json`: the allowed domain namespace, immutable owner IDs (RyanStanLin 93820487 and Deploy-ryanl-in 337720882), optional repository ID allowlist, approved exact reusable workflow refs/SHA and database/helper digests. All repositories under either bound owner are automatically supported. Platform workflow updates require a reviewed new pin and matching policy; do not use mutable `main` refs for trust.

Applications use their GitHub Actions operation workflow. Fixed operations: status/history/logs/routes, start, stop, restart, delete, cleanup-images, redeploy, rollback by release ID, backup, encrypted download, restore by backup ID and database upgrade by approved same-major digest. Stop retains stopped containers; delete removes containers. Both retain named volumes and the latest successful release. Data restore requires `RESTORE <repository ID>`; code rollback does not rewind database writes.

Daily backups run at 02:00 Asia/Taipei and remain locally for seven days. PostgreSQL uses a logical dump, Redis a consistent snapshot, other volumes a cold archive. Backup files are age encrypted before being indexed or offered for download. Restore creates new volumes before switching; failed restore returns to previous volumes. Losing the VPS and all downloaded copies loses data; local backup cannot survive destruction of its only disk.

Use `journalctl _COMM=paas` as the VPS operator for controller diagnostics. Application logs rotate at 3 × 5 MiB per container. Last three successful releases are kept for rollback; deployment history is retained 90 days. Only platform-labelled resources may be cleaned.

## Application actions

Run the generated repository’s **PaaS operations** workflow from its authorized deployment branch:

| Action | Inputs and result |
| --- | --- |
| `status` / `history` | Current commit, digest, release IDs and durable task history; use a retained release ID for rollback. |
| `logs` | `service` defaults to `web`; last 200 lines, with exact stored secret values redacted. |
| `redeploy` | Reuse the latest successful version, including a previously stopped application. |
| `rollback` | Supply a successful retained release ID; restores image/config/secrets, preserving current database volume bindings. |
| `stop` | Durably suspend containers and remove routes; container identities, volumes and successful release remain. Controller restarts keep the stack stopped. |
| `start` | Resume stopped containers or recreate deleted containers from the latest successful release; no rebuild. |
| `restart` | Stop then start the latest successful release without building another image. |
| `delete` | Remove this repository's containers and routes, retaining volumes and release history. |
| `cleanup-images` | Remove only unused VPS application image references owned by this repository; never remove an image used by a running or stopped container, volumes, shared database base images or GHCR packages. |
| `routes` | List domains, shared group, path filters and participating repositories/services. |
| `backup` / `backups` | Create an encrypted snapshot or list available backup IDs. Applications without volumes still produce encrypted recovery metadata. |
| `download-backup` | Supply backup ID; download the `encrypted-backup` artifact from the successful run. Artifact retention is one day; server retention is seven days. |
| `restore` | Supply backup ID and exact `RESTORE <repository ID>` confirmation. Restore into new volumes, verify, then switch; old volumes remain available if the restore fails. |
| `database-upgrade` | Supply named service and operator-approved image digest. Pause writers, backup, restore into fresh volumes and verify. Cross-major changes are rejected. |

A manual runtime operation advances the repository’s run ordering barrier; an older in-flight push cannot undo a newer stop or rollback. Repeat requests in the same workflow run reuse the same job. A new manual run creates a new operation. Failed deployments remain visible in history; `redeploy` uses the last successful version, not a failed candidate.

## Controller and infrastructure recovery

The runtime uses user systemd services `docker.service` and `personal-paas.service` under the dynamically allocated runtime UID; system Traefik is `paas-traefik.service`. To inspect or restart the controller as an operator:

```sh
runtime_uid=$(id -u paas-runtime)
runuser -u paas-runtime -- env XDG_RUNTIME_DIR=/run/user/$runtime_uid DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$runtime_uid/bus systemctl --user status personal-paas
runtime_uid=$(id -u paas-runtime)
runuser -u paas-runtime -- env XDG_RUNTIME_DIR=/run/user/$runtime_uid DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$runtime_uid/bus systemctl --user restart personal-paas
journalctl _COMM=paas --since '10 minutes ago'
```

Restarting the controller requeues interrupted tasks and reconciles platform-owned containers/routes. Successful applications keep their committed routes while a candidate is retried. The trusted workflow retries transient API/network failures with fresh OIDC tokens. This does not require restarting native x-ui/Xray or rootful Docker.

`paas-acme.timer` renews DNS-01 certificates automatically. `paas-origin-firewall.timer` refreshes official Cloudflare networks; boot uses a validated cached list when the provider is unavailable. Its own nftables table only restricts 80/443; existing service ports and firewall tables remain independent.

## Recovery material

Keep the age identity offline and download encrypted backups to a separate device. An encrypted backup can be inspected without writing plaintext to disk:

```sh
age --decrypt --identity /safe/offline/backup.agekey backup.tar.age | tar -tf -
```

For a complete VPS recovery, retain protected copies of the controller state database, `state.key`, `backup.agekey`, GitHub App/registry credentials and policy alongside encrypted volume backups; reacquire the DNS certificate. SQLite secret/job encryption needs the matching `state.key`. Do not place this material in Git, application images or ordinary Actions artifacts. Database helpers and workflows remain digest/SHA pinned; an administrator must approve upgrades before applications can request them.
