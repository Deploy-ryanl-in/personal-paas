# Security boundaries

The repository is trusted to run its own application code, not to choose host Docker arguments. The API accepts image digests and selected secrets, and reconstructs the manifest from GitHub. Immutable repository ID is the application boundary.

OIDC requires GitHub JWKS RS256, the exact API audience, short lifetime, approved owner/repository ID, an authorized branch from the default-branch manifest, push/manual event, a verified run belonging to the repository, and an approved reusable workflow commit. Write requests consume token JTI once. Pull requests and forks cannot deploy.

Rootless Docker runs under `paas-runtime`. No CI SSH key, sudo, rootful Docker socket, privileged container, arbitrary host mount, device, host namespace, raw shell command or raw Traefik configuration is exposed in `paas.json`. Application images must originate from the expected per-repository GHCR namespace and carry matching OCI source/commit labels. PostgreSQL/Redis/helper digests are operator-approved.

Traefik has no Docker socket. A separate user reads atomic file-provider routes and certificate files. DNS credentials stay with the certificate service. Only Cloudflare source networks can reach 80/443 after origin filtering is installed. Existing high TCP/UDP ports are not filtered by that rule.

Runtime secrets are AES-256-GCM encrypted in SQLite with a host-local key, and referenced values only are sent by the deploy job. Containers still receive the values they need. Application code can emit its own secrets; log retrieval redacts exact stored values but does not replace responsible application logging. GitHub `PAAS_SECRETS` must be JSON; values are single-line strings.

The rootless daemon/controller are privileged within this application's user boundary. Rootless Docker is not a substitute for a patched kernel. The dedicated runtime user's API and registry credentials cannot be mounted by a manifest.
