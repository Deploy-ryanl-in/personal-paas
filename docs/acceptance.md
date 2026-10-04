# Acceptance record

Status: implementation in progress. Checked items below are local checks, not VPS acceptance.

- [x] Manifest rejects duplicate keys, unknown fields, unsupported versions, unsafe paths and resource values.
- [x] Domain namespace, reserved names, repository-image binding and rename binding tests.
- [x] OIDC claim trust/event/workflow/lifetime negative tests.
- [x] SQLite idempotency, stale run refusal, encrypted secret storage and restart recovery tests.
- [x] Next.js ESLint, TypeScript and Vitest.
- [x] Next.js production standalone build.
- [x] ASP.NET 10 unit and HTTP integration tests.
- [x] Both public GitHub template repositories created and enabled (content publication tracked separately).
- [ ] GitHub OIDC end-to-end authentication.
- [ ] Private template-generated repository push -> GHCR -> HTTPS.
- [x] Rootless Docker cgroup v2 memory/CPU/PID limits verified on VPS; integration covers idempotent creation, loopback routing and candidate budget refusal.
- [ ] Traefik certificate loading and proxied wildcard verified.
- [x] Local Next.js standalone browser checks: SSR, API, streaming, image optimization, repeat ISR and unauthenticated revalidation rejection.
- [ ] Next.js read-only production container cache and authenticated invalidation on VPS.
- [ ] Failed health/capacity/concurrency/restart/rollback drills.
- [ ] PostgreSQL/Redis/Worker persistence, backup and restore drill.
- [x] Native services and DNS baseline saved; native service PID/start time and Xray config unchanged after isolated rootless installation.
- [ ] Final post-activation native service and DNS baseline comparison.

Credentials still required for the runtime: minimal GitHub App installation, GHCR read-only registry token, scoped Cloudflare DNS-01 token. Secrets are not committed.
