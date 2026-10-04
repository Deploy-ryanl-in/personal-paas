package runtime

import (
	"github.com/Deploy-ryanl-in/personal-paas/internal/manifest"
	"github.com/Deploy-ryanl-in/personal-paas/internal/store"
	"testing"
)

func TestDockerLimitsAreVerified(t *testing.T) {
	s := manifest.Service{Resources: manifest.Resources{MemoryMiB: 128, CPU: 0.5, Pids: 64}}
	c := Container{}
	c.Config.User = "10001:10001"
	c.HostConfig.Memory = 128 * 1024 * 1024
	c.HostConfig.NanoCpus = 500000000
	c.HostConfig.PidsLimit = 64
	c.HostConfig.ReadonlyRootfs = true
	c.HostConfig.CapDrop = []string{"ALL"}
	c.HostConfig.SecurityOpt = []string{"no-new-privileges"}
	d := Docker{}
	if e := d.Verify(c, s); e != nil {
		t.Fatal(e)
	}
	c.HostConfig.Memory = 0
	if e := d.Verify(c, s); e == nil {
		t.Fatal("unenforced memory accepted")
	}
	c.HostConfig.Memory = 128 * 1024 * 1024
	c.HostConfig.SecurityOpt = nil
	if e := d.Verify(c, s); e == nil {
		t.Fatal("privilege escalation accepted")
	}
}

func TestUndeclaredMountAndNetworkEscapeRejected(t *testing.T) {
	d := Docker{}
	r := store.Release{Repo: manifest.Identity{ID: 42}, Config: manifest.Manifest{Services: map[string]manifest.Service{"web": {Type: "web"}}}}
	c := Container{}
	c.HostConfig.NetworkMode = Network(42)
	if err := d.VerifyLayout(c, r, "web"); err != nil {
		t.Fatal(err)
	}
	c.Mounts = append(c.Mounts, struct{ Type, Name, Destination string }{Type: "bind", Destination: "/data"})
	if err := d.VerifyLayout(c, r, "web"); err == nil {
		t.Fatal("host bind mount accepted")
	}
	c.Mounts = nil
	c.HostConfig.NetworkMode = "host"
	if err := d.VerifyLayout(c, r, "web"); err == nil {
		t.Fatal("host network accepted")
	}
}
