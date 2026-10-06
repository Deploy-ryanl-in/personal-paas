package store

import (
	"bytes"
	"github.com/Deploy-ryanl-in/personal-paas/internal/manifest"
	"path/filepath"
	"testing"
)

func TestQueueRecoverySecretsAndOrdering(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	key := bytes.Repeat([]byte{7}, 32)
	s, e := Open(p, key)
	if e != nil {
		t.Fatal(e)
	}
	r := Release{Repo: manifest.Identity{ID: 42}, Run: 20, Commit: "abc", Secrets: map[string]string{"PASSWORD": "never-plaintext"}}
	j, e := s.Enqueue(r, "deploy", Operation{})
	if e != nil {
		t.Fatal(e)
	}
	same, e := s.Enqueue(r, "deploy", Operation{})
	if e != nil || same.ID != j.ID {
		t.Fatal("not idempotent", e)
	}
	r.Run = 19
	if _, e = s.Enqueue(r, "deploy", Operation{}); e == nil {
		t.Fatal("accepted stale run")
	}
	if _, e = s.Next(); e != nil {
		t.Fatal(e)
	}
	s.DB.Close()
	s, e = Open(p, key)
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	j, e = s.Next()
	if e != nil || j.Release.Secrets["PASSWORD"] != "never-plaintext" {
		t.Fatal("restart recovery failed", e)
	}
	var raw []byte
	s.DB.QueryRow("SELECT payload FROM jobs LIMIT 1").Scan(&raw)
	if bytes.Contains(raw, []byte("never-plaintext")) {
		t.Fatal("stored plaintext secrets")
	}
	if e = s.Nonce("jti", 9999999999); e != nil {
		t.Fatal(e)
	}
	if e = s.Nonce("jti", 9999999999); e == nil {
		t.Fatal("accepted replay")
	}
}

func TestSuspensionSurvivesRestartAndDomainUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	key := bytes.Repeat([]byte{8}, 32)
	s, e := Open(path, key)
	if e != nil {
		t.Fatal(e)
	}
	r := Release{ID: "one", Repo: manifest.Identity{ID: 7, FullName: "owner/app"}, Config: manifest.Manifest{Services: map[string]manifest.Service{"web": {Type: "web", Domain: "before.ryanl.in"}}}}
	s.DB.Exec("INSERT INTO apps(repo) VALUES(7)")
	if e = s.Activate(r); e != nil {
		t.Fatal(e)
	}
	if e = s.Suspend(7); e != nil {
		t.Fatal(e)
	}
	s.DB.Close()
	s, e = Open(path, key)
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	if _, e = s.Active(7); e == nil {
		t.Fatal("paused app is active")
	}
	if paused, e := s.Suspended(7); e != nil || paused.ID != "one" {
		t.Fatal(e)
	}
	list, e := s.ActiveAll()
	if e != nil || len(list) != 0 {
		t.Fatal("paused route active", e)
	}
	r.ID = "two"
	web := r.Config.Services["web"]
	web.Domain = "after.ryanl.in"
	r.Config.Services["web"] = web
	if e = s.Activate(r); e != nil {
		t.Fatal(e)
	}
	domains, e := s.Domains(7)
	if e != nil || domains["web"] != "after.ryanl.in" {
		t.Fatal(domains, e)
	}
	if _, e = s.Suspended(7); e == nil {
		t.Fatal("activation did not clear pause")
	}
	if e = s.Stop(7); e != nil {
		t.Fatal(e)
	}
	if _, e = s.LatestRelease(7); e != nil {
		t.Fatal("delete lost release", e)
	}
}
func TestSharedDomainRequiresConsentAndAllowsOverlappingFilters(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "state.db"), bytes.Repeat([]byte{9}, 32))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	a := manifest.Service{Type: "web", Domain: "endpoint.ryanl.in", Routing: &manifest.Routing{SharedGroup: "endpoint"}}
	r := Release{ID: "a", Repo: manifest.Identity{ID: 1}, Config: manifest.Manifest{Services: map[string]manifest.Service{"user": a}}}
	s.DB.Exec("INSERT INTO apps(repo) VALUES(1)")
	if e = s.Activate(r); e != nil {
		t.Fatal(e)
	}
	m := manifest.Manifest{Services: map[string]manifest.Service{"app": a}}
	if e = s.CheckDomains(2, m); e != nil {
		t.Fatal("shared domain rejected", e)
	}
	a.Routing = nil
	m.Services["app"] = a
	if e = s.CheckDomains(2, m); e == nil {
		t.Fatal("exclusive route hijack accepted")
	}
}
