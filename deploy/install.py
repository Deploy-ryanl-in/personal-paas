#!/usr/bin/env python3
"""Complete, repeatable Debian VPS installer. Run from an immutable reviewed release bundle."""
import argparse,hashlib,json,os,pathlib,pwd,shutil,subprocess,tarfile,tempfile,time,urllib.request
from configuration import read,validate,policy
from preflight import check
p=argparse.ArgumentParser()
p.add_argument('--config',required=True,type=pathlib.Path)
p.add_argument('--credentials',required=True,type=pathlib.Path)
p.add_argument('--configure-cloudflare',action='store_true',help='Create/update the wildcard and set Full (strict), with a separate setup token if needed')
a=p.parse_args()
if os.geteuid()!=0:raise SystemExit('run as root for one-time provisioning')
c=validate(read(a.config));credentials=read(a.credentials)
if a.credentials.stat().st_mode & 0o077:raise SystemExit('credential bundle must be mode 600')
required={'githubAppId','githubAppPrivateKey','ghcrUsername','ghcrReadToken','cloudflareDnsToken','acmeEmail'}
if required-set(credentials) or set(credentials)-required-{'workflowSha','cloudflareSetupToken'}:raise SystemExit('credential fields do not match')
root=pathlib.Path(__file__).resolve().parents[1];pins=read(root/'pins.json');bundle=read(root/'bundle.json')
if bundle['commit']!=c['platformSha']:raise SystemExit('installation must match the reviewed bundle commit')
binary=root/'bin/paas'
if hashlib.sha256(binary.read_bytes()).hexdigest()!=bundle['controllerSha256']:raise SystemExit('controller checksum mismatch')
osrelease=dict(line.strip().split('=',1) for line in open('/etc/os-release') if '=' in line)
if osrelease.get('ID','').strip('"')!='debian' or osrelease.get('VERSION_ID','').strip('"') not in ['12','13'] or subprocess.check_output(['uname','-m'],text=True).strip()!='x86_64':raise SystemExit('supported host: Debian 12/13 amd64')
if not pathlib.Path('/sys/fs/cgroup/cgroup.controllers').exists() or not pathlib.Path('/run/systemd/system').exists():raise SystemExit('systemd + cgroup v2 required')
old=pathlib.Path('/etc/personal-paas/installation.json')
if old.exists() and read(old)['domain']!=c['domain'] and pathlib.Path('/var/lib/paas-runtime/state.db').exists():raise SystemExit('use a fresh VPS for namespace replacement; migrate application bindings explicitly')
listeners=subprocess.check_output(['ss','-lntp'],text=True)
for line in listeners.splitlines():
    fields=line.split()
    if len(fields)>3 and fields[3].rsplit(':',1)[-1] in ['80','443'] and 'traefik' not in line:raise SystemExit('80/443 occupied by another service')
# Validate remote identity before changing users, packages, or DNS.
check(c,credentials)
subprocess.run(['bash',str(root/'deploy/bootstrap.sh')],check=True)
user=pwd.getpwnam('paas-runtime');uid=user.pw_uid
changed=set()
def write(path,content,mode=0o644,owner=0,group=0):
    path=pathlib.Path(path);data=content.encode() if isinstance(content,str) else content
    differs=not path.exists() or path.read_bytes()!=data
    if differs:
        path.parent.mkdir(parents=True,exist_ok=True)
        temp=path.with_name(path.name+'.new');temp.write_bytes(data);temp.chmod(mode);os.chown(temp,owner,group);os.replace(temp,path);changed.add(str(path))
    path.chmod(mode);os.chown(path,owner,group)
def run(args,**kwargs):subprocess.run(args,check=True,**kwargs)
def userrun(args):
    return subprocess.check_output(['runuser','-u','paas-runtime','--','env','XDG_RUNTIME_DIR=/run/user/'+str(uid),'DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/'+str(uid)+'/bus','PATH=/opt/personal-paas/docker/bin:/usr/local/bin:/usr/bin:/bin']+args,text=True)
cache=pathlib.Path('/var/cache/personal-paas');cache.mkdir(parents=True,exist_ok=True)
for name,entry in pins['binaries'].items():
    archive=cache/entry['archive']
    if not archive.exists() or hashlib.sha256(archive.read_bytes()).hexdigest()!=entry['sha256']:
        temporary=archive.with_suffix('.download');urllib.request.urlretrieve(entry['url'],temporary)
        if hashlib.sha256(temporary.read_bytes()).hexdigest()!=entry['sha256']:raise SystemExit('official archive checksum mismatch: '+name)
        os.replace(temporary,archive)
    with tarfile.open(archive) as tar:
        for item in tar.getmembers():
            if not item.isfile():continue
            filename=pathlib.PurePosixPath(item.name).name
            if name.startswith('docker'):target='/opt/personal-paas/docker/bin/'+filename
            elif filename==name:target='/usr/local/bin/'+name
            else:continue
            write(target,tar.extractfile(item).read(),0o755)
write('/usr/local/bin/paas',binary.read_bytes(),0o755)
write('/etc/personal-paas/policy.json',json.dumps(policy(c,pins),indent=2)+'\n',0o640,0,user.pw_gid)
write('/etc/personal-paas/installation.json',json.dumps(c,indent=2)+'\n',0o640,0,user.pw_gid)
credential_paths=['/etc/personal-paas/runtime.env','/var/lib/paas-runtime/github-app.pem','/var/lib/paas-runtime/docker/config.json']
before={n:pathlib.Path(n).read_bytes() if pathlib.Path(n).exists() else None for n in credential_paths}
run(['python3',str(root/'deploy/credential-install.py'),str(a.credentials.resolve()),str(a.config.resolve())])
for n in credential_paths:
    if before[n]!=pathlib.Path(n).read_bytes():changed.add(n)
for source in (root/'deploy').glob('paas-*'):
    if source.suffix not in ['.service','.timer']:continue
    if source.name=='paas-runtime.service':target='/var/lib/paas-runtime/.config/systemd/user/personal-paas.service';owner=uid;group=user.pw_gid
    else:target='/etc/systemd/system/'+source.name;owner=group=0
    write(target,source.read_bytes(),owner=owner,group=group)
write('/etc/paas-traefik/traefik.yml',(root/'deploy/traefik.yml').read_bytes(),0o640,0,__import__('grp').getgrnam('paas-proxy').gr_gid)
for name in ['origin-firewall.py','renew-certificate.sh']:
    write('/usr/local/lib/personal-paas/'+name,(root/'deploy'/name).read_bytes(),0o755)
run(['systemctl','daemon-reload']);userrun(['systemctl','--user','daemon-reload'])
if not pathlib.Path('/var/lib/paas-runtime/.config/systemd/user/docker.service').exists():
    userrun(['dockerd-rootless-setuptool.sh','install','--force'])
userrun(['systemctl','--user','enable','--now','docker'])
info=json.loads(userrun(['docker','--host','unix:///run/user/'+str(uid)+'/docker.sock','info','--format','{{json .}}']))
if not any('rootless' in v for v in info['SecurityOptions']) or info['CgroupVersion']!='2' or not all(info[n] for n in ['MemoryLimit','CpuCfsQuota','PidsLimit']):raise SystemExit('rootless cgroup memory/CPU/PID enforcement missing')
# Obtain the certificate before exposing any HTTPS route or changing DNS.
run(['systemctl','start','paas-acme.service'])
run(['systemctl','enable','--now','paas-acme.timer','paas-origin-firewall.service','paas-origin-firewall-refresh.timer'])
if changed.intersection(['/usr/local/bin/paas','/etc/personal-paas/policy.json','/var/lib/paas-runtime/.config/systemd/user/personal-paas.service']+credential_paths):
    userrun(['systemctl','--user','restart','personal-paas'])
userrun(['systemctl','--user','enable','--now','personal-paas'])
if '/etc/paas-traefik/traefik.yml' in changed and subprocess.run(['systemctl','is-active','--quiet','paas-traefik']).returncode==0:run(['systemctl','restart','paas-traefik'])
run(['systemctl','enable','--now','paas-traefik'])
for attempt in range(30):
    try:
        with urllib.request.urlopen('http://127.0.0.1:9080/healthz',timeout=2) as r:
            if r.status==200:break
    except Exception:time.sleep(1)
else:raise SystemExit('controller failed its readiness check')
if a.configure_cloudflare:run(['python3',str(root/'deploy/cloudflare-setup.py'),str(a.config.resolve()),str(a.credentials.resolve())])
print('Installed commit '+bundle['commit']+' for '+c['domain']+' with '+str(len(c['owners']))+' trusted owners.')
print('Export /var/lib/paas-runtime/backup.agekey to a protected off-host location. Re-running preserves state, volumes, age identity, and existing Docker services.')
