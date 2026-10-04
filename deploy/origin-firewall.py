#!/usr/bin/python3
"""Atomically update only our inet table, preserving SSH, Xray and other firewall chains."""
import urllib.request,ipaddress,subprocess,pathlib,json,sys,os
cache=pathlib.Path('/etc/personal-paas/cloudflare-addresses.json')
request=urllib.request.Request('https://api.cloudflare.com/client/v4/ips',headers={'User-Agent':'Ryanl-Personal-PaaS/1.0'})
fresh=False
try:
    with urllib.request.urlopen(request,timeout=30) as response:data=json.loads(response.read(65536))
    if data.get('success') is not True:raise RuntimeError('Cloudflare address API failed')
    fresh=True
except Exception:
    if not cache.is_file():raise
    data=json.loads(cache.read_text())
    print('Cloudflare address API unavailable; using the last validated origin allowlist',file=sys.stderr)
networks=[]
for family,key,version in [('ip','ipv4_cidrs',4),('ip6','ipv6_cidrs',6)]:
    addresses=[ipaddress.ip_network(v) for v in data['result'][key]]
    if len(addresses)<5 or any(v.version!=version or v.prefixlen<(8 if version==4 else 16) for v in addresses):raise RuntimeError('invalid Cloudflare address list')
    networks.append((family,[str(v) for v in addresses]))
existing=subprocess.run(['nft','list','table','inet','paas_origin'],capture_output=True).returncode==0
script=('delete table inet paas_origin\n' if existing else '')+'table inet paas_origin {\n chain input {\n type filter hook input priority -5; policy accept;\n iifname "lo" accept\n'
for family,values in networks:script+=family+' saddr { '+', '.join(values)+' } tcp dport { 80, 443 } accept\n'
script+='tcp dport { 80, 443 } drop\n }\n}\n'
subprocess.run(['nft','--check','-f','-'],input=script,text=True,check=True)
subprocess.run(['nft','-f','-'],input=script,text=True,check=True)
pathlib.Path('/etc/personal-paas/origin-firewall.nft').write_text(script.replace('delete table inet paas_origin\n',''))
if fresh:
    temporary=cache.with_suffix('.new')
    temporary.write_text(json.dumps(data));temporary.chmod(0o644);os.replace(temporary,cache)
