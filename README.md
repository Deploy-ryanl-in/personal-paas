# Personal PaaS

Go controller + encrypted SQLite + rootless Docker + Traefik File Provider for `ryanl.in`.

This repository is under implementation. Installation and acceptance results are tracked in `docs/acceptance.md`; do not infer production readiness from the presence of a workflow.

Applications declare `paas.json`. The controller fetches that exact commit from GitHub, verifies GitHub Actions OIDC and immutable repository identity, admits resources, pulls digest-addressed GHCR images, tests candidates, switches routes, and restores the previous release if health checks fail.

- [ASP.NET template](https://github.com/Deploy-ryanl-in/template-aspnet)
- [Next.js template](https://github.com/Deploy-ryanl-in/template-nextjs)
- [Manifest schema](schema/paas.v1.json)
- [Operator instructions](docs/operations.md)
- [Security boundaries](docs/security.md)

New repositories use the pinned reusable workflows. No SSH deployment identity or Cloudflare token is given to applications. Root SSH is used only for installation. Existing native 3x-ui/Xray and DNS-only infrastructure domains remain independent.

## Development

Requires Go 1.27.1. Run `go test ./...`, `go vet ./...`, and `go build ./cmd/paas`.

`paas validate --file paas.json --repository-id 42 --owner Deploy-ryanl-in --name new-project` validates the deployment manifest and prints the build matrix. JSON Schema helps editors; server-side Go validation remains authoritative and also rejects duplicate keys.

## Capacity

768 MiB is a hard aggregate container memory reservation budget, including candidate releases. Databases keep one stable container and volume. Web containers coexist during replacement. Workers stop before replacement. If the candidate cannot fit, the running release is retained and the workflow fails visibly.

There is one production stack per immutable GitHub repository ID. A repository rename keeps its initially bound automatic domain. Only one-label application subdomains of `ryanl.in` are allowed; infrastructure names are reserved.
