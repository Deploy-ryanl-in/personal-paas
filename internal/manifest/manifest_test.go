package manifest

import (
	"strings"
	"testing"
)

const valid = `{"schemaVersion":1,"name":"auto","state":"present","deployBranch":"main","services":{"web":{"type":"web","build":{"context":".","dockerfile":"Dockerfile"},"port":8080,"domain":"auto","health":{"path":"/healthz","status":200},"resources":{"memoryMiB":128,"cpu":0.5,"pids":64}}}}`

func TestUntrustedInputs(t *testing.T) {
	for _, s := range []string{strings.Replace(valid, `"schemaVersion":1`, `"schemaVersion":1,"schemaVersion":1`, 1), strings.Replace(valid, `"schemaVersion":1`, `"schemaVersion":99`, 1), strings.Replace(valid, `"type":"web"`, `"type":"web","privileged":true`, 1), strings.Replace(valid, `"Dockerfile"`, `"../Dockerfile"`, 1), strings.Replace(valid, `"memoryMiB":128`, `"memoryMiB":0`, 1)} {
		m, e := Decode([]byte(s))
		if e == nil {
			e = m.Validate()
		}
		if e == nil {
			t.Fatalf("accepted attack %s", s)
		}
	}
}
func TestDomains(t *testing.T) {
	m, e := Decode([]byte(valid))
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Validate(); e != nil {
		t.Fatal(e)
	}
	id := Identity{ID: 42, Name: "New_Project", Owner: "Example"}
	m, e = Resolve(m, id, nil)
	if e != nil || m.Services["web"].Domain != "new-project.ryanl.in" {
		t.Fatal(m, e)
	}
	for _, d := range []string{"ui.proxy.ryanl.in", "ping.ryanl.in", "evil.com", "a.ryanl.in.evil.com", "ryanl.in"} {
		s := m.Services["web"]
		s.Domain = d
		m.Services["web"] = s
		if _, e = Resolve(m, id, nil); e == nil {
			t.Fatal("accepted", d)
		}
	}
}
func TestIdentityAndRename(t *testing.T) {
	m, _ := Decode([]byte(valid))
	r, e := Resolve(m, Identity{ID: 42, Name: "renamed"}, map[string]string{"web": "original.ryanl.in"})
	if e != nil || r.Services["web"].Domain != "original.ryanl.in" {
		t.Fatal(r, e)
	}
	if e = ValidateImage(Identity{ID: 42, Owner: "Example"}, "web", "ghcr.io/example/paas-43-web@sha256:"+strings.Repeat("a", 64)); e == nil {
		t.Fatal("accepted other repository image")
	}
}
func TestCycle(t *testing.T) {
	m, _ := Decode([]byte(valid))
	s := m.Services["web"]
	s.DependsOn = []string{"web"}
	m.Services["web"] = s
	if e := m.Validate(); e == nil {
		t.Fatal("accepted cycle")
	}
}

func TestHostMountAndCommandInterfacesDenied(t *testing.T) {
	for _, attack := range []string{`"command":"rm -rf /"`, `"mounts":["/:/host"]`, `"network":"host"`, `"user":0`, `"capabilities":["SYS_ADMIN"]`} {
		s := strings.Replace(valid, `"type":"web"`, `"type":"web",`+attack, 1)
		if _, e := Decode([]byte(s)); e == nil {
			t.Fatal("accepted forbidden interface", attack)
		}
	}
	m, _ := Decode([]byte(valid))
	s := m.Services["web"]
	s.Volumes = []Volume{{Name: "data", Target: "/proc"}}
	m.Services["web"] = s
	if e := m.Validate(); e == nil {
		t.Fatal("accepted protected mount")
	}
	s.Volumes = []Volume{{Name: "data", Target: "/data,src=/var/lib/paas-runtime,type=bind,dst=/secrets"}}
	m.Services["web"] = s
	if e := m.Validate(); e == nil {
		t.Fatal("accepted Docker CSV mount injection")
	}
}

func TestAutoBindingsSurviveRenameAndExplicitReservedRepositoryName(t *testing.T) {
	m, _ := Decode([]byte(valid))
	resolved, err := Resolve(m, Identity{ID: 42, Name: "www"}, map[string]string{"__name": "original", "web": "original.ryanl.in"})
	if err != nil || resolved.Name != "original" || resolved.Services["web"].Domain != "original.ryanl.in" {
		t.Fatal("rename lost its original binding", err)
	}
	if err := ValidateDeclaredDomains(m); err != nil {
		t.Fatal("CI must leave existing auto bindings to the server", err)
	}
	m, _ = Decode([]byte(valid))
	s := m.Services["web"]
	s.Domain = "explicit-project.ryanl.in"
	m.Services["web"] = s
	if _, err := Resolve(m, Identity{ID: 42, Name: "www"}, nil); err != nil {
		t.Fatal("explicit safe domain unusable for a reserved repository name", err)
	}
	s.Domain = "www.ryanl.in"
	m.Services["web"] = s
	if err := ValidateDeclaredDomains(m); err == nil {
		t.Fatal("reserved explicit domain accepted")
	}
	m, _ = Decode([]byte(valid))
	if _, err := Resolve(m, Identity{ID: 42, Name: "www"}, nil); err == nil {
		t.Fatal("new automatic domain used infrastructure namespace")
	}
}
