package runtime

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"github.com/Deploy-ryanl-in/personal-paas/internal/manifest"
	"github.com/Deploy-ryanl-in/personal-paas/internal/store"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Run with the controller paused and its queue idle. Uses an isolated fixture repository
// and a separate encrypted state database; cleanup never touches application resources.
func TestDatabaseWorkerBackupRestoreIntegration(t *testing.T) {
	socket := os.Getenv("PAAS_TEST_DOCKER_SOCKET")
	if socket == "" {
		t.Skip("rootless integration is opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	dir := t.TempDir()
	d := &Docker{Socket: socket, Private: filepath.Join(dir, "tmp"), RegistryConfig: filepath.Join(dir, "docker"), Helper: "alpine:3.23@sha256:85fe1e81d6758c208f3e1eed4338a1997e19d4be002d4dd32d3100c9a8c010a0", HTTP: NoRedirectHTTP()}
	os.MkdirAll(d.RegistryConfig, 0700)
	os.MkdirAll(d.Private, 0700)
	repo := int64(900000002)
	list, err := d.List(ctx, repo)
	if err != nil || len(list) != 0 {
		t.Fatal("fixture resources already exist; refusing cleanup", err)
	}
	pg := "postgres:18.3-bookworm@sha256:80630f83606d8db77d30b3851b16a9f78be2d0d4dda6f7b82a1fdca5ebe3acba"
	redis := "redis:8.2.2-bookworm@sha256:4521b581dbddea6e7d81f8fe95ede93f5648aaa66a9dacd581611bf6fe7527bd"
	workerImage := "paas-backup-worker-test:local"
	cmd := d.Command(ctx, "build", "-t", workerImage, "-")
	cmd.Stdin = strings.NewReader("FROM " + d.Helper + "\nRUN apk add --no-cache redis\nENTRYPOINT [\"sh\",\"-c\",\"trap 'exit 0' TERM INT; redis-cli -h redis SET worker:ready yes >/dev/null; while true; do sleep 1; done\"]\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("worker fixture: %s %v", out, err)
	}
	r := store.Release{ID: strings.Repeat("a", 64), Repo: manifest.Identity{ID: repo, FullName: "Deploy-ryanl-in/backup-integration"}, Commit: "fixture", Run: 1, Created: time.Now(), Secrets: map[string]string{"PASSWORD": "isolated-test-password"}, Images: map[string]string{"worker": workerImage}, Config: manifest.Manifest{SchemaVersion: 1, State: "present", Name: "backup-integration", DeployBranch: "main", Services: map[string]manifest.Service{
		"postgres": {Type: "postgres", Image: pg, Resources: manifest.Resources{MemoryMiB: 128, CPU: 0.2, Pids: 64}, Environment: map[string]string{"POSTGRES_USER": "app", "POSTGRES_DB": "app"}, SecretRefs: map[string]string{"POSTGRES_PASSWORD": "PASSWORD"}, Volumes: []manifest.Volume{{Name: "pg-data", Target: "/var/lib/postgresql"}}},
		"redis":    {Type: "redis", Image: redis, Resources: manifest.Resources{MemoryMiB: 64, CPU: 0.1, Pids: 32}, Volumes: []manifest.Volume{{Name: "redis-data", Target: "/data"}}},
		"worker":   {Type: "worker", Build: &manifest.Build{Context: ".", Dockerfile: "Dockerfile"}, Resources: manifest.Resources{MemoryMiB: 32, CPU: 0.1, Pids: 16}, DependsOn: []string{"postgres", "redis"}, Volumes: []manifest.Volume{{Name: "worker-data", Target: "/data-proof"}}},
	}}}
	st, err := store.Open(filepath.Join(dir, "state.db"), bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer st.DB.Close()
	proxy := &Proxy{Directory: filepath.Join(dir, "routes"), APIURL: "http://127.0.0.1:9080", Docker: d}
	engine := &Engine{Store: st, Docker: d, Proxy: proxy, Budget: 768, ReadyTimeout: 60 * time.Second, Observation: time.Second}
	if err := engine.Admission(ctx, r, store.Release{}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		containers, _ := d.List(context.Background(), repo)
		for _, c := range containers {
			d.Remove(context.Background(), c.ID)
		}
		volumes, _ := d.Run(context.Background(), "volume", "ls", "-q", "--filter", "label="+Managed, "--filter", "label=in.ryanl.paas.repository=900000002")
		for _, v := range strings.Fields(string(volumes)) {
			d.Run(context.Background(), "volume", "rm", v)
		}
		d.Run(context.Background(), "network", "rm", Network(repo))
		d.Run(context.Background(), "image", "rm", workerImage)
	}()
	for _, image := range []string{pg, redis, d.Helper} {
		if err := d.Pull(ctx, image, "", "", false); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.EnsureNetwork(ctx, repo); err != nil {
		t.Fatal(err)
	}
	order, err := r.Config.Order()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range order {
		if err := d.Create(ctx, r, n); err != nil {
			t.Fatal(n, err)
		}
		if err := engine.waitReady(ctx, r, n, false); err != nil {
			t.Fatal(n, err)
		}
	}
	if _, err := st.Enqueue(r, "deploy", store.Operation{}); err != nil {
		t.Fatal(err)
	}
	if err := st.Activate(r); err != nil {
		t.Fatal(err)
	}
	run := func(n string, args ...string) string {
		t.Helper()
		out, err := d.Run(ctx, append([]string{"exec", ServiceName(r, n)}, args...)...)
		if err != nil {
			t.Fatal(n, err)
		}
		return strings.TrimSpace(string(out))
	}
	run("postgres", "psql", "-U", "app", "-d", "app", "-v", "ON_ERROR_STOP=1", "-c", "CREATE TABLE proof(value text); INSERT INTO proof VALUES('before');")
	run("redis", "redis-cli", "SET", "proof", "before")
	run("worker", "sh", "-c", "printf before > /data-proof/proof")
	for i := 0; i < 20 && run("redis", "redis-cli", "GET", "worker:ready") != "yes"; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if run("redis", "redis-cli", "GET", "worker:ready") != "yes" {
		t.Fatal("worker could not reach its Redis dependency")
	}
	identity := filepath.Join(dir, "backup.agekey")
	keygen := exec.CommandContext(ctx, "/usr/bin/age-keygen", "-o", identity)
	if out, err := keygen.CombinedOutput(); err != nil {
		t.Fatalf("key generation: %s %v", out, err)
	}
	recipient, err := exec.CommandContext(ctx, "/usr/bin/age-keygen", "-y", identity).Output()
	if err != nil {
		t.Fatal(err)
	}
	backups := filepath.Join(dir, "backups")
	os.MkdirAll(backups, 0700)
	b := &Backup{Directory: backups, Recipient: strings.TrimSpace(string(recipient)), Identity: identity, Docker: d, Store: st}
	engine.Backup = b
	id, err := b.Create(ctx, r)
	if err != nil {
		t.Fatal("backup", err)
	}
	encrypted, err := os.ReadFile(filepath.Join(backups, id+".tar.age"))
	if err != nil || !bytes.HasPrefix(encrypted, []byte("age-encryption.org/v1")) || bytes.Contains(encrypted, []byte(r.Secrets["PASSWORD"])) {
		t.Fatal("backup is not encrypted", err)
	}
	run("postgres", "psql", "-U", "app", "-d", "app", "-c", "UPDATE proof SET value='after';")
	run("redis", "redis-cli", "SET", "proof", "after")
	run("worker", "sh", "-c", "printf after > /data-proof/proof")
	nonce := make([]byte, 32)
	rand.Read(nonce)
	job := hex.EncodeToString(nonce)
	if err := b.Restore(ctx, engine, r, id, job); err != nil {
		t.Fatal("restore", err)
	}
	restored, err := st.Active(repo)
	if err != nil {
		t.Fatal(err)
	}
	r = restored
	for _, check := range []struct{ name, value string }{
		{"PostgreSQL", run("postgres", "psql", "-U", "app", "-d", "app", "-Atc", "SELECT value FROM proof")},
		{"Redis", run("redis", "redis-cli", "GET", "proof")},
		{"cold volume", run("worker", "cat", "/data-proof/proof")},
	} {
		if check.value != "before" {
			t.Fatalf("%s restored data differs: %q", check.name, check.value)
		}
	}
	if restored.ID != job || restored.Volumes["pg-data"] == "" || restored.Volumes["redis-data"] == "" {
		t.Fatal("fresh volume binding not persisted")
	}
	if _, err := d.Run(ctx, "volume", "inspect", Volume(repo, "pg-data")); err != nil {
		t.Fatal("old data volume was overwritten")
	}
	if err := engine.Upgrade(ctx, r, store.Operation{Service: "postgres", Image: "postgres:19.0@sha256:" + strings.Repeat("0", 64)}, strings.Repeat("c", 64)); err == nil {
		t.Fatal("cross-major upgrade admitted")
	}
	if err := engine.Upgrade(ctx, r, store.Operation{Service: "postgres", Image: pg}, strings.Repeat("d", 64)); err != nil {
		t.Fatal("same-major maintenance", err)
	}
	r, err = st.Active(repo)
	if err != nil {
		t.Fatal(err)
	}
	if run("postgres", "psql", "-U", "app", "-d", "app", "-Atc", "SELECT value FROM proof") != "before" {
		t.Fatal("maintenance lost database data")
	}
	if err := engine.Stop(ctx, r); err != nil {
		t.Fatal(err)
	}
	volumes, err := d.Run(ctx, "volume", "ls", "-q", "--filter", "label=in.ryanl.paas.repository=900000002")
	if err != nil || len(strings.Fields(string(volumes))) < 3 {
		t.Fatal("stop lost persistent data", err)
	}
}
