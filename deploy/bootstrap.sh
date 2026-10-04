#!/bin/bash
set -euo pipefail
# Run once as root on the VPS, from a reviewed platform checkout.
# Never restarts rootful Docker, x-ui, Xray or existing firewall services.
test "$(id -u)" -eq 0
test -f /sys/fs/cgroup/cgroup.controllers
if ss -lnt '( sport = :80 or sport = :443 )' | tail -n +2 | grep -q .; then
  echo '80/443 already occupied; refusing installation' >&2; exit 1
fi
umask 077
baseline="/root/paas-baseline-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -m 700 "$baseline"
ss -lntup > "$baseline/listeners.txt"
systemctl show x-ui --property=ActiveState,MainPID,ExecMainStartTimestamp > "$baseline/x-ui.txt"
nft -j list ruleset > "$baseline/firewall.json"
cp -a /etc/x-ui "$baseline/x-ui-config"
cp -a /usr/local/x-ui/bin/config.json "$baseline/xray-config.json"
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends uidmap slirp4netns dbus-user-session acl age ca-certificates
getent group paas-proxy >/dev/null || groupadd --system paas-proxy
getent group paas-tls >/dev/null || groupadd --system paas-tls
id paas-runtime >/dev/null 2>&1 || useradd --create-home --home-dir /var/lib/paas-runtime --shell /bin/bash paas-runtime
id paas-traefik >/dev/null 2>&1 || useradd --system --home-dir /var/lib/paas-traefik --shell /usr/sbin/nologin --gid paas-proxy paas-traefik
id paas-acme >/dev/null 2>&1 || useradd --system --home-dir /var/lib/paas-acme --shell /usr/sbin/nologin paas-acme
usermod -a -G paas-proxy paas-runtime
usermod -a -G paas-tls paas-traefik
# useradd assigns subordinate UIDs/GIDs. Refuse overlaps or a missing mapping.
grep -q '^paas-runtime:' /etc/subuid
grep -q '^paas-runtime:' /etc/subgid
install -d -o paas-runtime -g paas-runtime -m 700 /var/lib/paas-runtime/{backups,tmp,docker}
install -d -o paas-runtime -g paas-proxy -m 2750 /var/lib/paas-routes
install -d -o paas-acme -g paas-tls -m 2750 /var/lib/paas-acme
install -d -o paas-acme -g paas-acme -m 700 /var/lib/paas-acme/private
setfacl -m u:paas-runtime:--x /var/lib/paas-acme
install -d -o root -g paas-runtime -m 750 /etc/personal-paas
install -d -o root -g paas-proxy -m 750 /etc/paas-traefik
install -d -o root -g paas-acme -m 750 /etc/paas-acme
install -d -o paas-traefik -g paas-proxy -m 750 /var/lib/paas-traefik
test -f /var/lib/paas-runtime/state.key || head -c 32 /dev/urandom > /var/lib/paas-runtime/state.key
chown paas-runtime:paas-runtime /var/lib/paas-runtime/state.key
chmod 600 /var/lib/paas-runtime/state.key
if ! test -f /var/lib/paas-runtime/backup.agekey; then
  age-keygen -o /var/lib/paas-runtime/backup.agekey
  chown paas-runtime:paas-runtime /var/lib/paas-runtime/backup.agekey
  chmod 600 /var/lib/paas-runtime/backup.agekey
fi
ln -sf /usr/bin/age /usr/local/bin/age
loginctl enable-linger paas-runtime
runtime_uid=$(id -u paas-runtime)
systemctl start "user@${runtime_uid}.service"
install -d -m 755 "/etc/systemd/system/user@${runtime_uid}.service.d"
cat > "/etc/systemd/system/user@${runtime_uid}.service.d/paas-delegate.conf" <<'UNIT'
[Service]
Delegate=cpu cpuset io memory pids
UNIT
systemctl daemon-reload
# Delegation takes effect for this newly installed user's manager only.
systemctl restart "user@${runtime_uid}.service"
runuser -u paas-runtime -- env XDG_RUNTIME_DIR="/run/user/${runtime_uid}" DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/${runtime_uid}/bus" dockerd-rootless-setuptool.sh install --force
runuser -u paas-runtime -- env XDG_RUNTIME_DIR="/run/user/${runtime_uid}" DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/${runtime_uid}/bus" systemctl --user enable --now docker
echo "Rootless runtime UID: ${runtime_uid}; baseline: ${baseline}"
echo 'Install reviewed binaries, policy and credentials before starting controller/Traefik.'
