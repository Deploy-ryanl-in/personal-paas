#!/usr/bin/env python3
"""Generate independent starter templates from pinned sources for an installation."""
import argparse,pathlib,sys,re,shutil,tempfile,urllib.request,tarfile,io
root=pathlib.Path(__file__).resolve().parents[1];sys.path.insert(0,str(root/'deploy'))
from configuration import read,validate,audience
p=argparse.ArgumentParser();p.add_argument('--config',required=True,type=pathlib.Path);p.add_argument('--output',required=True,type=pathlib.Path);p.add_argument('--source',type=pathlib.Path)
a=p.parse_args();c=validate(read(a.config));pins=read(root/'pins.json')
if a.output.exists() and any(a.output.iterdir()):raise SystemExit('output must be empty; refusing to overwrite projects')
a.output.mkdir(parents=True,exist_ok=True)
for kind in ['aspnet','nextjs']:
 target=a.output/kind
 if a.source:shutil.copytree(a.source/kind,target,ignore=shutil.ignore_patterns('.git','node_modules','.next','bin','obj','.env','.env.local'))
 else:
  pin=pins['templates'][kind];url='https://api.github.com/repos/'+pin['repository']+'/tarball/'+pin['sha']
  with urllib.request.urlopen(urllib.request.Request(url,headers={'User-Agent':'Personal-PaaS-Template/1.0'}),timeout=60) as r:data=r.read(10_000_000)
  with tempfile.TemporaryDirectory() as d:
   with tarfile.open(fileobj=io.BytesIO(data)) as t:t.extractall(d,filter='data')
   shutil.copytree(next(pathlib.Path(d).iterdir()),target)
 for name in ['ci.yml','operations.yml']:
  file=target/'.github/workflows'/name;text=file.read_text()
  text=re.sub(r'uses: [A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/\.github/workflows/(release|operate)\.yml@[a-f0-9]{40}',lambda m:'uses: '+c['platformRepository']+'/.github/workflows/'+m.group(1)+'.yml@'+c['platformSha'],text)
  text=re.sub(r'platform-sha: [a-f0-9]{40}','platform-sha: '+c['platformSha'],text)
  text=re.sub(r'^      (deployment-url|domain|platform-repository):.*\n','',text,flags=re.M)
  if name=='ci.yml':text=text.replace('      platform-sha:','      deployment-url: '+audience(c)+'\n      domain: '+c['domain']+'\n      platform-repository: '+c['platformRepository']+'\n      platform-sha:')
  else:text=text.replace('    with:\n','    with:\n      deployment-url: '+audience(c)+'\n',1)
  file.write_text(text)
 owners=', '.join(o['login'] for o in c['owners'])
 (target/'PLATFORM.md').write_text('Bound to '+audience(c)+' and '+c['platformRepository']+' at '+c['platformSha']+'.\n\nTrusted owners: '+owners+'.\nCreate an independent repository from this template, clone, develop, and push. Default domain: <new-repository-name>.'+c['domain']+'. No per-repository DNS or credentials are required for the default web example.\n')
 file=target/'README.md';text=file.read_text().replace('ryanl.in',c['domain']).replace('owner 选择 `Deploy-ryanl-in`','owner 选择 '+owners).replace('https://github.com/Deploy-ryanl-in/你的仓库.git','https://github.com/'+c['owners'][0]['login']+'/你的仓库.git');file.write_text(text)
print('Prepared both templates with immutable workflows and '+c['domain']+' defaults. Publish these directories as template repositories.')
