#!/bin/sh
set -eu
test -n "$ACME_EMAIL"
# lego v5 uses one run command for both initial issuance and renewal.
/usr/local/bin/lego run --path /var/lib/paas-acme/private --email "$ACME_EMAIL" --dns cloudflare --domains '*.ryanl.in' --cert.name ryanl-wildcard --accept-tos --renew-days 30 --dns.resolvers 1.1.1.1:53 --dns.resolvers 1.0.0.1:53
install -m 640 '/var/lib/paas-acme/private/certificates/ryanl-wildcard.crt' /var/lib/paas-acme/current.crt.new
install -m 640 '/var/lib/paas-acme/private/certificates/ryanl-wildcard.key' /var/lib/paas-acme/current.key.new
mv /var/lib/paas-acme/current.key.new /var/lib/paas-acme/current.key
mv /var/lib/paas-acme/current.crt.new /var/lib/paas-acme/current.crt
# Touch dynamic provider config so Traefik reloads replaced certificate bytes.
touch /var/lib/paas-acme/certificate-renewed
chmod 644 /var/lib/paas-acme/certificate-renewed
