# Personal PaaS

Go controller + SQLite with encrypted secrets + rootless Docker + Traefik File Provider with configurable domain and account/organization bindings.

Installed on the single VPS. Actual GitHub Actions, private-template and VPS acceptance evidence is recorded in [docs/acceptance.md](docs/acceptance.md).

Applications declare `paas.json`. The controller fetches that exact commit from GitHub, verifies GitHub Actions OIDC and immutable repository identity, admits resources, pulls digest-addressed GHCR images, tests candidates, switches routes, and restores the previous release if health checks fail.

- [ASP.NET template](https://github.com/Deploy-ryanl-in/template-aspnet)
- [Next.js template](https://github.com/Deploy-ryanl-in/template-nextjs)
- [Manifest schema](schema/paas.v1.json)
- [Operator instructions](docs/operations.md)
- [Security boundaries](docs/security.md)

New repositories use the pinned reusable workflows. No SSH deployment identity or Cloudflare token is given to applications. Root SSH is used only for installation. Existing native 3x-ui/Xray and DNS-only infrastructure domains remain independent.

## Create and develop an application

1. Open one of the template links above and select **Use this template → Create a new repository**. Choose `RyanStanLin` or `Deploy-ryanl-in` as owner; public and private repositories both work.
2. Clone your new repository locally and follow its language-specific README. The initial commit deploys the default starter automatically.
3. Edit application code, commit and push to `main`. Default CI validates/tests, builds an immutable GHCR image and deploys through GitHub OIDC. No repository SSH key, DNS token or default application secret is needed.
4. Open `https://<repository-name>.ryanl.in`. The Actions summary reports the actual domain, commit and release. Subsequent `git pull` synchronizes your independent repository.

Keep `paas.json` declarative. `auto` binds the new repository identity on its first successful deployment and preserves the domain after renames. Reserved/invalid names require an explicit allowed domain. For application secrets, use the generated repository’s `PAAS_SECRETS` JSON secret and declare only required references.

For management, choose **Actions → PaaS operations → Run workflow**. Status, history, logs, redeploy, rollback, stop, backup/download, restore and same-major database maintenance are available there. `state: absent` plus push removes containers and routes while retaining volumes; this path skips image builds.

Personal-account repositories require an operator-added immutable repository ID allowlist entry. Existing generated repositories remain independent of template changes: reusable workflow updates need a reviewed SHA pin and matching server trust policy.

## Development

Requires Go 1.27.1. Run `go test ./...`, `go vet ./...`, and `go build ./cmd/paas`.

`paas validate --file paas.json --repository-id 42 --owner Deploy-ryanl-in --name new-project` validates the deployment manifest and prints the build matrix. JSON Schema helps editors; server-side Go validation remains authoritative and also rejects duplicate keys.

## Capacity

768 MiB is a hard aggregate container memory reservation budget, including candidate releases. Databases keep one stable container and volume. Web containers coexist during replacement. Workers stop before replacement. If the candidate cannot fit, the running release is retained and the workflow fails visibly.

There is one production stack per immutable GitHub repository ID. A repository rename keeps its initially bound automatic domain. Only one-label application subdomains of `ryanl.in` are allowed; infrastructure names are reserved.

Clean VPS installation and rebinding: see [install/reinstall from zero](docs/reinstall.md). Both personal accounts and organizations are authorized by immutable owner ID; no per-repository allowlist is needed under a trusted owner.
