#!/usr/bin/env python3
"""Prepare reviewable installation inputs on a workstation; never creates credentials."""
import argparse, json, pathlib, re, sys
from configuration import validate, policy, read, resolve_owners
parser=argparse.ArgumentParser()
parser.add_argument('--config', required=True, type=pathlib.Path)
parser.add_argument('--output', required=True, type=pathlib.Path)
parser.add_argument('--resolve-owners', nargs='+', help='Resolve account/organization logins to immutable GitHub IDs')
a=parser.parse_args();c=read(a.config)
if a.resolve_owners: c['owners']=resolve_owners(a.resolve_owners)
validate(c)
root=pathlib.Path(__file__).resolve().parents[1]
a.output.mkdir(parents=True,exist_ok=True)
(a.output/'installation.json').write_text(json.dumps(c,indent=2)+'\n')
(a.output/'policy.json').write_text(json.dumps(policy(c,read(root/'pins.json')),indent=2)+'\n')
print('Prepared non-secret installation.json and policy.json; inspect owner IDs, domain, origin and approved workflow commits before installation.')
