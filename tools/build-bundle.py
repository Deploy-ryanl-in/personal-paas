#!/usr/bin/env python3
"""Package the precompiled controller plus reviewed installation sources."""
import pathlib,json,sys,hashlib,shutil,tarfile,subprocess
root=pathlib.Path(__file__).resolve().parents[1]
sha=sys.argv[1];out=pathlib.Path(sys.argv[2]).resolve();out.mkdir(parents=True,exist_ok=True)
bundle=out/'personal-paas';bundle.mkdir(exist_ok=True)
for name in ['deploy','tools','schema']:
    shutil.copytree(root/name,bundle/name,dirs_exist_ok=True,ignore=shutil.ignore_patterns('__pycache__','*.pyc'))
shutil.copy2(root/'pins.json',bundle/'pins.json')
(bundle/'bin').mkdir(exist_ok=True)
shutil.copy2(root/'paas',bundle/'bin/paas')
(bundle/'bundle.json').write_text(json.dumps({'commit':sha,'controllerSha256':hashlib.sha256((bundle/'bin/paas').read_bytes()).hexdigest()},indent=2)+'\n')
archive=out/'personal-paas-linux-amd64.tar.gz'
with tarfile.open(archive,'w:gz') as t:t.add(bundle,arcname='personal-paas')
(out/'SHA256SUMS').write_text(hashlib.sha256(archive.read_bytes()).hexdigest()+'  '+archive.name+'\n')
