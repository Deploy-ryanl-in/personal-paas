#!/bin/sh
set -eu
test -n "$ACME_EMAIL"
if test -f '/var/lib/paas-acme/private/certificates/_.ryanl.in.crt'; then
  /usr/local/bin/lego --path /var/lib/paas-acme/private --email "$ACME_EMAIL" --dns cloudflare --domains '*.ryanl.in' --accept-tos renew --days 30
else
  /usr/local/bin/lego --path /var/lib/paas-acme/private --email "$ACME_EMAIL" --dns cloudflare --domains '*.ryanl.in' --accept-tos run
fi
install -m 640 '/var/lib/paas-acme/private/certificates/_.ryanl.in.crt' /var/lib/paas-acme/current.crt.new
install -m 640 '/var/lib/paas-acme/private/certificates/_.ryanl.in.key' /var/lib/paas-acme/current.key.new
mv /var/lib/paas-acme/current.key.new /var/lib/paas-acme/current.key
mv /var/lib/paas-acme/current.crt.new /var/lib/paas-acme/current.crt
# Touch dynamic provider config so Traefik reloads replaced certificate bytes.
touch /var/lib/paas-acme/certificate-renewed
chmod 644 /var/lib/paas-acme/certificate-renewed
