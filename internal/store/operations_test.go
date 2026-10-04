package store

import (
	"bytes"
	"github.com/Deploy-ryanl-in/personal-paas/internal/manifest"
	"path/filepath"
	"testing"
	"time"
)

func TestManualOperationsAreIdempotentPerWorkflowRun(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.db"), bytes.Repeat([]byte{8}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	r := Release{Repo: manifest.Identity{ID: 43}, Commit: "unchanged", Run: 10}
	first, err := s.Enqueue(r, "operation", Operation{Action: "backup"})
	if err != nil {
		t.Fatal(err)
	}
	same, err := s.Enqueue(r, "operation", Operation{Action: "backup"})
	if err != nil || first.ID != same.ID {
		t.Fatal("same workflow request duplicated", err)
	}
	r.Run = 11
	next, err := s.Enqueue(r, "operation", Operation{Action: "backup"})
	if err != nil || next.ID == first.ID {
		t.Fatal("new manual workflow reused an old operation", err)
	}
	r.ID = "last-successful-release"
	r.Created = time.Now()
	r.Volumes = map[string]string{"data": "restored-volume"}
	if err := s.Activate(r); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(r.Repo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Active(r.Repo.ID); err == nil {
		t.Fatal("stopped app still active")
	}
	saved, err := s.LatestRelease(r.Repo.ID)
	if err != nil || saved.ID != r.ID || saved.Volumes["data"] != "restored-volume" {
		t.Fatal("stop lost the redeploy version or volume binding", err)
	}
}

func TestIdenticalRunningDeploymentDoesNotSupersedeItself(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.db"), bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	r := Release{Repo: manifest.Identity{ID: 44}, Commit: "first", Run: 10}
	j, err := s.Enqueue(r, "deploy", Operation{})
	if err != nil {
		t.Fatal(err)
	}
	running, err := s.Next()
	if err != nil {
		t.Fatal(err)
	}
	r.Run = 11
	same, err := s.Enqueue(r, "deploy", Operation{})
	if err != nil || same.ID != j.ID {
		t.Fatal("same running deployment not reused", err)
	}
	if s.IsStale(running.Release) {
		t.Fatal("identical candidate superseded itself")
	}
	r.Commit = "second"
	r.Run = 12
	if _, err := s.Enqueue(r, "deploy", Operation{}); err != nil {
		t.Fatal(err)
	}
	if !s.IsStale(running.Release) {
		t.Fatal("older different candidate could overwrite newer version")
	}
}

func TestOldPushCannotUndoManualRuntimeOperation(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.db"), bytes.Repeat([]byte{2}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	r := Release{Repo: manifest.Identity{ID: 45}, Commit: "version", Run: 10}
	if _, err := s.Enqueue(r, "deploy", Operation{}); err != nil {
		t.Fatal(err)
	}
	r.Run = 20
	if _, err := s.Enqueue(r, "operation", Operation{Action: "stop"}); err != nil {
		t.Fatal(err)
	}
	r.Run = 15
	r.Commit = "older-in-flight-push"
	if _, err := s.Enqueue(r, "deploy", Operation{}); err == nil {
		t.Fatal("older push could undo a newer manual operation")
	}
}
