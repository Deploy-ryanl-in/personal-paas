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
