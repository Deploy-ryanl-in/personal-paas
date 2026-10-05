package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Deploy-ryanl-in/personal-paas/internal/store"
	"log/slog"
	"os"
	"path/filepath"
)

type Proxy struct {
	Directory     string
	APIURL        string
	ControlDomain string
	Docker        *Docker
}

func (p *Proxy) Write(ctx context.Context, releases []store.Release) error {
	control := p.ControlDomain
	if control == "" {
		control = "deploy.ryanl.in"
	}
	routers := map[string]any{"control": map[string]any{"rule": "Host(`" + control + "`)", "entryPoints": []string{"websecure"}, "service": "control", "tls": map[string]any{}, "middlewares": []string{"api-limit"}}}
	services := map[string]any{"control": map[string]any{"loadBalancer": map[string]any{"servers": []any{map[string]any{"url": p.APIURL}}}}}
	for _, r := range releases {
		for n, s := range r.Config.Services {
			if s.Type != "web" {
				continue
			}
			port, e := p.Docker.Port(ctx, r, n)
			if e != nil {
				slog.Warn("route unavailable", "repository", r.Repo.ID, "service", n)
				continue
			}
			k := fmt.Sprintf("r%d-%s", r.Repo.ID, n)
			routers[k] = map[string]any{"rule": "Host(`" + s.Domain + "`)", "entryPoints": []string{"websecure"}, "service": k, "tls": map[string]any{}, "middlewares": []string{"headers"}}
			services[k] = map[string]any{"loadBalancer": map[string]any{"servers": []any{map[string]any{"url": fmt.Sprintf("http://127.0.0.1:%d", port)}}, "responseForwarding": map[string]any{"flushInterval": "100ms"}}}
		}
	}
	cfg := map[string]any{"http": map[string]any{"routers": routers, "services": services, "middlewares": map[string]any{"api-limit": map[string]any{"rateLimit": map[string]any{"average": 20, "burst": 40}}, "headers": map[string]any{"headers": map[string]any{"contentTypeNosniff": true, "referrerPolicy": "strict-origin-when-cross-origin"}}}}, "tls": map[string]any{"certificates": []any{map[string]any{"certFile": "/var/lib/paas-acme/current.crt", "keyFile": "/var/lib/paas-acme/current.key"}}, "options": map[string]any{"default": map[string]any{"minVersion": "VersionTLS12", "sniStrict": true}}}}
	b, e := json.MarshalIndent(cfg, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(p.Directory, 0750); e != nil {
		return e
	}
	f, e := os.CreateTemp(p.Directory, ".routes-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0640); e != nil {
		return e
	}
	if _, e = f.Write(b); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = os.Rename(f.Name(), filepath.Join(p.Directory, "routes.yml")); e != nil {
		return e
	}
	dir, e := os.Open(p.Directory)
	if e != nil {
		return e
	}
	defer dir.Close()
	return dir.Sync()
}
