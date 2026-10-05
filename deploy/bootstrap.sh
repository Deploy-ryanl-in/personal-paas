#!/bin/bash
set -euo pipefail
# Debian 12/13 amd64, cgroup v2 + systemd. Only provisions dedicated PaaS identities.
test "$(id -u)" -eq 0
test -f /sys/fs/cgroup/cgroup.controllers
umask 077
baseline="/root/paas-baseline-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -m 700 "$baseline"
ss -lntup > "$baseline/listeners.txt"
systemctl show x-ui --property=ActiveState,MainPID,ExecMainStartTimestamp > "$baseline/x-ui.txt" 2>/dev/null || true
command -v nft >/dev/null && nft -j list ruleset > "$baseline/firewall.json" || true
test ! -d /etc/x-ui || cp -a /etc/x-ui "$baseline/x-ui-config"
test ! -f /usr/local/x-ui/bin/config.json || cp -a /usr/local/x-ui/bin/config.json "$baseline/xray-config.json"
export DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=l
apt-get update
apt-get install -y --no-install-recommends uidmap slirp4netns dbus-user-session acl age ca-certificates iptables nftables openssl curl
getent group paas-proxy >/dev/null || groupadd --system paas-proxy
getent group paas-tls >/dev/null || groupadd --system paas-tls
id paas-runtime >/dev/null 2>&1 || useradd --create-home --home-dir /var/lib/paas-runtime --shell /bin/bash paas-runtime
id paas-traefik >/dev/null 2>&1 || useradd --system --home-dir /var/lib/paas-traefik --shell /usr/sbin/nologin --gid paas-proxy paas-traefik
id paas-acme >/dev/null 2>&1 || useradd --system --home-dir /var/lib/paas-acme --shell /usr/sbin/nologin paas-acme
usermod -a -G paas-proxy paas-runtime
usermod -a -G paas-tls paas-traefik
python3 - <<'PY'
# Refuse overlapping subordinate mappings, including overlap with another user's range.
for filename in ['/etc/subuid','/etc/subgid']:
    ranges=[]
    for line in open(filename):
        name,start,count=line.strip().split(':');ranges.append((int(start),int(start)+int(count),name))
    own=[r for r in ranges if r[2]=='paas-runtime']
    if not own or sum(b-a for a,b,_ in own)<65536:raise SystemExit('missing subordinate UID/GID allocation')
    for a,b,name in own:
        if any(a<d and c<b for c,d,other in ranges if other!=name):raise SystemExit('overlapping subordinate UID/GID allocations')
PY
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
runtime_uid=$(id -u paas-runtime)
install -d -m 755 "/etc/systemd/system/user@${runtime_uid}.service.d"
delegate="/etc/systemd/system/user@${runtime_uid}.service.d/paas-delegate.conf"
if ! test -f "$delegate"; then
  if systemctl is-active --quiet "user@${runtime_uid}.service"; then
    echo 'Existing runtime user lacks delegation; stop only PaaS workloads before migration' >&2;exit 1
  fi
  cat > "$delegate" <<'UNIT'
[Service]
Delegate=cpu cpuset io memory pids
UNIT
fi
systemctl daemon-reload
loginctl enable-linger paas-runtime
systemctl start "user@${runtime_uid}.service"
echo "Rootless runtime UID: ${runtime_uid}; baseline: ${baseline}"
