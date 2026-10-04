#!/usr/bin/python3
"""Run as root. Import an explicitly supplied protected credential bundle without outputting it."""
import json,os,sys,pwd,pathlib,base64,subprocess
if os.geteuid()!=0:raise SystemExit('root required for initial credential installation')
source=pathlib.Path(sys.argv[1]);data=json.loads(source.read_text());user=pwd.getpwnam('paas-runtime');acme=pwd.getpwnam('paas-acme')
allowed={'githubAppId','githubAppPrivateKey','ghcrUsername','ghcrReadToken','cloudflareDnsToken','acmeEmail','workflowSha'}
if set(data)!=allowed:raise SystemExit('credential bundle fields do not match')
def write(path,value,uid,gid,mode=0o600):
    p=pathlib.Path(path);p.parent.mkdir(parents=True,exist_ok=True);fd=os.open(str(p),os.O_WRONLY|os.O_CREAT|os.O_TRUNC,mode)
    with os.fdopen(fd,'w') as f:f.write(value)
    os.chown(p,uid,gid);os.chmod(p,mode)
def safe(value):
    if not isinstance(value,str) or any(c in value for c in '\r\n\x00'):raise SystemExit('invalid credential value')
    return value
write('/var/lib/paas-runtime/github-app.pem',data['githubAppPrivateKey'],user.pw_uid,user.pw_gid)
registry={'auths':{'ghcr.io':{'auth':base64.b64encode((safe(data['ghcrUsername'])+':'+safe(data['ghcrReadToken'])).encode()).decode()}}}
write('/var/lib/paas-runtime/docker/config.json',json.dumps(registry),user.pw_uid,user.pw_gid)
recipient=subprocess.check_output(['age-keygen','-y','/var/lib/paas-runtime/backup.agekey'],text=True).strip()
write('/etc/personal-paas/runtime.env',f'DOCKER_HOST=unix:///run/user/{user.pw_uid}/docker.sock\nGITHUB_APP_ID={safe(str(data["githubAppId"]))}\nAGE_RECIPIENT={recipient}\n',0,user.pw_gid,0o640)
write('/etc/paas-acme/dns.env',f'CF_DNS_API_TOKEN={safe(data["cloudflareDnsToken"])}\nCF_ZONE_API_TOKEN={safe(data["cloudflareDnsToken"])}\nACME_EMAIL={safe(data["acmeEmail"])}\n',0,acme.pw_gid,0o640)
sha=safe(data['workflowSha'])
if len(sha)!=40 or any(c not in '0123456789abcdef' for c in sha):raise SystemExit('workflow SHA required')
policy=json.loads(pathlib.Path('/etc/personal-paas/policy.json').read_text())
policy['workflows']={f'Deploy-ryanl-in/personal-paas/.github/workflows/{name}.yml@{sha}':sha for name in ['release','operate']}
write('/etc/personal-paas/policy.json',json.dumps(policy,indent=2)+'\n',0,user.pw_gid,0o640)
print('Protected runtime and ACME credentials installed; values were not printed.')
