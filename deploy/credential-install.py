#!/usr/bin/python3
"""Import protected credentials; trust policy is managed independently, never overwritten here."""
import json,os,sys,pwd,pathlib,base64,subprocess,re
from configuration import read,validate
if os.geteuid()!=0:raise SystemExit('root required')
source=pathlib.Path(sys.argv[1]);data=read(source);config=validate(read(sys.argv[2]))
user=pwd.getpwnam('paas-runtime');acme=pwd.getpwnam('paas-acme')
required={'githubAppId','githubAppPrivateKey','ghcrUsername','ghcrReadToken','cloudflareDnsToken','acmeEmail'}
if required-set(data) or set(data)-required-{'workflowSha','cloudflareSetupToken'}:raise SystemExit('credential fields do not match')
def write(path,value,uid,gid,mode=0o600):
 p=pathlib.Path(path);temporary=p.with_name(p.name+'.new')
 fd=os.open(temporary,os.O_WRONLY|os.O_CREAT|os.O_TRUNC,mode)
 with os.fdopen(fd,'w') as f:f.write(value);f.flush();os.fsync(f.fileno())
 os.chown(temporary,uid,gid);os.chmod(temporary,mode);os.replace(temporary,p)
def safe(value):
 if not isinstance(value,str) or not re.fullmatch('[A-Za-z0-9_@.+:/=-]+',value):raise SystemExit('invalid environment credential value')
 return value
write('/var/lib/paas-runtime/github-app.pem',data['githubAppPrivateKey'],user.pw_uid,user.pw_gid)
registry={'auths':{'ghcr.io':{'auth':base64.b64encode((safe(data['ghcrUsername'])+':'+safe(data['ghcrReadToken'])).encode()).decode()}}}
write('/var/lib/paas-runtime/docker/config.json',json.dumps(registry),user.pw_uid,user.pw_gid)
recipient=subprocess.check_output(['age-keygen','-y','/var/lib/paas-runtime/backup.agekey'],text=True).strip()
write('/etc/personal-paas/runtime.env',f'DOCKER_HOST=unix:///run/user/{user.pw_uid}/docker.sock\nGITHUB_APP_ID={safe(str(data["githubAppId"]))}\nAGE_RECIPIENT={recipient}\nPATH=/opt/personal-paas/docker/bin:/usr/local/bin:/usr/bin:/bin\n',0,user.pw_gid,0o640)
# Preserve a pre-existing cert name when upgrading the original installation.
certname='paas-wildcard'
old=pathlib.Path('/etc/paas-acme/dns.env')
if old.exists():
 for line in old.read_text().splitlines():
  if line.startswith('ACME_CERT_NAME='):certname=safe(line.split('=',1)[1])
 if pathlib.Path('/var/lib/paas-acme/private/certificates/ryanl-wildcard.crt').exists():certname='ryanl-wildcard'
write('/etc/paas-acme/dns.env',f'CF_DNS_API_TOKEN={safe(data["cloudflareDnsToken"])}\nCF_ZONE_API_TOKEN={safe(data["cloudflareDnsToken"])}\nACME_EMAIL={safe(data["acmeEmail"])}\nPAAS_DOMAIN={config["domain"]}\nACME_CERT_NAME={certname}\n',0,acme.pw_gid,0o640)
print('Protected runtime and ACME credentials installed; values were not printed.')
