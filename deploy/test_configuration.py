import unittest,copy,json,pathlib
from configuration import validate,policy,unique
class ConfigurationTests(unittest.TestCase):
 def setUp(self):
  self.c=json.loads((pathlib.Path(__file__).parent/'installation.example.json').read_text())
 def test_rebind_domain_and_multiple_owners(self):
  self.c['domain']='example.net';self.c['controlSubdomain']='ship';self.c['platformRepository']='NewOwner/platform'
  p=policy(validate(self.c),{'images':{'postgres':'pg@sha256:a','redis':'redis@sha256:b','alpine':'helper@sha256:c'}})
  self.assertEqual(p['audience'],'https://ship.example.net');self.assertEqual(len(p['owners']),2)
  self.assertTrue(all(ref.startswith('NewOwner/platform/') for ref in p['workflows']))
 def test_invalid_install_inputs(self):
  for key,value in [('schemaVersion',True),('domain','evil.net` || true'),('controlSubdomain','nested.name'),('originIPv4','::1'),('platformRepository','owner/repo;cmd'),('platformSha','main'),('shell','echo hi')]:
   c=copy.deepcopy(self.c);c[key]=value
   with self.subTest(key=key),self.assertRaises(ValueError):validate(c)
  self.c['owners'].append(copy.deepcopy(self.c['owners'][0]))
  with self.assertRaises(ValueError):validate(self.c)
 def test_duplicate_keys(self):
  with self.assertRaises(ValueError):json.loads('{"domain":"a.net","domain":"b.net"}',object_pairs_hook=unique)

class CredentialAndPreflightTests(unittest.TestCase):
 def setUp(self):
  self.c=json.loads((pathlib.Path(__file__).parent/'installation.example.json').read_text())
  self.credentials={'ghcrUsername':'example','ghcrReadToken':'readonly-token','cloudflareDnsToken':'dns-token','acmeEmail':'ops@example.net'}
 def test_default_job_token_mode_needs_no_app(self):
  from unittest.mock import patch
  from preflight import check
  identities={o['login']:o for o in self.c['owners']}
  def get(path,token=None):
   self.assertIsNone(token);o=identities[path.removeprefix('/users/')]
   return {'id':int(o['id']),'type':o['type'],'login':o['login']}
  with patch('preflight.github_get',side_effect=get):check(self.c,self.credentials)
  from configuration import policy
  self.assertTrue(policy(self.c,{'images':{'postgres':'p','redis':'r','alpine':'a'}})['allowJobToken'])
 def test_identity_spoof_and_incomplete_app_rejected(self):
  from unittest.mock import patch
  from preflight import check
  from configuration import validate_credentials
  with patch('preflight.github_get',return_value={'id':999,'type':'User'}),self.assertRaises(ValueError):check(self.c,self.credentials)
  o=self.c['owners'][0]
  with patch('preflight.github_get',return_value={'id':int(o['id']),'type':o['type'],'login':'RenamedOwner'}),self.assertRaises(ValueError):check(self.c,self.credentials)
  with self.assertRaises(ValueError):validate_credentials(dict(self.credentials,githubAppId='123'))
  self.c['repositoryVerification']='app'
  with self.assertRaises(ValueError):check(self.c,self.credentials)
