"""Verify account bindings and readonly App permissions before provisioning a host."""
import base64, json, subprocess, tempfile, os, time
from configuration import github_get,validate_credentials

def check(c, credentials):
    validate_credentials(credentials)
    if c.get('repositoryVerification','job-token-or-app')=='app' and not credentials.get('githubAppId'):raise ValueError('App verification mode requires an App ID and PEM')
    app=None;installs=[]
    if credentials.get("githubAppId"):
        encode=lambda value:base64.urlsafe_b64encode(json.dumps(value,separators=(',',':')).encode()).decode().rstrip('=')
        payload=encode({'alg':'RS256','typ':'JWT'})+'.'+encode({'iss':str(credentials['githubAppId']),'iat':int(time.time())-60,'exp':int(time.time())+480})
        with tempfile.NamedTemporaryFile(mode='w') as pem:
            pem.write(credentials['githubAppPrivateKey']);pem.flush()
            signature=subprocess.check_output(['openssl','dgst','-sha256','-sign',pem.name],input=payload.encode(),stderr=subprocess.DEVNULL)
        token=payload+'.'+base64.urlsafe_b64encode(signature).decode().rstrip('=')
        app=github_get('/app',token)
        permissions=app['permissions']
        if set(permissions)!={'contents','actions','metadata'} or any(v!='read' for v in permissions.values()):
            raise ValueError('runtime GitHub App must have readonly contents, actions and metadata permissions')
        installs=[]
        for page in range(1,100):
            rows=github_get('/app/installations?per_page=100&page='+str(page),token);installs+=rows
            if len(rows)<100:break
    for owner in c['owners']:
        identity=github_get('/users/'+owner['login'])
        if str(identity['id'])!=owner['id'] or identity['type']!=owner['type']:raise ValueError('GitHub owner ID/type mismatch: '+owner['login'])
        matching=[i for i in installs if str(i['account']['id'])==owner['id'] and not i.get('suspended_at')]
        if c.get('repositoryVerification','job-token-or-app')=='app' and (not matching or matching[0]['repository_selection']!='all'):
            raise ValueError('Install readonly runtime App on all current/future repositories for '+owner['login'])
    print('Verified '+str(len(c['owners']))+' immutable owner bindings; source verification: '+c.get('repositoryVerification','job-token-or-app'))
