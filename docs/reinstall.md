# Install or reinstall from zero

The supported production host is Debian 12/13 **amd64**, systemd, cgroup v2, with 80/443 available. Use a fresh VPS for a full reset. The installer provisions its own rootless Docker binaries and never installs/restarts a rootful Docker daemon, requires x-ui, or changes existing infrastructure ports. A practical host has at least 2 GiB RAM and 20 GiB disk. Application reservations remain 768 MiB including replacement candidates.

## 1. Bind accounts and organizations

Fork or reuse this public platform repository. CI produces a controller and installer release named `build-<full commit SHA>`; application images continue to build in GitHub Actions. Choose an explicitly reviewed commit, not `main` or `latest`, for every installation and template.

Create a runtime GitHub App under your chosen personal account or organization:

- Repository permissions: **Contents: Read**, **Actions: Read**, **Metadata: Read**. No write permissions, webhook, OAuth callback, or SSH key.
- If binding more than its owning account, allow installation on other accounts (an existing private App uses **Advanced → Make public**). Public installability does not authorize deployment: the VPS separately checks immutable owner/repository IDs and approved workflow commits.
- Install the App on **All repositories** for each trusted personal account and organization. GitHub requires an account/organization owner to approve installation. This covers future private repositories.
- Download the RSA PEM and record the App ID. Create a GHCR credential with only `read:packages` that can read private images under **all configured owners**; organization SSO must authorize it if applicable.
- Create a Cloudflare token scoped to your zone: **Zone DNS Edit + Zone Read**, for ACME only. For optional automatic DNS/SSL bootstrap, supply a separate `cloudflareSetupToken` scoped to the same zone with **Zone DNS Edit + Zone Read + Zone Settings Edit**. The setup token is not installed in any long-running service.

These external credentials cannot be created silently on a new account. They are explicit one-time prerequisites. No application repository receives these credentials.

## 2. Generate the installation configuration

Copy `deploy/installation.example.json`. Change the base domain, control subdomain, origin IPv4, Cloudflare zone ID, platform repository and full reviewed SHA. `owners` supports both `User` and `Organization`; `repositoryAllowlist` optionally permits individually enrolled repository IDs outside those owners. Every listed owner grants all its repositories the ability to request deployments, so bind only accounts you control.

On a workstation with Python 3.13+, resolve IDs instead of guessing them:

```sh
python3 deploy/configure.py --config installation.json --output prepared \
  --resolve-owners YourPersonalAccount YourOrganization
```

Review `prepared/installation.json` and `prepared/policy.json`. Owner login and immutable ID must both match. API hostname, domain validation, GHCR owner paths, ACME certificate and reusable workflows derive from this configuration. Existing repositories with identical names in different owners need explicit different domains; domain ownership is reserved by immutable repository ID.

Copy `deploy/credentials.example.json` outside your checkout as `credentials.json`; fill in actual values, including the PEM as a JSON string. Use a password manager or a small local JSON-generation script to preserve PEM newlines. `chmod 600 credentials.json`. Never commit it. A `cloudflareSetupToken` is optional; an old credential bundle's `workflowSha` is accepted for compatibility but never controls or overwrites the trust policy.

## 3. Install the verified release on a clean VPS

Download `personal-paas-linux-amd64.tar.gz` and `SHA256SUMS` from the **same** immutable `build-<SHA>` release under your configured platform repository. Verify the archive before extraction:

```sh
sha256sum --check SHA256SUMS
tar -xzf personal-paas-linux-amd64.tar.gz
chmod 600 credentials.json
sudo python3 personal-paas/deploy/install.py \
  --config prepared/installation.json --credentials credentials.json \
  --configure-cloudflare
sudo python3 personal-paas/deploy/doctor.py \
  --credentials credentials.json --resource-probe
```

The installer checks App permissions and installations **before** provisioning. It downloads checksum-pinned Docker/rootless extras, Traefik and lego from official vendors; creates separate runtime, proxy and ACME identities; delegates cgroup controllers; imports protected credentials; obtains a DNS-01 wildcard certificate; starts the controller and Traefik; then configures the one wildcard and Full (strict). It only filters 80/443 to Cloudflare source addresses. Installation input must match the bundle's commit and controller checksum.

If Cloudflare is already configured, omit `--configure-cloudflare`; verify `*.your-domain → VPS` is proxied and SSL is Full (strict). The DNS-only ACME token need not gain SSL settings permissions. All existing non-wildcard records remain untouched.

Run the same command again to repair an installation. It preserves runtime identity, SQLite state, volumes, encrypted secrets, age key and running rootless Docker. Policy/binary changes restart only the PaaS controller. An occupied HTTP port belonging to another service is a hard error; the installer will not evict it. Namespace replacement with existing state requires an explicit migration or a fresh VPS.

Export `/var/lib/paas-runtime/backup.agekey` to protected **off-host** storage, then remove the temporary credential bundle from the VPS. Record the installation JSON and release SHA in your infrastructure repository; store secrets separately. Source configuration recreates infrastructure, not database contents. A new VPS without application data can redeploy from a normal push. To preserve data, separately move encrypted backups and the recovery key, install the original state/age keys when migrating encrypted state, and use the restore workflow after creating the application stack. Never overwrite database data during an ordinary reinstall.

## 4. Create templates for this installation

On the workstation, or from the extracted release (Python 3.13+):

```sh
python3 tools/prepare-templates.py --config prepared/installation.json --output starters
```

The tool fetches commit-pinned source templates, preserves language-specific CI/tests/Docker locks, and updates the deployment API URL, domain namespace, platform repository and approved workflow SHA. It refuses nonempty output directories. Publish `starters/aspnet` and `starters/nextjs` as two public template repositories, enable **Settings → Template repository**, and use them under any bound personal account or organization. When reusing this platform repository, GitHub already permits public reusable workflow access by private callers. If making the platform private, explicitly configure GitHub reusable-workflow access for callers; cross-account private-workflow access is subject to GitHub's account/organization restrictions.

Use template → clone → develop → first push. No per-repository enrollment, DNS, SSH or application secret is needed for the default web app. Change `paas.json` only for application needs. Templates generated for another VPS/domain are independently configurable and never contain credentials.

## Updating approved workflows

Review a new platform release and use its commit in `platformSha`; put still-required older reviewed revisions in `previousWorkflowShas`. Re-run installation to update the controller/policy, then regenerate/publish templates. Existing generated repositories keep their original pinned workflows until explicitly updated; old versions work only while retained in the server trust policy. Credential import never resets this approval list.
