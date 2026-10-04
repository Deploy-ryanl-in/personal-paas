package runtime

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"github.com/Deploy-ryanl-in/personal-paas/internal/manifest"
	"github.com/Deploy-ryanl-in/personal-paas/internal/store"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Exercise the exact published Next.js production image, with runtime-only test secrets.
func TestNextProductionReadonlyCacheIntegration(t *testing.T) {
	socket, image := os.Getenv("PAAS_TEST_DOCKER_SOCKET"), os.Getenv("PAAS_TEST_NEXT_IMAGE")
	if socket == "" || image == "" {
		t.Skip("set rootless socket and deployed Next image")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dir := t.TempDir()
	d := &Docker{Socket: socket, Private: filepath.Join(dir, "tmp"), RegistryConfig: filepath.Join(dir, "docker"), HTTP: NoRedirectHTTP()}
	os.MkdirAll(d.RegistryConfig, 0700)
	os.MkdirAll(d.Private, 0700)
	repo := int64(900000003)
	list, err := d.List(ctx, repo)
	if err != nil || len(list) > 0 {
		t.Fatal("test resources already exist; refusing cleanup", err)
	}
	raw := make([]byte, 24)
	rand.Read(raw)
	secret := hex.EncodeToString(raw)
	r := store.Release{ID: strings.Repeat("e", 64), Repo: manifest.Identity{ID: repo}, Commit: "production-fixture", Images: map[string]string{"web": image}, Secrets: map[string]string{"REVALIDATE_TOKEN": secret}, Config: manifest.Manifest{Services: map[string]manifest.Service{"web": {Type: "web", Port: 3000, Domain: "cache-test.ryanl.in", Health: &manifest.Health{Path: "/healthz", Status: 200}, Resources: manifest.Resources{MemoryMiB: 256, CPU: 0.5, Pids: 128}, SecretRefs: map[string]string{"REVALIDATE_TOKEN": "REVALIDATE_TOKEN"}}}}}
	st, err := store.Open(filepath.Join(dir, "state.db"), bytes.Repeat([]byte{6}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer st.DB.Close()
	engine := &Engine{Docker: d, Store: st, Budget: 768, ReadyTimeout: 30 * time.Second}
	if err := engine.Admission(ctx, r, store.Release{}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		containers, _ := d.List(context.Background(), repo)
		for _, c := range containers {
			d.Remove(context.Background(), c.ID)
		}
		d.Run(context.Background(), "network", "rm", Network(repo))
	}()
	if err := d.EnsureNetwork(ctx, repo); err != nil {
		t.Fatal(err)
	}
	if err := d.Create(ctx, r, "web"); err != nil {
		t.Fatal(err)
	}
	if err := engine.waitReady(ctx, r, "web", false); err != nil {
		t.Fatal(err)
	}
	port, err := d.Port(ctx, r, "web")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	request := func(method, path, token string) (int, http.Header, []byte) {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, method, base+path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := d.HTTP.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, resp.Header, body
	}
	stamp := func(body []byte) string {
		m := regexp.MustCompile(`data-testid="isr-time">([^<]+)`).FindSubmatch(body)
		if len(m) != 2 {
			t.Fatal("ISR timestamp absent")
		}
		return string(m[1])
	}
	_, _, a := request("GET", "/isr", "")
	_, _, b := request("GET", "/isr", "")
	if stamp(a) != stamp(b) {
		t.Fatal("ISR not cached")
	}
	if code, _, _ := request("POST", "/api/revalidate", "wrong"); code != 401 {
		t.Fatal("unauthorized invalidation accepted")
	}
	if code, _, _ := request("POST", "/api/revalidate", secret); code != 200 {
		t.Fatal("runtime secret invalidation failed", code)
	}
	_, _, c := request("GET", "/isr", "")
	if stamp(a) == stamp(c) {
		t.Fatal("authenticated invalidation did not regenerate ISR")
	}
	time.Sleep(11 * time.Second)
	request("GET", "/isr", "")
	time.Sleep(200 * time.Millisecond)
	_, _, fresh := request("GET", "/isr", "")
	if stamp(c) == stamp(fresh) {
		t.Fatal("timed ISR regeneration failed")
	}
	for i := 0; i < 2; i++ {
		code, headers, body := request("GET", "/_next/image?url=%2Fsample.png&w=64&q=75", "")
		if code != 200 || !strings.HasPrefix(headers.Get("Content-Type"), "image/") || len(body) == 0 {
			t.Fatal("image cache failed", code)
		}
	}
	if _, err := d.Run(ctx, "exec", ServiceName(r, "web"), "sh", "-c", "touch /app/should-fail"); err == nil {
		t.Fatal("production root filesystem writable")
	}
	out, err := d.Run(ctx, "exec", ServiceName(r, "web"), "sh", "-c", "test -d /tmp/paas-next-cache && test -d /tmp/next-image-cache && du -sk /tmp/paas-next-cache /tmp/next-image-cache")
	if err != nil {
		t.Fatal("cache escaped the temporary writable directories", err)
	}
	t.Log("bounded caches:", string(out))
}
