"""Strict, non-secret installation configuration shared by setup and template tools."""
import json, re, ipaddress, urllib.request
from pathlib import Path

def unique(pairs):
    result = {}
    for key, value in pairs:
        if key in result: raise ValueError('duplicate configuration key: '+key)
        result[key] = value
    return result

def read(path): return json.loads(Path(path).read_text(), object_pairs_hook=unique)

def validate(c):
    required = {'schemaVersion','domain','controlSubdomain','originIPv4','cloudflareZoneId','platformRepository','platformSha','owners'}
    optional = {'previousWorkflowShas','repositoryAllowlist'}
    if set(c)-required-optional or required-set(c): raise ValueError('installation fields do not match schema')
    if type(c['schemaVersion']) is not int or c['schemaVersion'] != 1: raise ValueError('unsupported installation schema')
    label = r'[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?'
    if not re.fullmatch(label+r'(?:\.'+label+r')+',c['domain']) or len(c['domain'])>190: raise ValueError('invalid domain')
    if not re.fullmatch(label,c['controlSubdomain']): raise ValueError('invalid control subdomain')
    if ipaddress.ip_address(c['originIPv4']).version != 4: raise ValueError('IPv4 origin required')
    if not re.fullmatch('[a-f0-9]{32}',c['cloudflareZoneId']): raise ValueError('invalid Cloudflare zone ID')
    if not re.fullmatch(r'[A-Za-z0-9-]+/[A-Za-z0-9_.-]+',c['platformRepository']): raise ValueError('invalid platform repository')
    for sha in [c['platformSha']]+c.get('previousWorkflowShas',[]):
        if not re.fullmatch('[a-f0-9]{40}',sha): raise ValueError('full workflow commit SHA required')
    if not c['owners'] or not isinstance(c['owners'],list): raise ValueError('at least one owner required')
    ids, logins = set(), set()
    for owner in c['owners']:
        if set(owner) != {'id','login','type'} or owner['type'] not in ['User','Organization']: raise ValueError('invalid owner fields')
        if not isinstance(owner['id'],str) or not re.fullmatch('[1-9][0-9]*',owner['id']): raise ValueError('immutable owner ID required')
        if not re.fullmatch('[A-Za-z0-9][A-Za-z0-9-]{0,38}',owner['login']): raise ValueError('invalid owner login')
        if owner['id'] in ids or owner['login'].lower() in logins: raise ValueError('duplicate owner binding')
        ids.add(owner['id']);logins.add(owner['login'].lower())
    for repo in c.get('repositoryAllowlist',[]):
        if not isinstance(repo,str) or not re.fullmatch('[1-9][0-9]*',repo): raise ValueError('invalid repository ID')
    return c

def audience(c): return 'https://'+c['controlSubdomain']+'.'+c['domain']

def policy(c, pins):
    refs = {}
    for sha in [c['platformSha']]+c.get('previousWorkflowShas',[]):
        for name in ['release','operate']:
            refs[c['platformRepository']+'/.github/workflows/'+name+'.yml@'+sha] = sha
    return {'audience':audience(c),'domain':c['domain'], 'owners':[{'id':o['id'],'login':o['login']} for o in c['owners']],
            'repositoryAllowlist':c.get('repositoryAllowlist',[]),'workflows':refs,
            'databaseImages':{pins['images'][n]:n for n in ['postgres','redis']},'helperImage':pins['images']['alpine']}

def github_get(path, token=None):
    headers={'User-Agent':'Personal-PaaS-Setup/1.0','Accept':'application/vnd.github+json'}
    if token: headers['Authorization']='Bearer '+token
    with urllib.request.urlopen(urllib.request.Request('https://api.github.com'+path,headers=headers),timeout=30) as r:
        return json.load(r)

def resolve_owners(logins):
    return [{'id':str(o['id']),'login':o['login'],'type':o['type']} for o in (github_get('/users/'+login) for login in logins)]
