#!/usr/bin/env python3
import urllib.request,json,pathlib,hashlib,tarfile
root=pathlib.Path(__file__).resolve().parents[1];target=root.parent/'private/binaries';target.mkdir(parents=True,exist_ok=True)
pins=json.loads((root/'pins.json').read_text());pins['binaries']={}
for name,repo in [('traefik','traefik/traefik'),('lego','go-acme/lego')]:
    with urllib.request.urlopen('https://api.github.com/repos/'+repo+'/releases/latest',timeout=60) as r:release=json.load(r)
    asset=next(a for a in release['assets'] if a['name'].endswith('linux_amd64.tar.gz'))
    sums=next(a for a in release['assets'] if a['name'].endswith('checksums.txt'))
    with urllib.request.urlopen(sums['browser_download_url'],timeout=60) as r:checksums=r.read().decode().splitlines()
    expected=next(line.split()[0] for line in checksums if line.split()[-1].lstrip('*')==asset['name'])
    archive=target/asset['name'];urllib.request.urlretrieve(asset['browser_download_url'],archive)
    actual=hashlib.sha256(archive.read_bytes()).hexdigest()
    if actual!=expected:raise RuntimeError('release checksum mismatch')
    with tarfile.open(archive) as t:
        member=t.getmember(name)
        with t.extractfile(member) as f:(target/name).write_bytes(f.read())
    pins['binaries'][name]={'version':release['tag_name'],'archive':asset['name'],'sha256':actual,'url':asset['browser_download_url']}
(root/'pins.json').write_text(json.dumps(pins,indent=2)+'\n')
print(json.dumps(pins['binaries'],indent=2))
