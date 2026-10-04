package store

import (
	"bytes"
	"github.com/Deploy-ryanl-in/personal-paas/internal/manifest"
	"path/filepath"
	"testing"
	"time"
)

func TestDomainOwnershipTransactional(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db"), bytes.Repeat([]byte{9}, 32))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	r := Release{ID: "one", Repo: manifest.Identity{ID: 1}, Created: time.Now(), Config: manifest.Manifest{Services: map[string]manifest.Service{"web": {Type: "web", Domain: "reserved-app.ryanl.in"}}}}
	s.DB.Exec("INSERT INTO apps(repo) VALUES(1)")
	if e = s.Activate(r); e != nil {
		t.Fatal(e)
	}
	if e = s.CheckDomains(2, r.Config); e == nil {
		t.Fatal("allowed domain takeover")
	}
	r2 := r
	r2.ID = "two"
	r2.Repo.ID = 2
	s.DB.Exec("INSERT INTO apps(repo) VALUES(2)")
	if e = s.Activate(r2); e == nil {
		t.Fatal("allowed concurrent unique domain takeover")
	}
	active, e := s.Active(1)
	if e != nil || active.ID != "one" {
		t.Fatal("lost original app", e)
	}
	if _, e = s.Release("two", 2); e == nil {
		t.Fatal("failed transaction left a release")
	}
}
