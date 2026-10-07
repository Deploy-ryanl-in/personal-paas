package runtime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/Deploy-ryanl-in/personal-paas/internal/store"
	"log/slog"
	"maps"
	"time"
)

type Engine struct {
	Store        *store.Store
	Docker       *Docker
	Proxy        *Proxy
	ReadyTimeout time.Duration
	Observation  time.Duration
	Budget       int
	Backup       *Backup
}

func (e *Engine) Start(ctx context.Context) {
	events := make(chan struct{}, 1)
	go e.Docker.Events(ctx, events)
	go certificateChanges(ctx, events)
	for {
		if ctx.Err() != nil {
			return
		}
		j, err := e.Store.Next()
		if err == sql.ErrNoRows {
			select {
			case <-ctx.Done():
				return
			case <-events:
				if err := e.Reconcile(ctx); err != nil {
					slog.Error("runtime reconciliation failed", "error", err)
				}
			case <-time.After(time.Second):
				continue
			}
			continue
		}
		if err != nil {
			slog.Error("queue read failed", "error", err)
			time.Sleep(time.Second)
			continue
		}
		jobCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
		err = e.Execute(jobCtx, j)
		cancel()
		status := "succeeded"
		if err != nil {
			status = "failed"
		}
		if ctx.Err() != nil {
			return
		}
		if err2 := e.Store.Finish(j.ID, status, err); err2 != nil {
			slog.Error("persist job result failed", "error", err2)
		}
		e.Store.Prune()
	}
}
func (e *Engine) Execute(ctx context.Context, j store.Job) error {
	if j.Kind == "operation" && store.OperationChangesRuntime(j.Operation) && e.Store.IsStale(j.Release) {
		return errors.New("operation superseded by a newer workflow")
	}
	if j.Kind == "deploy" {
		if e.Store.IsStale(j.Release) {
			return errors.New("superseded by newer workflow")
		}
		return e.Deploy(ctx, j.Release)
	}
	r, err := e.Store.Active(j.RepoID)
	if err == sql.ErrNoRows && (j.Operation.Action == "redeploy" || j.Operation.Action == "rollback" || j.Operation.Action == "stop" || j.Operation.Action == "start" || j.Operation.Action == "restart" || j.Operation.Action == "delete" || j.Operation.Action == "cleanup-images") {
		r, err = e.Store.LatestRelease(j.RepoID)
	}
	if err != nil {
		return errors.New("no active application")
	}
	r.Run = j.Release.Run
	switch j.Operation.Action {
	case "start":
		return e.Deploy(ctx, r)
	case "restart":
		if err := e.Suspend(ctx, r); err != nil {
			return err
		}
		return e.Deploy(ctx, r)
	case "redeploy":
		r.ID = j.ID
		r.Created = time.Now()
		return e.Deploy(ctx, r)
	case "rollback":
		old, err := e.Store.Release(j.Operation.ReleaseID, j.RepoID)
		if err != nil {
			return errors.New("rollback version not found")
		}
		old.ID = j.ID
		old.Run = j.Release.Run
		old.Created = time.Now()
		old.Volumes = r.Volumes
		return e.Deploy(ctx, old)
	case "stop":
		return e.Suspend(ctx, r)
	case "delete":
		return e.Stop(ctx, r)
	case "cleanup-images":
		return e.Docker.PruneImages(ctx, r, nil)
	case "backup":
		_, err = e.Backup.Create(ctx, r)
		return err
	case "restore":
		if j.Operation.Confirm != "RESTORE "+fmt.Sprint(j.RepoID) {
			return errors.New("restore confirmation required")
		}
		return e.Backup.Restore(ctx, e, r, j.Operation.BackupID, j.ID)
	case "database-upgrade":
		return e.Upgrade(ctx, r, j.Operation, j.ID)
	default:
		return errors.New("unsupported operation")
	}
}
func (e *Engine) Admission(ctx context.Context, r, old store.Release) error {
	list, err := e.Docker.List(ctx, 0)
	if err != nil {
		return err
	}
	used := int64(0)
	running := map[string]int64{}
	for _, c := range list {
		if c.State.Running {
			used += c.HostConfig.Memory
			running[c.Name] = c.HostConfig.Memory
		}
	}
	candidate := 0
	for n, s := range r.Config.Services {
		if s.Type == "postgres" || s.Type == "redis" {
			if _, ok := old.Config.Services[n]; ok {
				continue
			}
		}
		if s.Type == "worker" {
			if _, ok := old.Config.Services[n]; ok {
				used -= running["/"+ServiceName(old, n)]
			}
		}
		if c, err := e.Docker.Inspect(ctx, ServiceName(r, n)); err == nil && c.State.Running {
			continue
		}
		candidate += s.Resources.MemoryMiB
	}
	if used+int64(candidate)*1024*1024 > int64(e.Budget)*1024*1024 {
		return errors.New("768 MiB application budget includes candidates; retain live version")
	}
	if err := HostCapacity(e.Docker.Private, candidate); err != nil {
		return err
	}
	return nil
}
func (e *Engine) Deploy(ctx context.Context, r store.Release) (err error) {
	if _, _, err = e.ClearRouteCache(r.Repo.ID, r.Config); err != nil {
		return err
	}
	old, oldErr := e.Store.Active(r.Repo.ID)
	if oldErr != nil && oldErr != sql.ErrNoRows {
		return oldErr
	}
	if old.ID == r.ID {
		return nil
	}
	// Keep stable data mappings and database restrictions even while paused or deleted.
	if oldErr == sql.ErrNoRows {
		if previous, lookupErr := e.Store.LatestRelease(r.Repo.ID); lookupErr == nil {
			old = previous
		}
	}
	if r.Config.State == "absent" {
		if oldErr == nil {
			return e.Stop(ctx, old)
		}
		return nil
	}
	if err = e.Store.CheckDomains(r.Repo.ID, r.Config); err != nil {
		return err
	}
	if r.Volumes == nil {
		r.Volumes = old.Volumes
	}
	for n, s := range old.Config.Services {
		if s.Type == "postgres" || s.Type == "redis" {
			next, ok := r.Config.Services[n]
			if !ok || next.Type != s.Type || next.Image != s.Image {
				return errors.New("database changes require dedicated operation")
			}
			if next.Resources != s.Resources {
				return errors.New("database resource changes require maintenance")
			}
			if !databaseEnvironmentEqual(old, r, n) {
				return errors.New("database credentials or environment changes require explicit maintenance; retain current data")
			}
		}
	}
	admissionOld := old
	if oldErr == sql.ErrNoRows {
		admissionOld = store.Release{}
	}
	if err = e.Admission(ctx, r, admissionOld); err != nil {
		return err
	}
	if err = e.Docker.EnsureNetwork(ctx, r.Repo.ID); err != nil {
		return err
	}
	switched := false
	success := false
	defer func() {
		if success {
			return
		}
		recoverCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if switched {
			if oldErr == nil {
				e.Routes(recoverCtx, old, r.Repo.ID)
			} else {
				e.Routes(recoverCtx, store.Release{}, r.Repo.ID)
			}
		}
		list, _ := e.Docker.List(recoverCtx, r.Repo.ID)
		for _, c := range list {
			if c.Config.Labels["in.ryanl.paas.release"] == r.ID {
				n := c.Config.Labels["in.ryanl.paas.service"]
				s := r.Config.Services[n]
				if s.Type != "postgres" && s.Type != "redis" {
					e.Docker.Remove(recoverCtx, c.ID)
				}
			}
		}
		for n, s := range old.Config.Services {
			if s.Type == "worker" && oldErr == nil {
				e.Docker.Create(recoverCtx, old, n)
			}
		}
		if cleanupErr := e.Cleanup(recoverCtx, r); cleanupErr != nil {
			slog.Error("failed candidate cleanup incomplete", "repository", r.Repo.ID, "error", cleanupErr)
		}
	}()
	order, _ := r.Config.Order()
	for _, n := range order {
		s := r.Config.Services[n]
		image := s.Image
		if image == "" {
			image = r.Images[n]
		}
		if err = e.Docker.Pull(ctx, image, r.Repo.FullName, r.Commit, s.Type == "web" || s.Type == "worker"); err != nil {
			return err
		}
	}
	readyCtx, readyCancel := context.WithTimeout(ctx, e.ReadyTimeout)
	defer readyCancel()
	for _, n := range order {
		s := r.Config.Services[n]
		if s.Type == "worker" {
			if _, ok := old.Config.Services[n]; ok {
				// Deleted stacks keep release metadata, but have no old worker.
				// List the managed resources rather than stopping a missing name.
				previous, lookupErr := e.Docker.List(ctx, old.Repo.ID)
				if lookupErr != nil {
					return lookupErr
				}
				for _, container := range previous {
					if container.Name == "/"+ServiceName(old, n) && container.State.Running {
						if err = e.Docker.Stop(ctx, container.ID); err != nil {
							return err
						}
					}
				}
			}
		}
		if err = e.Docker.Create(ctx, r, n); err != nil {
			return err
		}
		if err = e.waitReady(readyCtx, r, n, false); err != nil {
			return err
		}
	}
	if r.Run > old.Run && e.Store.IsStale(r) {
		return errors.New("superseded before route switch")
	}
	if err = e.Routes(ctx, r, r.Repo.ID); err != nil {
		return err
	}
	switched = true
	for n, s := range r.Config.Services {
		if s.Type == "web" {
			if err = e.waitReady(ctx, r, n, true); err != nil {
				return err
			}
		}
	}
	deadline := time.Now().Add(e.Observation)
	for time.Now().Before(deadline) {
		for n := range r.Config.Services {
			if err = e.Docker.Ready(ctx, r, n, r.Config.Services[n].Type == "web"); err != nil {
				return errors.New("post-switch health failed; previous version restored")
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	if r.Run > old.Run && e.Store.IsStale(r) {
		return errors.New("superseded before activation")
	}
	if err = e.Store.Activate(r); err != nil {
		return err
	}
	success = true
	for n, s := range old.Config.Services {
		if s.Type != "postgres" && s.Type != "redis" {
			if ServiceName(old, n) == ServiceName(r, n) {
				continue
			}
			e.Docker.Stop(ctx, ServiceName(old, n))
			e.Docker.Remove(ctx, ServiceName(old, n))
		}
	}
	return e.Cleanup(ctx, r)
}
func (e *Engine) waitReady(ctx context.Context, r store.Release, n string, external bool) error {
	deadline := time.Now().Add(e.ReadyTimeout)
	for {
		if err := e.Docker.Ready(ctx, r, n, external); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s not ready within 120 seconds", n)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
func (e *Engine) Routes(ctx context.Context, r store.Release, repo int64) error {
	all, err := e.Store.ActiveAll()
	if err != nil {
		return err
	}
	out := []store.Release{}
	for _, v := range all {
		if v.Repo.ID != repo {
			out = append(out, v)
		}
	}
	if r.ID != "" {
		out = append(out, r)
	}
	return e.Proxy.Write(ctx, out)
}
func (e *Engine) Stop(ctx context.Context, r store.Release) error {
	if err := e.Routes(ctx, store.Release{}, r.Repo.ID); err != nil {
		return err
	}
	if err := e.Store.Stop(r.Repo.ID); err != nil {
		return err
	}
	list, err := e.Docker.List(ctx, r.Repo.ID)
	if err != nil {
		return err
	}
	for _, c := range list {
		if err = e.Docker.Stop(ctx, c.ID); err != nil {
			return err
		}
		if err = e.Docker.Remove(ctx, c.ID); err != nil {
			return err
		}
	}
	return nil
}

func databaseEnvironmentEqual(a, b store.Release, n string) bool {
	resolved := func(r store.Release) map[string]string {
		out := maps.Clone(r.Config.Services[n].Environment)
		if out == nil {
			out = map[string]string{}
		}
		for k, ref := range r.Config.Services[n].SecretRefs {
			out[k] = r.Secrets[ref]
		}
		return out
	}
	return maps.Equal(resolved(a), resolved(b))
}

// A stopped stack is durably suspended so Docker events cannot restart or
// orphan-clean it. Named volumes and container identities remain available.
func (e *Engine) Suspend(ctx context.Context, r store.Release) error {
	if err := e.Routes(ctx, store.Release{}, r.Repo.ID); err != nil {
		return err
	}
	if err := e.Store.Suspend(r.Repo.ID); err != nil {
		return err
	}
	list, err := e.Docker.List(ctx, r.Repo.ID)
	if err != nil {
		return err
	}
	for _, c := range list {
		if c.State.Running {
			if err := e.Docker.Stop(ctx, c.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
func (e *Engine) Cleanup(ctx context.Context, r store.Release) error {
	list, err := e.Docker.List(ctx, r.Repo.ID)
	if err != nil {
		return err
	}
	for _, c := range list {
		if !c.State.Running {
			if err = e.Docker.Remove(ctx, c.ID); err != nil {
				return err
			}
		}
	}
	_, err = e.Store.DB.Exec("DELETE FROM releases WHERE repo=? AND id NOT IN (SELECT id FROM releases WHERE repo=? ORDER BY created DESC LIMIT 3) AND id NOT IN (SELECT active FROM apps)", r.Repo.ID, r.Repo.ID)
	if err != nil {
		return err
	}
	rows, err := e.Store.DB.Query("SELECT id FROM releases WHERE repo=?", r.Repo.ID)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	retained := []store.Release{}
	for _, id := range ids {
		release, err := e.Store.Release(id, r.Repo.ID)
		if err != nil {
			return err
		}
		retained = append(retained, release)
	}
	return e.Docker.PruneImages(ctx, r, retained)
}
func (e *Engine) Reconcile(ctx context.Context) error {
	all, err := e.Store.ActiveAll()
	if err != nil {
		return err
	}
	var failures []error
	for _, r := range all {
		order, orderErr := r.Config.Order()
		if orderErr != nil {
			failures = append(failures, orderErr)
			continue
		}
		if err := e.Docker.EnsureNetwork(ctx, r.Repo.ID); err != nil {
			failures = append(failures, fmt.Errorf("repository %d: %w", r.Repo.ID, err))
			continue
		}
		for _, n := range order {
			s := r.Config.Services[n]
			if s.Type == "postgres" || s.Type == "redis" {
				if c, inspectErr := e.Docker.Inspect(ctx, ServiceName(r, n)); inspectErr == nil && !e.Docker.DatabaseMatches(c, r, n) {
					if err := e.Docker.Remove(ctx, c.ID); err != nil {
						failures = append(failures, fmt.Errorf("repository %d: %w", r.Repo.ID, err))
						break
					}
				}
			}
			if err := e.Docker.Create(ctx, r, n); err != nil {
				failures = append(failures, fmt.Errorf("repository %d service %s: %w", r.Repo.ID, n, err))
				break
			}
		}
	}
	list, err := e.Docker.List(ctx, 0)
	if err != nil {
		failures = append(failures, err)
	} else {
		active := map[string]bool{}
		for _, r := range all {
			for n := range r.Config.Services {
				active[ServiceName(r, n)] = true
			}
		}
		paused, pauseErr := e.Store.SuspendedAll()
		if pauseErr != nil {
			return pauseErr
		}
		pausedNames := map[string]bool{}
		for _, r := range paused {
			for n := range r.Config.Services {
				pausedNames[ServiceName(r, n)] = true
				active[ServiceName(r, n)] = true
			}
		}
		for _, c := range list {
			if pausedNames[c.Name[1:]] && c.State.Running {
				if err := e.Docker.Stop(ctx, c.ID); err != nil {
					failures = append(failures, err)
				}
			}
			if !active[c.Name[1:]] {
				if err := e.Docker.Remove(ctx, c.ID); err != nil {
					failures = append(failures, err)
				}
			}
		}
	}
	// Publish every available application and the control API even if one application cannot recover.
	if err := e.Proxy.Write(ctx, all); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}
