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
 if a.source:shutil.copytree(a.source/kind,target,ignore=shutil.ignore_patterns('.git','node_modules','.next','bin','obj','.env*','paas.secrets.json','local-data','TestResults'))
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
  text=re.sub(r'^      (deployment-url|domain|platform-repository):.*\n',lambda m:m.group(0) if 'inputs.domain' in m.group(0) or 'description:' in m.group(0) else '',text,flags=re.M)
  text=re.sub(r'^    with:\n','    with:\n      deployment-url: '+audience(c)+'\n',text,flags=re.M)
  if name=='ci.yml':text=text.replace('      platform-sha:','      domain: '+c['domain']+'\n      platform-repository: '+c['platformRepository']+'\n      platform-sha:')
  if name=='operations.yml' and not re.search(r'^  actions: read$',text,re.M):text=text.replace('  id-token: write\n','  id-token: write\n  actions: read\n',1)
  file.write_text(text)
 owners=', '.join(o['login'] for o in c['owners'])
 (target/'PLATFORM.md').write_text('Bound to '+audience(c)+' and '+c['platformRepository']+' at '+c['platformSha']+'.\n\nTrusted owners: '+owners+'.\nCreate an independent repository from this template, clone, develop, and push. Default domain: <new-repository-name>.'+c['domain']+'. No per-repository DNS or credentials are required for the default web example.\n')
 file=target/'README.md'
 text=file.read_text().replace('个人账号仓库需要 repository ID 白名单；组织内新仓库自动接入。','安装时绑定的个人账号和组织内新仓库均自动接入，无需逐仓库白名单。').replace('组织内新仓库自动接入；个人仓库需 repository ID 白名单。','安装时绑定的个人账号和组织内新仓库均自动接入，无需逐仓库白名单。').replace('ryanl.in',c['domain']).replace('https://github.com/Deploy-ryanl-in/personal-paas/wiki','https://github.com/'+c['platformRepository']+'/wiki')
 owner_labels=' 或 '.join('`'+o['login']+'`' for o in c['owners'])
 text=re.sub(r'owner (?:选择|可选) .*?，(?:可选公有或私有|公有和私有均支持)。','owner 选择 '+owner_labels+'，可选公有或私有。',text)
 text=re.sub(r'https://github\.com/[A-Za-z0-9-]+/你的仓库\.git','https://github.com/'+c['owners'][0]['login']+'/你的仓库.git',text)
 text=re.sub(r'已绑定的 .*?新仓库均自动接入，无需逐仓库白名单。','安装时绑定的 '+owners+' 下的新仓库均自动接入，无需逐仓库白名单。',text)
 file.write_text(text)

print('Prepared both templates with immutable workflows and '+c['domain']+' defaults. Publish these directories as template repositories.')
