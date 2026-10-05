#!/bin/sh
set -eu
: "${ACME_EMAIL:?}" "${PAAS_DOMAIN:?}"
name="${ACME_CERT_NAME:-paas-wildcard}"
/usr/local/bin/lego run --path /var/lib/paas-acme/private --email "$ACME_EMAIL" --dns cloudflare --domains "*.${PAAS_DOMAIN}" --cert.name "$name" --accept-tos --renew-days 30 --dns.resolvers 1.1.1.1:53 --dns.resolvers 1.0.0.1:53
install -m 640 "/var/lib/paas-acme/private/certificates/${name}.crt" /var/lib/paas-acme/current.crt.new
install -m 640 "/var/lib/paas-acme/private/certificates/${name}.key" /var/lib/paas-acme/current.key.new
mv /var/lib/paas-acme/current.key.new /var/lib/paas-acme/current.key
mv /var/lib/paas-acme/current.crt.new /var/lib/paas-acme/current.crt
touch /var/lib/paas-acme/certificate-renewed
chmod 644 /var/lib/paas-acme/certificate-renewed
