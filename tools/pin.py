#!/usr/bin/env python3
"""Resolve official versions/digests and GitHub Action commits; never reads credentials."""
import json, urllib.request, pathlib, base64
root=pathlib.Path(__file__).resolve().parents[1]
def get(url,headers=None):
    with urllib.request.urlopen(urllib.request.Request(url,headers=headers or {}),timeout=60) as r:
        return json.load(r),r.headers
def image(name,tag):
    host="registry-1.docker.io"; repository=name if "/" in name else "library/"+name
    token,_=get("https://auth.docker.io/token?service=registry.docker.io&scope=repository:"+repository+":pull")
    _,headers=get(f"https://{host}/v2/{repository}/manifests/{tag}",{"Authorization":"Bearer "+token["token"],"Accept":"application/vnd.oci.image.index.v1+json,application/vnd.docker.distribution.manifest.list.v2+json"})
    return f"{name}:{tag}@{headers['Docker-Content-Digest']}"
def mcr(name,tag):
    _,headers=get(f"https://mcr.microsoft.com/v2/dotnet/{name}/manifests/{tag}",{"Accept":"application/vnd.docker.distribution.manifest.list.v2+json,application/vnd.oci.image.index.v1+json"})
    return f"mcr.microsoft.com/dotnet/{name}:{tag}@{headers['Docker-Content-Digest']}"
actions={}
for repo,ref in {"actions/checkout":"v4","actions/setup-node":"v4","actions/setup-dotnet":"v4","actions/setup-go":"v5","actions/upload-artifact":"v4","actions/download-artifact":"v4","docker/setup-buildx-action":"v3","docker/login-action":"v3","docker/build-push-action":"v6"}.items():
    data,_=get(f"https://api.github.com/repos/{repo}/commits/{ref}")
    actions[repo]=data["sha"]
pins={"actions":actions,"images":{"node":image("node","24.21.0-bookworm-slim"),"alpine":image("alpine","3.23"),"postgres":image("postgres","18.3-bookworm"),"redis":image("redis","8.2.2-bookworm"),"dotnet-sdk":mcr("sdk","10.0.401-noble"),"dotnet-runtime":mcr("aspnet","10.0.12-noble")}}
(root/"pins.json").write_text(json.dumps(pins,indent=2)+"\n")
print(json.dumps(pins,indent=2))
