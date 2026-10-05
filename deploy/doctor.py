#!/usr/bin/env python3
"""Verify installed bindings, services, rootless isolation and actual cgroup enforcement."""
import argparse,json,os,pathlib,pwd,subprocess,urllib.request
from configuration import read,validate
from preflight import check
p=argparse.ArgumentParser();p.add_argument('--credentials',type=pathlib.Path);p.add_argument('--resource-probe',action='store_true');a=p.parse_args()
if os.geteuid()!=0:raise SystemExit('run as root for host inspection')
c=validate(read('/etc/personal-paas/installation.json'));policy=read('/etc/personal-paas/policy.json');uid=pwd.getpwnam('paas-runtime').pw_uid
prefix=['runuser','-u','paas-runtime','--','env','XDG_RUNTIME_DIR=/run/user/'+str(uid),'DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/'+str(uid)+'/bus','PATH=/opt/personal-paas/docker/bin:/usr/local/bin:/usr/bin:/bin']
def user(args):return subprocess.check_output(prefix+args,text=True)
for service in ['paas-traefik','paas-acme.timer','paas-origin-firewall','paas-origin-firewall-refresh.timer']:
 subprocess.run(['systemctl','is-active','--quiet',service],check=True)
for service in ['docker','personal-paas']:user(['systemctl','--user','is-active','--quiet',service])
info=json.loads(user(['docker','--host','unix:///run/user/'+str(uid)+'/docker.sock','info','--format','{{json .}}']))
assert info['CgroupVersion']=='2' and all(info[n] for n in ['MemoryLimit','CpuCfsQuota','PidsLimit']) and any('rootless' in s for s in info['SecurityOptions'])
for file in ['/var/lib/paas-acme/current.key','/etc/paas-acme/dns.env','/var/run/docker.sock']:
 assert subprocess.run(prefix+['test','-r',file]).returncode!=0,'runtime can access protected infrastructure'
assert not {'sudo','docker','paas-tls'}.intersection(user(['id','-nG']).split())
with urllib.request.urlopen('http://127.0.0.1:9080/healthz',timeout=5) as r:assert r.status==200
if a.credentials:check(c,read(a.credentials))
if a.resource_probe:
 output=user(['docker','--host','unix:///run/user/'+str(uid)+'/docker.sock','run','--rm','--network','none','--user','10001:10001','--read-only','--cap-drop','ALL','--security-opt','no-new-privileges','--memory','32m','--memory-swap','32m','--cpus','0.2','--pids-limit','32',policy['helperImage'],'sh','-c','cat /sys/fs/cgroup/memory.max /sys/fs/cgroup/cpu.max /sys/fs/cgroup/pids.max'])
 lines=output.strip().splitlines();assert lines[0]=='33554432' and lines[1]=='20000 100000' and lines[2]=='32',output
 print('Actual cgroups: 32 MiB memory, 0.2 CPU, 32 PIDs; readonly non-root probe passed')
print('Healthy installation for '+c['domain']+'; trusted owners: '+', '.join(o['login']+' ('+o['id']+')' for o in c['owners']))
