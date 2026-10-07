package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/Deploy-ryanl-in/personal-paas/internal/auth"
	"github.com/Deploy-ryanl-in/personal-paas/internal/gateway"
	gh "github.com/Deploy-ryanl-in/personal-paas/internal/github"
	"github.com/Deploy-ryanl-in/personal-paas/internal/manifest"
	"github.com/Deploy-ryanl-in/personal-paas/internal/runtime"
	"github.com/Deploy-ryanl-in/personal-paas/internal/store"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRouteCacheScopeAndApprovedPushOperation(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"), make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	declaration := `{"schemaVersion":1,"name":"auto","state":"present","deployBranch":"main","services":{"web":{"type":"web","build":{"context":".","dockerfile":"Dockerfile"},"port":8080,"domain":"api.ryanl.in","health":{"path":"/healthz","status":200},"resources":{"memoryMiB":128,"cpu":0.5,"pids":128}}}}`
	m, err := manifest.Decode([]byte(declaration))
	if err != nil {
		t.Fatal(err)
	}
	id := manifest.Identity{ID: 42, Owner: "RyanStanLin", Name: "demo"}
	old := m
	old.Services = map[string]manifest.Service{"web": m.Services["web"]}
	svc := old.Services["web"]
	svc.Domain = "old-api.ryanl.in"
	old.Services["web"] = svc
	if err = db.Activate(store.Release{ID: "old", Repo: id, Config: old, Created: time.Now()}); err != nil {
		t.Fatal(err)
	}
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("owner")) }))
	defer owner.Close()
	other := httptest.NewServer(http.HandlerFunc(http.NotFound))
	defer other.Close()
	g := gateway.New(nil)
	routes := []gateway.Route{}
	domains := []string{"api.ryanl.in", "old-api.ryanl.in", "unrelated.ryanl.in"}
	for _, domain := range domains {
		for i, url := range []string{owner.URL, other.URL} {
			routes = append(routes, gateway.Route{RepositoryID: int64(i + 42), ServiceName: "web", URL: url, Service: manifest.Service{Domain: domain}})
		}
	}
	g.Update(routes)
	for _, domain := range domains {
		w := httptest.NewRecorder()
		g.ServeHTTP(w, httptest.NewRequest("GET", "http://"+domain+"/data", nil))
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	client := &http.Client{Transport: tokenTransport(func(r *http.Request) (*http.Response, error) {
		data, _ := json.Marshal(map[string]any{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(declaration)), "size": len(declaration)})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), Header: make(http.Header)}, nil
	})}
	s := Server{Verifier: &auth.Verifier{Policy: auth.Policy{}}, GitHub: &gh.Client{HTTP: client}, Store: db, Engine: &runtime.Engine{Store: db, Proxy: &runtime.Proxy{Gateway: g}}}
	claims := auth.Claims{SHA: strings.Repeat("a", 40), Event: "push", WorkflowRef: "Deploy-ryanl-in/personal-paas/.github/workflows/operate.yml@" + strings.Repeat("b", 40), JTI: "cache-1", Exp: time.Now().Add(time.Minute).Unix()}
	call := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/v1/route-cache/clear", strings.NewReader(body))
		ctx := context.WithValue(r.Context(), identityKey{}, struct {
			Claims   auth.Claims
			Identity manifest.Identity
		}{claims, id})
		r = r.WithContext(gh.WithReadToken(ctx, "readonly"))
		w := httptest.NewRecorder()
		s.api(w, r)
		claims.JTI += "x"
		return w
	}
	if w := call(`{"domain":"unrelated.ryanl.in"}`); w.Code != 403 {
		t.Fatal("foreign cache clear allowed", w.Code, w.Body.String())
	}
	if w := call(`{"domain":"api.ryanl.in","command":"sh"}`); w.Code != 400 {
		t.Fatal("unknown field accepted", w.Code)
	}
	claims.Event = "pull_request"
	if w := call(`{}`); w.Code != 403 {
		t.Fatal("PR operation accepted", w.Code)
	}
	claims.Event = "push"
	if w := call(`{"domain":"api.ryanl.in"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	counts := g.CacheCounts(domains)
	if counts[domains[0]] != 0 || counts[domains[1]] != 1 || counts[domains[2]] != 1 {
		t.Fatal("scope not respected", counts)
	}
	if w := call(`{}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	counts = g.CacheCounts(domains)
	if counts[domains[1]] != 0 || counts[domains[2]] != 1 {
		t.Fatal("previous domain or unrelated scope wrong", counts)
	}
	// An invalid commit cannot authorize new domains, but still invalidates old ones.
	declaration = `{"schemaVersion":1,"unknown":true}`
	w := httptest.NewRecorder()
	g.ServeHTTP(w, httptest.NewRequest("GET", "http://old-api.ryanl.in/data", nil))
	if w := call(`{}`); w.Code != 200 || !strings.Contains(w.Body.String(), "Invalid declaration") {
		t.Fatal("invalid declaration prevented old-route invalidation", w.Code, w.Body.String())
	}
	counts = g.CacheCounts(domains)
	if counts[domains[1]] != 0 || counts[domains[2]] != 1 {
		t.Fatal("invalid declaration scope wrong", counts)
	}

}
