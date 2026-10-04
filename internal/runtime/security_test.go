package runtime

import (
	"github.com/Deploy-ryanl-in/personal-paas/internal/manifest"
	"testing"
)

func TestDockerLimitsAreVerified(t *testing.T) {
	s := manifest.Service{Resources: manifest.Resources{MemoryMiB: 128, CPU: 0.5, Pids: 64}}
	c := Container{}
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
