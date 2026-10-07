import contextlib,io,json,os,pathlib,runpy,tempfile,unittest,urllib.error
from unittest.mock import patch

class CacheWorkflowTests(unittest.TestCase):
 def execute(self,status=200,error=None):
  self.requests=[]
  with tempfile.TemporaryDirectory() as directory:
   summary=pathlib.Path(directory)/'summary.md'
   environment={'PAAS_MODE':'operation','OP_ACTION':'clear-route-cache','OP_DOMAIN':'endpoint.ryanl.in','GITHUB_EVENT_NAME':'push','GH_TOKEN':'fixture-read','ACTIONS_ID_TOKEN_REQUEST_URL':'https://oidc.invalid/request?job=1','ACTIONS_ID_TOKEN_REQUEST_TOKEN':'fixture-job','GITHUB_STEP_SUMMARY':str(summary)}
   def urlopen(request,timeout):
    self.requests.append(request)
    if request.full_url.startswith('https://oidc.invalid/'):
     return io.BytesIO(b'{"value":"fixture-oidc"}')
    self.assertEqual(request.full_url,'https://deploy.ryanl.in/v1/route-cache/clear')
    self.assertEqual(request.headers['Authorization'],'Bearer fixture-oidc')
    self.assertEqual(json.loads(request.data),{'domain':'endpoint.ryanl.in'})
    if status!=200:raise urllib.error.HTTPError(request.full_url,status,'rejected',{},io.BytesIO(json.dumps({'error':error}).encode()))
    return io.BytesIO(b'{"clearedEntries":1,"entries":{"endpoint.ryanl.in":0},"warning":"Invalid declaration: previous domains only"}')
   with patch.dict(os.environ,environment,clear=True),patch('urllib.request.urlopen',side_effect=urlopen),contextlib.redirect_stdout(io.StringIO()):
    runpy.run_path(str(pathlib.Path(__file__).resolve().parents[1]/'tools/transport.py'))
   return summary.read_text()
 def test_clear_does_not_parse_untrusted_manifest_on_runner(self):
  summary=self.execute()
  self.assertEqual(len(self.requests),2)
  self.assertIn('Cleared entries: `1`',summary)
 def test_only_non_deployment_branch_is_a_successful_skip(self):
  with self.assertRaises(SystemExit) as result:self.execute(403,'branch not authorized by default-branch manifest')
  self.assertEqual(result.exception.code,0)
 def test_identity_failure_is_not_silently_skipped(self):
  with self.assertRaises(RuntimeError):self.execute(403,'repository ownership verification failed')
 def test_invalid_oidc_is_not_silently_skipped(self):
  with self.assertRaises(RuntimeError):self.execute(401,'OIDC token expired')
