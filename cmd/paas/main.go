package main

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Deploy-ryanl-in/personal-paas/internal/auth"
	"github.com/Deploy-ryanl-in/personal-paas/internal/gateway"
	"github.com/Deploy-ryanl-in/personal-paas/internal/github"
	"github.com/Deploy-ryanl-in/personal-paas/internal/manifest"
	"github.com/Deploy-ryanl-in/personal-paas/internal/runtime"
	"github.com/Deploy-ryanl-in/personal-paas/internal/server"
	"github.com/Deploy-ryanl-in/personal-paas/internal/store"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "validate" {
		validate(os.Args[2:])
		return
	}
	state := flag.String("state", "/var/lib/paas-runtime", "state directory")
	policyFile := flag.String("policy", "/etc/personal-paas/policy.json", "trust policy")
	routes := flag.String("routes", "/var/lib/paas-routes", "proxy files")
	flag.Parse()
	b, e := os.ReadFile(*policyFile)
	must(e)
	var policy auth.Policy
	must(json.Unmarshal(b, &policy))
	must(policy.Validate())
	key, e := os.ReadFile(filepath.Join(*state, "state.key"))
	must(e)
	db, e := store.Open(filepath.Join(*state, "state.db"), key)
	must(e)
	defer db.DB.Close()
	var rsaKey *rsa.PrivateKey
	if os.Getenv("GITHUB_APP_ID") != "" {
		appKey, err := os.ReadFile(filepath.Join(*state, "github-app.pem"))
		must(err)
		rsaKey, err = github.LoadKey(appKey)
		must(err)
	}
	d := &runtime.Docker{Socket: os.Getenv("DOCKER_HOST"), Private: filepath.Join(*state, "tmp"), RegistryConfig: filepath.Join(*state, "docker"), Helper: policy.HelperImage, HTTP: runtime.NoRedirectHTTP()}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	must(d.AssertRootless(ctx))
	ingress := gateway.New(key)
	d.Gateway = ingress
	proxy := &runtime.Proxy{Directory: *routes, APIURL: "http://127.0.0.1:9080", Docker: d, ControlDomain: policy.ControlDomain(), Gateway: ingress}
	shared := &http.Server{Addr: "127.0.0.1:9081", Handler: ingress, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 20 << 10}
	listener, err := net.Listen("tcp", shared.Addr)
	must(err)
	go func() {
		if err := shared.Serve(listener); err != http.ErrServerClosed {
			slog.Error("application ingress failed")
			cancel()
		}
	}()
	go func() {
		<-ctx.Done()
		c, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		shared.Shutdown(c)
	}()
	backup := &runtime.Backup{Directory: filepath.Join(*state, "backups"), Recipient: os.Getenv("AGE_RECIPIENT"), Identity: filepath.Join(*state, "backup.agekey"), Docker: d, Store: db}
	must(os.MkdirAll(backup.Directory, 0700))
	engine := &runtime.Engine{Store: db, Docker: d, Proxy: proxy, ReadyTimeout: 120 * time.Second, Observation: 60 * time.Second, Budget: 768, Backup: backup}
	if err := engine.Reconcile(ctx); err != nil {
		slog.Error("startup reconciliation failed; control API remains available", "error", err)
	}
	api := &server.Server{Verifier: &auth.Verifier{Policy: policy, Client: &http.Client{Timeout: 10 * time.Second}}, GitHub: &github.Client{HTTP: &http.Client{Timeout: 15 * time.Second}, AppID: os.Getenv("GITHUB_APP_ID"), Key: rsaKey}, Store: db, Engine: engine}
	go engine.Start(ctx)
	go backup.Daily(ctx)
	srv := &http.Server{Addr: "127.0.0.1:9080", Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: time.Minute, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 20 << 10}
	go func() {
		<-ctx.Done()
		c, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		srv.Shutdown(c)
	}()
	slog.Info("controller ready", "address", srv.Addr)
	if e = srv.ListenAndServe(); e != http.ErrServerClosed {
		must(e)
	}
}
func validate(args []string) {
	f := flag.NewFlagSet("validate", flag.ExitOnError)
	file := f.String("file", "paas.json", "manifest")
	id := f.Int64("repository-id", 0, "immutable ID")
	owner := f.String("owner", "", "owner")
	name := f.String("name", "", "repository name")
	domain := f.String("domain", "ryanl.in", "allowed DNS namespace")
	control := f.String("control-domain", "deploy.ryanl.in", "reserved control hostname")
	f.Parse(args)
	b, e := os.ReadFile(*file)
	must(e)
	m, e := manifest.Decode(b)
	must(e)
	must(m.Validate())
	if *id <= 0 {
		fatal("repository-id required")
	}
	identity := manifest.Identity{ID: *id, Owner: *owner, Name: *name}
	must(manifest.ValidateDeclaredDomains(m, *domain, *control))
	rows := []map[string]any{}
	for n, s := range m.Services {
		if m.State == "present" && s.Build != nil {
			rows = append(rows, map[string]any{"service": n, "context": s.Build.Context, "dockerfile": s.Build.Dockerfile, "image": manifest.ImagePath(identity, n), "publicArgs": s.Build.PublicArgs})
		}
	}
	out := map[string]any{"matrix": map[string]any{"include": rows}, "deployBranch": m.DeployBranch, "schemaVersion": m.SchemaVersion}
	json.NewEncoder(os.Stdout).Encode(out)
}
func must(e error) {
	if e != nil {
		fatal(e.Error())
	}
}
func fatal(msg string) { fmt.Fprintln(os.Stderr, msg); os.Exit(1) }
