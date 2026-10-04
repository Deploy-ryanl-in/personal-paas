# Acceptance record

Status: acceptance in progress (2026-10-05, Asia/Taipei). The records distinguish local checks from actual VPS and GitHub Actions checks.

- [x] Manifest rejects duplicate keys, unknown fields, unsupported versions, unsafe paths and resource values.
- [x] Domain namespace, reserved names, repository-image binding and rename binding tests.
- [x] OIDC claim trust/event/workflow/lifetime negative tests.
- [x] SQLite idempotency, stale run refusal, encrypted secret storage and restart recovery tests.
- [x] Next.js ESLint, TypeScript and Vitest.
- [x] Next.js production standalone build.
- [x] ASP.NET 10 unit and HTTP integration tests.
- [x] Both public GitHub template repositories created and enabled (content publication tracked separately).
- [x] GitHub OIDC end-to-end authentication using the installed read-only GitHub App.
- [x] Private template-generated repositories cloned locally, edited and pushed: ASP.NET run 37225791796 and Next.js run 37225843829 both succeeded through GHCR and HTTPS.
- [x] Rootless Docker cgroup v2 memory/CPU/PID limits verified on VPS; integration covers idempotent creation, loopback routing and candidate budget refusal.
- [x] DNS-01 wildcard certificate, Traefik certificate loading, Cloudflare proxied wildcard and Full (strict) verified.
- [x] Local Next.js standalone browser checks: SSR, API, streaming, image optimization, repeat ISR and unauthenticated revalidation rejection.
- [x] Exact published Next.js production image: read-only root, bounded temporary caches, authenticated invalidation and timed ISR regeneration verified on VPS. Public SSR/hydration, API, streaming and optimized images verified through Cloudflare.
- [ ] Failed health/capacity/concurrency/restart/rollback drills.
- [x] PostgreSQL/Redis/Worker persistence, cold volume backup, age encryption, restore into fresh volumes, same-major maintenance and stop-with-volumes-retained: VPS integration passed (143.23 seconds).
- [x] Native services and DNS baseline saved; native service PID/start time and Xray config unchanged after isolated rootless installation.
- [ ] Final post-activation native service and DNS baseline comparison.

Runtime credentials were supplied by the operator and imported into protected VPS files. GitHub App permissions are Contents/Actions/Metadata read; GHCR is read-only; DNS-01 token is scoped to ryanl.in. Secrets and age recovery identity are excluded from Git.

An initial Python-urllib request was rejected by Cloudflare Browser Integrity Check. The approved reusable workflows now send an explicit Personal PaaS user agent; Cloudflare protections remain enabled. Database drills caught Redis empty AOF precedence over restored RDB and PostgreSQL temporary initialization-server readiness. RDB is now converted to durable AOF before switching; PostgreSQL readiness requires its final TCP listener. The corrected restore/maintenance drill passed.
