package server

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Deploy-ryanl-in/personal-paas/internal/auth"
	"github.com/Deploy-ryanl-in/personal-paas/internal/github"
	"github.com/Deploy-ryanl-in/personal-paas/internal/store"
)

type tokenTransport func(*http.Request) (*http.Response, error)

func (f tokenTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestPrivateDeployUsesReadTokenAfterOIDCThroughManifestFetch(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("a", 40)
	ref := "Deploy-ryanl-in/personal-paas/.github/workflows/release.yml@" + sha
	claims := auth.Claims{Iss: auth.Issuer, Aud: "https://deploy.ryanl.in", Iat: time.Now().Unix(), Nbf: time.Now().Unix(), Exp: time.Now().Add(5 * time.Minute).Unix(), JTI: "private-job", Repository: "RyanStanLin/private-demo", RepositoryID: "42", Owner: "RyanStanLin", OwnerID: "93820487", Ref: "refs/heads/main", SHA: sha, Event: "push", RunID: "123", WorkflowRef: ref, WorkflowSHA: sha}
	b, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"test"}`)) + "." + base64.RawURLEncoding.EncodeToString(b)
	hash := sha256.Sum256([]byte(payload))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	signed := payload + "." + base64.RawURLEncoding.EncodeToString(signature)
	manifest := `{"schemaVersion":1,"name":"auto","state":"absent","deployBranch":"main","services":{"web":{"type":"web","build":{"context":".","dockerfile":"Dockerfile"},"port":8080,"domain":"auto","health":{"path":"/healthz","status":200},"resources":{"memoryMiB":192,"cpu":0.5,"pids":128}}}}`
	githubReads := 0
	legacyApp := false
	client := &http.Client{Transport: tokenTransport(func(r *http.Request) (*http.Response, error) {
		var result any
		if r.URL.Host == "token.actions.githubusercontent.com" {
			result = map[string]any{"keys": []any{map[string]string{"kid": "test", "kty": "RSA", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB"}}}
		} else {
			githubReads++
			expectedToken := "job-read"
			if legacyApp {
				expectedToken = "legacy-read"
			}
			if r.URL.Host != "api.github.com" {
				t.Fatalf("unexpected GitHub request %s %s", r.Method, r.URL)
			}
			switch r.URL.Path {
			case "/repos/RyanStanLin/private-demo/installation", "/app/installations/77/access_tokens":
				if !legacyApp || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer eyJ") {
					t.Fatal("legacy App token acquisition used the supplied job token")
				}
				if r.URL.Path == "/repos/RyanStanLin/private-demo/installation" {
					if r.Method != "GET" {
						t.Fatal("installation lookup must be GET")
					}
					result = map[string]int{"id": 77}
				} else {
					var request struct {
						RepositoryIDs []int64           `json:"repository_ids"`
						Permissions   map[string]string `json:"permissions"`
					}
					if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&request) != nil || len(request.RepositoryIDs) != 1 || request.RepositoryIDs[0] != 42 || len(request.Permissions) != 2 || request.Permissions["contents"] != "read" || request.Permissions["actions"] != "read" {
						t.Fatal("App token must remain scoped to one repository and read permissions")
					}
					result = map[string]any{"token": "legacy-read", "expires_at": time.Now().Add(time.Hour)}
				}
			case "/repos/RyanStanLin/private-demo":
				result = map[string]any{"id": 42, "name": "private-demo", "full_name": "RyanStanLin/private-demo", "default_branch": "main", "owner": map[string]any{"id": 93820487, "login": "RyanStanLin"}}
			case "/repos/RyanStanLin/private-demo/contents/paas.json":
				result = map[string]any{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(manifest)), "size": len(manifest)}
			case "/repos/RyanStanLin/private-demo/actions/runs/123":
				result = map[string]any{"head_sha": sha, "head_branch": "main", "event": "push", "repository": map[string]int{"id": 42}, "head_repository": map[string]int{"id": 42}}
			default:
				t.Fatalf("unexpected endpoint %s", r.URL)
			}
			if !strings.Contains(r.URL.Path, "/installation") && (r.Method != "GET" || r.Header.Get("Authorization") != "Bearer "+expectedToken) {
				t.Fatal("repository reads used the wrong token")
			}
		}
		data, _ := json.Marshal(result)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(data))}, nil
	})}
	state, err := store.Open(filepath.Join(t.TempDir(), "state.db"), make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer state.DB.Close()
	s := Server{Verifier: &auth.Verifier{Client: client, Policy: auth.Policy{Audience: claims.Aud, AllowJobToken: true, Owners: []auth.Owner{{ID: claims.OwnerID, Login: claims.Owner}}, Workflows: map[string]string{ref: sha}}}, GitHub: &github.Client{HTTP: client}, Store: state}
	call := func(token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/v1/deployments", strings.NewReader(`{"images":{},"secrets":{}}`))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-GitHub-Read-Token", "job-read")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	w := call("invalid")
	if w.Code != 401 || githubReads != 0 {
		t.Fatalf("unverified OIDC reached GitHub: %d %d", w.Code, githubReads)
	}
	s.Verifier.Policy.AllowJobToken = false
	w = call(signed)
	if w.Code != 403 || githubReads != 0 {
		t.Fatal("disabled read-token policy was ignored")
	}
	s.Verifier.Policy.AllowJobToken = true
	w = call(signed)
	if w.Code != 202 || githubReads != 4 {
		t.Fatalf("private request failed %d reads=%d: %s", w.Code, githubReads, w.Body.String())
	}
	if _, err = s.GitHub.Token(t.Context(), claims.Repository, 42); err == nil {
		t.Fatal("job token was retained outside the HTTP request")
	}
	// Current templates always send a read token; strict App installations must
	// ignore it and continue using their independently scoped App credential.
	legacyApp = true
	s.Verifier.Policy.AllowJobToken = false
	s.GitHub.AppID, s.GitHub.Key = "77", key
	r := httptest.NewRequest("GET", "/v1/status", nil)
	r.Header.Set("Authorization", "Bearer "+signed)
	r.Header.Set("X-GitHub-Read-Token", "job-read")
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || githubReads != 9 {
		t.Fatalf("strict App compatibility failed: %d reads=%d: %s", w.Code, githubReads, w.Body.String())
	}
}
