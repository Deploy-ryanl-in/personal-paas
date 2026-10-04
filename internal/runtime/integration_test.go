package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/Deploy-ryanl-in/personal-paas/internal/manifest"
	"github.com/Deploy-ryanl-in/personal-paas/internal/store"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Opt-in: execute the compiled test binary as paas-runtime against its isolated rootless daemon.
// This builds a tiny busybox static HTTP fixture, never application source code.
func TestRootlessRuntimeIntegration(t *testing.T) {
	socket := os.Getenv("PAAS_TEST_DOCKER_SOCKET")
	if socket == "" {
		t.Skip("set PAAS_TEST_DOCKER_SOCKET for isolated runtime integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	dir := t.TempDir()
	d := &Docker{Socket: socket, Private: filepath.Join(dir, "tmp"), RegistryConfig: filepath.Join(dir, "docker"), Helper: "alpine:3.23@sha256:85fe1e81d6758c208f3e1eed4338a1997e19d4be002d4dd32d3100c9a8c010a0", HTTP: NoRedirectHTTP()}
	os.MkdirAll(d.RegistryConfig, 0700)
	if e := d.AssertRootless(ctx); e != nil {
		t.Fatal(e)
	}
	dockerfile := []byte("FROM " + d.Helper + "\nRUN apk add --no-cache busybox-extras && mkdir /www && printf ok >/www/healthz\nUSER 10001:10001\nENTRYPOINT [\"busybox-extras\",\"httpd\",\"-f\",\"-p\",\"8080\",\"-h\",\"/www\"]\n")
	cmd := d.Command(ctx, "build", "--label", "in.ryanl.paas.test=true", "-t", "paas-runtime-test:local", "-")
	cmd.Stdin = bytes.NewReader(dockerfile)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("fixture build: %v %s", e, b)
	}
	defer d.Run(context.Background(), "image", "rm", "paas-runtime-test:local")
	repo := int64(900000001)
	if existing, err := d.List(ctx, repo); err != nil || len(existing) != 0 {
		t.Fatal("test repository resources already exist; refusing cleanup", err)
	}
	r := store.Release{ID: "testcandidate0000000000000000000000000000000000000000000000000000000", Repo: manifest.Identity{ID: repo, FullName: "Deploy-ryanl-in/integration"}, Commit: "fixture", Images: map[string]string{"web": "paas-runtime-test:local"}, Config: manifest.Manifest{SchemaVersion: 1, Name: "integration", State: "present", DeployBranch: "main", Services: map[string]manifest.Service{"web": {Type: "web", Port: 8080, Domain: "integration.ryanl.in", Health: &manifest.Health{Path: "/healthz", Status: 200}, Resources: manifest.Resources{MemoryMiB: 64, CPU: 0.2, Pids: 32}, Build: &manifest.Build{Context: ".", Dockerfile: "Dockerfile"}}}}}
	defer func() {
		list, _ := d.List(context.Background(), repo)
		for _, c := range list {
			d.Remove(context.Background(), c.ID)
		}
		d.Run(context.Background(), "network", "rm", Network(repo))
	}()
	if e := d.EnsureNetwork(ctx, repo); e != nil {
		t.Fatal(e)
	}
	if e := d.Create(ctx, r, "web"); e != nil {
		t.Fatal(e)
	}
	if e := d.Create(ctx, r, "web"); e != nil {
		t.Fatal("idempotent create", e)
	}
	list, e := d.List(ctx, repo)
	if e != nil || len(list) != 1 {
		t.Fatal("duplicate containers", len(list), e)
	}
	c, e := d.Inspect(ctx, ServiceName(r, "web"))
	if e != nil {
		t.Fatal(e)
	}
	if e = d.Verify(c, r.Config.Services["web"]); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 20; i++ {
		if d.Ready(ctx, r, "web", false) == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if e = d.Ready(ctx, r, "web", false); e != nil {
		t.Fatal(e)
	}
	data, e := d.Run(ctx, "exec", ServiceName(r, "web"), "cat", "/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/cpu.max", "/sys/fs/cgroup/pids.max")
	if e != nil || string(data) != "67108864\n20000 100000\n32\n" {
		t.Fatal("actual cgroup limits", string(data), e)
	}
	db, e := store.Open(filepath.Join(dir, "state.db"), bytes.Repeat([]byte{3}, 32))
	if e != nil {
		t.Fatal(e)
	}
	defer db.DB.Close()
	engine := &Engine{Store: db, Docker: d, Budget: 64}
	candidate := r
	candidate.ID = "othercandidate00000000000000000000000000000000000000000000000000000"
	if e = engine.Admission(ctx, candidate, r); e == nil {
		t.Fatal("over-budget candidate admitted")
	}
	proxy := Proxy{Directory: filepath.Join(dir, "routes"), APIURL: "http://127.0.0.1:9080", Docker: d}
	if e = proxy.Write(ctx, []store.Release{r}); e != nil {
		t.Fatal(e)
	}
	routes, e := os.ReadFile(filepath.Join(proxy.Directory, "routes.yml"))
	if e != nil {
		t.Fatal(e)
	}
	var document map[string]any
	if e = json.Unmarshal(routes, &document); e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(routes, []byte("integration.ryanl.in")) {
		t.Fatal("route missing")
	}
	port, e := d.Port(ctx, r, "web")
	if e != nil || port < 1024 {
		t.Fatal("unprivileged host port", e)
	}
}
