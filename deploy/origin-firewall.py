#!/usr/bin/python3
"""Atomically update only our inet table, leaving SSH, Xray and all other chains untouched."""
import urllib.request,ipaddress,subprocess,pathlib
networks=[]
for family,url in [('ip','https://www.cloudflare.com/ips-v4'),('ip6','https://www.cloudflare.com/ips-v6')]:
    with urllib.request.urlopen(url,timeout=30) as r:lines=r.read().decode().splitlines()
    values=[str(ipaddress.ip_network(v.strip())) for v in lines if v.strip()]
    if len(values)<5:raise RuntimeError('incomplete Cloudflare address list')
    networks.append((family,values))
existing=subprocess.run(['nft','list','table','inet','paas_origin'],capture_output=True).returncode==0
script=('delete table inet paas_origin\n' if existing else '')+'table inet paas_origin {\n chain input {\n type filter hook input priority -5; policy accept;\n iifname "lo" accept\n'
for family,values in networks:script+=family+' saddr { '+', '.join(values)+' } tcp dport { 80, 443 } accept\n'
script+='tcp dport { 80, 443 } drop\n }\n}\n'
subprocess.run(['nft','--check','-f','-'],input=script,text=True,check=True)
subprocess.run(['nft','-f','-'],input=script,text=True,check=True)
pathlib.Path('/etc/personal-paas/origin-firewall.nft').write_text(script.replace('delete table inet paas_origin\n',''))
