#!/usr/bin/env python3
"""Manage only the configured wildcard and Full (strict); never edits infrastructure records."""
import sys,json,urllib.request,urllib.error
from configuration import read,validate
c=validate(read(sys.argv[1]));secrets=read(sys.argv[2]);token=secrets.get('cloudflareSetupToken',secrets['cloudflareDnsToken'])
base='https://api.cloudflare.com/client/v4/zones/'+c['cloudflareZoneId']
def api(method,path,body=None):
    req=urllib.request.Request(base+path,data=None if body is None else json.dumps(body).encode(),method=method,headers={'Authorization':'Bearer '+token,'Content-Type':'application/json','User-Agent':'Personal-PaaS-Setup/1.0'})
    try:
        with urllib.request.urlopen(req,timeout=30) as response:data=json.load(response)
    except urllib.error.HTTPError as e:raise SystemExit('Cloudflare setup HTTP '+str(e.code)+'; check zone-scoped DNS/SSL setup permissions') from None
    if not data.get('success'):raise SystemExit('Cloudflare setup rejected; check token permissions')
    return data['result']
zone=api('GET','')
if zone['name']!=c['domain']:raise SystemExit('Cloudflare zone/domain mismatch')
name='*.'+c['domain'];records=api('GET','/dns_records?name='+name)
if len(records)>1 or records and records[0]['type']!='A':raise SystemExit('conflicting wildcard; resolve explicitly before installation')
body={'type':'A','name':name,'content':c['originIPv4'],'ttl':1,'proxied':True}
if records:
    if any(records[0].get(k)!=v for k,v in body.items()):api('PATCH','/dns_records/'+records[0]['id'],body)
else:api('POST','/dns_records',body)
ssl=api('GET','/settings/ssl')
if ssl['value']!='strict':api('PATCH','/settings/ssl',{'value':'strict'})
print('Wildcard and Full (strict) verified; other DNS records untouched')
