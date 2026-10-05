package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Deploy-ryanl-in/personal-paas/internal/auth"
	"github.com/Deploy-ryanl-in/personal-paas/internal/github"
	"github.com/Deploy-ryanl-in/personal-paas/internal/manifest"
	"github.com/Deploy-ryanl-in/personal-paas/internal/runtime"
	"github.com/Deploy-ryanl-in/personal-paas/internal/store"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	Verifier *auth.Verifier
	GitHub   *github.Client
	Store    *store.Store
	Engine   *runtime.Engine
}
type identityKey struct{}

func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	m.Handle("/v1/", s.authorize(http.HandlerFunc(s.api)))
	return m
}
func (s *Server) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			fail(w, 401, errors.New("OIDC bearer token required"))
			return
		}
		c, e := s.Verifier.Verify(r.Context(), strings.TrimPrefix(h, "Bearer "))
		if e != nil {
			fail(w, 401, e)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		idv, e := strconv.ParseInt(c.RepositoryID, 10, 64)
		if e != nil {
			fail(w, 403, e)
			return
		}
		id, e := s.GitHub.Identity(ctx, c.Repository, idv)
		if e != nil || fmt.Sprint(id.OwnerID) != c.OwnerID {
			fail(w, 403, errors.New("repository ownership verification failed"))
			return
		}
		m, e := s.GitHub.Manifest(ctx, id, id.DefaultBranch)
		if e != nil || c.Ref != "refs/heads/"+m.DeployBranch {
			fail(w, 403, errors.New("branch not authorized by default-branch manifest"))
			return
		}
		if e = s.GitHub.CheckRun(ctx, id, c.RunID, c.SHA, c.Ref, c.Event); e != nil {
			fail(w, 403, e)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, struct {
			Claims   auth.Claims
			Identity manifest.Identity
		}{c, id})))
	})
}
func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	v := r.Context().Value(identityKey{}).(struct {
		Claims   auth.Claims
		Identity manifest.Identity
	})
	id := v.Identity
	c := v.Claims
	path := strings.TrimPrefix(r.URL.Path, "/v1/")
	switch {
	case r.Method == "POST" && path == "deployments":
		if !strings.Contains(c.WorkflowRef, "/.github/workflows/release.yml@") {
			fail(w, 403, errors.New("release workflow required"))
			return
		}
		var req struct {
			Images  map[string]string `json:"images"`
			Secrets map[string]string `json:"secrets"`
		}
		if e := decode(w, r, &req); e != nil {
			fail(w, 400, e)
			return
		}
		m, e := s.GitHub.Manifest(r.Context(), id, c.SHA)
		if e != nil {
			fail(w, 400, e)
			return
		}
		if c.Ref != "refs/heads/"+m.DeployBranch {
			fail(w, 403, errors.New("commit manifest branch differs"))
			return
		}
		domains, e := s.Store.Domains(id.ID)
		if e != nil {
			fail(w, 500, e)
			return
		}
		m, e = manifest.Resolve(m, id, domains, s.Verifier.Policy.BaseDomain(), s.Verifier.Policy.ControlDomain())
		if e == nil {
			e = s.Store.CheckDomains(id.ID, m)
		}
		if e != nil {
			fail(w, 409, e)
			return
		}
		secrets := map[string]string{}
		count := 0
		for n, svc := range m.Services {
			if m.State == "absent" {
				continue
			}
			if svc.Type == "web" || svc.Type == "worker" {
				count++
				if e = manifest.ValidateImage(id, n, req.Images[n]); e != nil {
					fail(w, 403, e)
					return
				}
			} else if s.Verifier.Policy.DatabaseImages[svc.Image] != svc.Type {
				fail(w, 403, errors.New("database image not approved by operator"))
				return
			}
			for _, ref := range svc.SecretRefs {
				value, ok := req.Secrets[ref]
				if !ok || len(value) > 16384 || strings.ContainsAny(value, "\x00\r\n") {
					fail(w, 400, errors.New("missing/invalid selected secret"))
					return
				}
				secrets[ref] = value
			}
		}
		if len(req.Images) != count || len(req.Secrets) != len(secrets) {
			fail(w, 400, errors.New("undeclared image or secret"))
			return
		}
		if e = s.Store.Nonce(c.JTI, c.Exp); e != nil {
			fail(w, 409, errors.New("OIDC token already used"))
			return
		}
		run, _ := strconv.ParseInt(c.RunID, 10, 64)
		j, e := s.Store.Enqueue(store.Release{Repo: id, Commit: c.SHA, Run: run, Config: m, Images: req.Images, Secrets: secrets}, "deploy", store.Operation{})
		if e != nil {
			fail(w, 409, e)
			return
		}
		reply(w, 202, j)
	case r.Method == "POST" && path == "operations":
		if c.Event != "workflow_dispatch" || !strings.Contains(c.WorkflowRef, "/.github/workflows/operate.yml@") {
			fail(w, 403, errors.New("manual operation workflow required"))
			return
		}
		var op store.Operation
		if e := decode(w, r, &op); e != nil {
			fail(w, 400, e)
			return
		}
		switch op.Action {
		case "redeploy", "rollback", "stop", "backup", "restore", "database-upgrade":
		default:
			fail(w, 400, errors.New("unsupported operation"))
			return
		}
		if op.Action == "database-upgrade" && s.Verifier.Policy.DatabaseImages[op.Image] == "" {
			fail(w, 403, errors.New("database image not approved"))
			return
		}
		if e := s.Store.Nonce(c.JTI, c.Exp); e != nil {
			fail(w, 409, errors.New("OIDC token already used"))
			return
		}
		active, e := s.Store.Active(id.ID)
		if e != nil && (op.Action == "redeploy" || op.Action == "rollback" || op.Action == "stop") {
			active, e = s.Store.LatestRelease(id.ID)
		}
		if e != nil {
			fail(w, 404, errors.New("application has no usable release"))
			return
		}
		active.Run, _ = strconv.ParseInt(c.RunID, 10, 64)
		j, e := s.Store.Enqueue(active, "operation", op)
		if e != nil {
			fail(w, 409, e)
			return
		}
		reply(w, 202, j)
	case r.Method == "GET" && strings.HasPrefix(path, "deployments/"):
		j, e := s.Store.Job(strings.TrimPrefix(path, "deployments/"), id.ID)
		if e != nil {
			fail(w, 404, errors.New("job not found"))
			return
		}
		reply(w, 200, j)
	case r.Method == "GET" && path == "status":
		active, e := s.Store.Active(id.ID)
		if e != nil {
			reply(w, 200, map[string]any{"state": "inactive", "repositoryId": id.ID})
			return
		}
		reply(w, 200, map[string]any{"state": "active", "release": active})
	case r.Method == "GET" && path == "history":
		h, e := s.Store.History(id.ID)
		if e != nil {
			fail(w, 500, e)
			return
		}
		reply(w, 200, h)
	case r.Method == "GET" && path == "logs":
		active, e := s.Store.Active(id.ID)
		if e != nil {
			fail(w, 404, e)
			return
		}
		logs, e := s.Engine.Docker.Logs(r.Context(), active, r.URL.Query().Get("service"))
		if e != nil {
			fail(w, 400, e)
			return
		}
		reply(w, 200, map[string]string{"logs": logs})
	case r.Method == "GET" && path == "backups":
		rows, e := s.Store.DB.Query("SELECT id,created,release_id FROM backups WHERE repo=? ORDER BY created DESC", id.ID)
		if e != nil {
			fail(w, 500, e)
			return
		}
		defer rows.Close()
		out := []map[string]string{}
		for rows.Next() {
			var b, c, rel string
			rows.Scan(&b, &c, &rel)
			out = append(out, map[string]string{"id": b, "created": c, "releaseId": rel})
		}
		reply(w, 200, out)
	case r.Method == "GET" && strings.HasPrefix(path, "backups/"):
		f, e := s.Engine.Backup.Download(r.Context(), strings.TrimPrefix(path, "backups/"), id.ID)
		if e != nil {
			fail(w, 404, errors.New("backup not found"))
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename=backup.tar.age")
		io.Copy(w, f)
	default:
		fail(w, 404, errors.New("unknown API route"))
	}
}
func decode(w http.ResponseWriter, r *http.Request, out any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	return manifest.DecodeStrict(data, out)
}
func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, e error) {
	reply(w, status, map[string]string{"error": e.Error()})
}
