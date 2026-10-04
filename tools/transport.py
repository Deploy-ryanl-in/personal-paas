"""This code is embedded in trusted reusable workflow steps, not fetched from the app."""
import json, os, time, urllib.request, urllib.error, pathlib, sys
BASE='https://deploy.ryanl.in'
class TransientRequestError(RuntimeError):pass
def request(url,method='GET',body=None,token=None,binary=False):
    headers={'Accept':'application/json','User-Agent':'Ryanl-Personal-PaaS/1.0 (+https://github.com/Deploy-ryanl-in/personal-paas)'}
    if token: headers['Authorization']='Bearer '+token
    if body is not None: headers['Content-Type']='application/json'
    req=urllib.request.Request(url,data=None if body is None else json.dumps(body).encode(),headers=headers,method=method)
    try:
        with urllib.request.urlopen(req,timeout=60) as r: return r.read() if binary else json.load(r)
    except urllib.error.HTTPError as e:
        detail=e.read(2048).decode(errors='replace')
        error=TransientRequestError if e.code in [429,502,503,504] else RuntimeError
        raise error('API HTTP '+str(e.code)+': '+detail) from None
    except (urllib.error.URLError,TimeoutError) as e:
        raise TransientRequestError('API temporarily unreachable') from None
def oidc():
    url=os.environ['ACTIONS_ID_TOKEN_REQUEST_URL']+'&audience=https%3A%2F%2Fdeploy.ryanl.in'
    return request(url,token=os.environ['ACTIONS_ID_TOKEN_REQUEST_TOKEN'])['value']
def api(path,method='GET',body=None,binary=False):
    for attempt in range(6):
        try:return request(BASE+'/v1/'+path,method,body,oidc(),binary)
        except TransientRequestError:
            if attempt==5:raise
            time.sleep(min(2**attempt,10))
    raise RuntimeError('API retry limit exceeded')
def poll(job):
    for _ in range(240):
        state=api('deployments/'+job['id'])
        if state['status'] in ['failed','superseded']: raise RuntimeError(state.get('error','deployment failed'))
        if state['status']=='succeeded':
            release=state['release'];domains=[s['domain'] for s in release['config']['services'].values() if s['type']=='web']
            with open(os.environ['GITHUB_STEP_SUMMARY'],'a') as f:
                f.write('Commit: `'+release['commit']+'`\n\nRelease: `'+state['id']+'`\n\n')
                for domain in domains:f.write('- https://'+domain+'\n')
            return state
        time.sleep(5)
    raise RuntimeError('deployment deadline exceeded')
if os.environ['PAAS_MODE']=='deploy':
    repo=os.environ['GITHUB_REPOSITORY'];sha=os.environ['GITHUB_SHA']
    manifest=request('https://api.github.com/repos/'+repo+'/contents/paas.json?ref='+sha,token=os.environ['GH_TOKEN'])
    import base64
    def unique(pairs):
        result={}
        for k,v in pairs:
            if k in result:raise ValueError('duplicate manifest key')
            result[k]=v
        return result
    config=json.loads(base64.b64decode(manifest['content']),object_pairs_hook=unique)
    supplied=json.loads(os.environ.get('PAAS_SECRETS') or '{}',object_pairs_hook=unique)
    wanted={ref for s in config['services'].values() for ref in s.get('secretRefs',{}).values()}
    selected={ref:supplied[ref] for ref in wanted}
    for value in selected.values():
        if not isinstance(value,str) or '\n' in value or '\r' in value:raise ValueError('secrets must be single-line strings')
        print('::add-mask::'+value)
    images={}
    for file in pathlib.Path('images').glob('*.json'):
        value=json.loads(file.read_text());images[value['service']]=value['image']+'@'+value['digest']
    poll(api('deployments','POST',{'images':images,'secrets':selected}))
else:
    action=os.environ['OP_ACTION']
    if action in ['status','history','backups','logs']:
        route=action
        if action=='logs':
            import urllib.parse
            route+='?service='+urllib.parse.quote(os.environ['OP_SERVICE'])
        print(json.dumps(api(route),indent=2))
    elif action=='download-backup':
        import urllib.parse
        data=api('backups/'+urllib.parse.quote(os.environ['OP_BACKUP'],safe=''),binary=True)
        pathlib.Path('backup.tar.age').write_bytes(data)
    else:
        body={'action':action}
        for env,key in [('OP_RELEASE','releaseId'),('OP_SERVICE','service'),('OP_IMAGE','image'),('OP_BACKUP','backupId'),('OP_CONFIRM','confirm')]:
            if os.environ.get(env):body[key]=os.environ[env]
        print(json.dumps(poll(api('operations','POST',body)),indent=2))
