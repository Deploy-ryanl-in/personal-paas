# Acceptance record

Completed on 2026-10-05 (Asia/Taipei), against the existing single VPS and the installed GitHub/Cloudflare configuration. The evidence below distinguishes automated tests from live deployment drills.

## Final template-to-production flow

Both final public repositories are enabled as GitHub templates. New **private** repositories were created through **Use this template**, default branch only, without editing `paas.json` or any default workflow and without adding application secrets. Both were cloned over SSH, run/tested locally, changed only in application code and pushed to `main`.

| Repository | Immutable ID | Initial template deployment | Local development push | Final HTTPS endpoint |
| --- | --- | --- | --- | --- |
| `Deploy-ryanl-in/demo-aspnet` | 1404788365 | [37230094988](https://github.com/Deploy-ryanl-in/demo-aspnet/actions/runs/37230094988) | [37230689287](https://github.com/Deploy-ryanl-in/demo-aspnet/actions/runs/37230689287), commit `46cb30a2be3ce3a4988aeafa1e2065398a4779f2` | https://demo-aspnet.ryanl.in |
| `Deploy-ryanl-in/demo-nextjs` | 1404791247 | [37230334432](https://github.com/Deploy-ryanl-in/demo-nextjs/actions/runs/37230334432) | [37230819396](https://github.com/Deploy-ryanl-in/demo-nextjs/actions/runs/37230819396), commit `f0c46b9433c3602d19a7f40f318843325edebe68` | https://demo-nextjs.ryanl.in |

Both development-push runs passed tests, manifest validation, `linux/amd64` Docker build, private GHCR publication/pull, OIDC deployment and external HTTPS observation. The two generated repositories keep their own commit history. New images are bound to the new repository IDs rather than template identities.

Final ASP.NET image: `ghcr.io/deploy-ryanl-in/paas-1404788365-web@sha256:0827020e308a7963a478580c6d477c0bf14dc14da28b9dd184d577faacc5e883`.

Final Next.js image: `ghcr.io/deploy-ryanl-in/paas-1404791247-web@sha256:24865c8db6b0aabf72805b4022d9b7b84354836ac3effe306273556fdd37a3be`.

Approved reusable workflow pin: `372999fbd35273b7c3366a350bc4a47515396938`. Public controller and template CI passed before the final fresh-template acceptance. Documentation commits do not change that reviewed pin. The prior reviewed `89e69485c95771272977c3bdec74bb0fe20f8eec` remains allowed for existing generated repositories.

## Reliability and security evidence

| Check | Result and evidence |
| --- | --- |
| Strict manifest and trust boundaries | Go tests reject duplicate/unknown fields, unsupported versions, unsafe paths/resources, external/reserved domains, unbound image sources, unauthorized owner/repository/event/workflow/lifetime claims, host mounts and network escapes. |
| Ordering and idempotency | SQLite tests verify encrypted secret storage, queue recovery, repeated request reuse, identical running candidate reuse, newer candidate supersession and refusal of older pushes after manual runtime operations. Both applications deployed through the shared serial queue. |
| Health failure after route switch | [37228181186](https://github.com/Deploy-ryanl-in/acceptance-aspnet/actions/runs/37228181186) intentionally failed after the candidate became unhealthy. The old route/version was restored and the candidate removed. |
| Capacity refusal | [37229087484](https://github.com/Deploy-ryanl-in/acceptance-aspnet/actions/runs/37229087484) intentionally requested 768 MiB for an additional candidate. Admission failed visibly and the old application kept serving. |
| Declarative shutdown | [37229762760](https://github.com/Deploy-ryanl-in/acceptance-aspnet/actions/runs/37229762760) succeeded with `state: absent`; build and artifact download were skipped, route returned 404 and the other app remained healthy. |
| Interrupted operation recovery | [37229900048](https://github.com/Deploy-ryanl-in/acceptance-aspnet/actions/runs/37229900048) restored a stopped application. Its running controller was restarted during candidate observation; the same durable job resumed and succeeded without duplicate candidates. |
| Manual code rollback | [37230175416](https://github.com/Deploy-ryanl-in/acceptance-aspnet/actions/runs/37230175416) succeeded using a retained release ID. |
| Operations and backups | Status [37228204560](https://github.com/Deploy-ryanl-in/acceptance-aspnet/actions/runs/37228204560), encrypted backup [37230356765](https://github.com/Deploy-ryanl-in/acceptance-aspnet/actions/runs/37230356765), artifact download [37230459429](https://github.com/Deploy-ryanl-in/acceptance-aspnet/actions/runs/37230459429) succeeded. The server backup has a valid age header and the independent recovery identity decrypts a valid archive. |
| Stop and cleanup | Old drill apps stopped through [37230510060](https://github.com/Deploy-ryanl-in/acceptance-aspnet/actions/runs/37230510060) and [37230598002](https://github.com/Deploy-ryanl-in/acceptance-nextjs/actions/runs/37230598002). Only the two final demo containers remain; retained releases/backups and data are preserved. |
| Effective rootless limits | VPS checks confirmed 192/256 MiB, 0.5 CPU and 128 PIDs per final web container, UID 10001, read-only root, no privilege, distinct repository networks and loopback-only host bindings. The total live reservation is 448 MiB; candidates are included in the 768 MiB budget. |
| Runtime privileges | `paas-runtime` has no sudo or rootful Docker access and cannot read the ACME DNS token or TLS private key. Traefik has no Docker socket. |
| Origin protection | A GitHub-hosted runner could access the HTTPS domain but direct raw-IP 80/443 access timed out. Existing high ports remain separate. Validated Cloudflare network-cache fallback passed a provider-outage simulation. |

The health and capacity runs above are intentionally red: the verified result is retention of the healthy prior version.

## Applications and data

- ASP.NET: locked restore, format check, Release build and three unit/HTTP integration tests passed locally and in Actions. A new clone started locally and its health/API returned 200.
- Next.js: npm lock restore, ESLint, TypeScript, three Vitest tests, production standalone build and three Playwright smoke tests passed. A new clone started locally; SSR included the development edit. Public SSR/client interaction, server API, static assets, optimized image and streaming were verified through Cloudflare.
- Published production Next.js image: VPS integration passed read-only root, bounded temporary caches, runtime-only revalidation secret, unauthorized rejection, authenticated invalidation, timed ISR regeneration and repeated optimized-image requests. Cloudflare preserved dynamic cache control; no Cache Everything rule was introduced.
- PostgreSQL/Redis/Worker and generic volume: opt-in VPS integration passed in 143.23 seconds, including service dependencies, persistent values, consistent database snapshots, cold file backup, age encryption, restore into fresh volumes, previous-volume preservation, stop-with-volumes-retained and cross-major rejection. The same-major maintenance path was exercised using the already approved digest; approving a different future database release is an operator responsibility.

Database drills caught Redis empty AOF precedence over restored RDB and PostgreSQL temporary initialization-server readiness. Redis restore now converts the snapshot to durable AOF; PostgreSQL readiness waits for the final TCP listener. Corrected restore/maintenance tests passed.

## Infrastructure preservation

Final post-activation comparison found all seven original DNS records unchanged in name/type/content/TTL/proxy/priority. The only addition is proxied `*.ryanl.in → 23.169.184.53`; Cloudflare SSL is Full (strict). `ui.proxy.ryanl.in`, `ping.ryanl.in`, MX and TXT remain unchanged.

Native x-ui remained PID 179399 with start time 2026-10-02 22:07:16 CST; Xray remained PID 179492. Its configuration SHA-256 stayed `d031d2e7dcda21c3c5da8b1e7bb290df9382f5acbab84f37a5ac1e58aaefea71`. Native ports 22, 11111, 62789, 17568, 2096, 25390, 49818 TCP/UDP and 45584 remained present. No native service was migrated or restarted.

Traefik terminates origin TLS using a DNS-01 wildcard certificate valid through 2027-01-02 and an automatic renewal timer. Only platform-owned nftables resources were added. Controller and Docker run under a separate rootless user; Traefik and certificate services use separate users.

Runtime credentials were supplied by the operator and imported into protected VPS files. GitHub App permissions are Contents/Actions/Metadata read; GHCR is read-only; DNS-01 is limited to ryanl.in. Secrets, state key and age recovery identity are excluded from Git. Backups remain local for seven days and can be downloaded; no R2 service was configured.

An initial Python urllib request was rejected by Cloudflare Browser Integrity Check. Trusted workflows now send an explicit Personal PaaS user agent; Cloudflare protections remain enabled.
