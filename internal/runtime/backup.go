package runtime

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Deploy-ryanl-in/personal-paas/internal/store"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Backup struct {
	Directory string
	Recipient string
	Identity  string
	Docker    *Docker
	Store     *store.Store
}
type BackupMetadata struct {
	Release store.Release
	Secrets map[string]string
}

func (b *Backup) Create(ctx context.Context, r store.Release) (string, error) {
	if !strings.HasPrefix(b.Recipient, "age1") {
		return "", errors.New("age recipient unavailable")
	}
	id := fmt.Sprintf("%d-%s", r.Repo.ID, time.Now().UTC().Format("20060102T150405.000000000"))
	dir, err := os.MkdirTemp(b.Directory, ".plaintext-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	paused := []string{}
	defer func() {
		for _, n := range paused {
			b.Docker.Run(context.Background(), "start", n)
		}
	}()
	needsCold := false
	for _, s := range r.Config.Services {
		if len(s.Volumes) > 0 && s.Type != "postgres" && s.Type != "redis" {
			needsCold = true
		}
	}
	if needsCold {
		for n, s := range r.Config.Services {
			if s.Type == "web" || s.Type == "worker" {
				name := ServiceName(r, n)
				if err = b.Docker.Stop(ctx, name); err != nil {
					return "", err
				}
				paused = append(paused, name)
			}
		}
	}
	f, err := os.OpenFile(filepath.Join(dir, "bundle.tar"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	tw := tar.NewWriter(f)
	meta, _ := json.Marshal(BackupMetadata{r, r.Secrets})
	if err = tw.WriteHeader(&tar.Header{Name: "metadata.json", Size: int64(len(meta)), Mode: 0600}); err != nil {
		return "", err
	}
	if _, err = tw.Write(meta); err != nil {
		return "", err
	}
	seen := map[string]bool{}
	for n, s := range r.Config.Services {
		for _, v := range s.Volumes {
			if seen[v.Name] {
				continue
			}
			seen[v.Name] = true
			archive := filepath.Join(dir, v.Name+".data")
			out, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return "", err
			}
			var cmd *exec.Cmd
			switch s.Type {
			case "postgres":
				cmd = b.Docker.Command(ctx, "exec", ServiceName(r, n), "pg_dump", "--format=custom", "--no-owner", "-U", dbUser(s), "-d", dbName(s))
			case "redis":
				if _, err = b.Docker.Run(ctx, "exec", ServiceName(r, n), "redis-cli", "SAVE"); err != nil {
					out.Close()
					return "", err
				}
				cmd = b.Docker.Command(ctx, "exec", ServiceName(r, n), "cat", "/data/dump.rdb")
			default:
				cmd = b.Docker.Command(ctx, "run", "--rm", "--network", "none", "--user", "10001:10001", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--memory", "32m", "--pids-limit", "16", "--mount", "type=volume,src="+Volume(r.Repo.ID, volumeName(r, v.Name))+",dst=/data,readonly", b.Docker.Helper, "tar", "-C", "/data", "-cf", "-", ".")
			}
			cmd.Stdout = out
			cmd.Stderr = io.Discard
			err = cmd.Run()
			out.Close()
			if err != nil {
				return "", errors.New("volume backup failed")
			}
			info, err := os.Stat(archive)
			if err != nil {
				return "", err
			}
			if err = tw.WriteHeader(&tar.Header{Name: v.Name + ".data", Size: info.Size(), Mode: 0600}); err != nil {
				return "", err
			}
			in, err := os.Open(archive)
			if err != nil {
				return "", err
			}
			_, err = io.Copy(tw, in)
			in.Close()
			if err != nil {
				return "", err
			}
		}
	}
	if err = tw.Close(); err != nil {
		return "", err
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	f.Close()
	dest := filepath.Join(b.Directory, id+".tar.age")
	cmd := exec.CommandContext(ctx, "/usr/bin/age", "--encrypt", "--recipient", b.Recipient, "--output", dest, filepath.Join(dir, "bundle.tar"))
	cmd.Stderr = io.Discard
	if err = cmd.Run(); err != nil {
		os.Remove(dest)
		return "", errors.New("age encryption failed")
	}
	os.Chmod(dest, 0600)
	_, err = b.Store.DB.Exec("INSERT INTO backups(id,repo,release_id,created,path) VALUES(?,?,?,?,?)", id, r.Repo.ID, r.ID, time.Now().UTC().Format(time.RFC3339Nano), dest)
	if err != nil {
		return "", err
	}
	b.Prune()
	return id, nil
}
func (b *Backup) Prune() {
	rows, err := b.Store.DB.Query("SELECT id,path FROM backups WHERE created<?", time.Now().Add(-7*24*time.Hour).UTC().Format(time.RFC3339Nano))
	if err != nil {
		return
	}
	paths := map[string]string{}
	for rows.Next() {
		var id, p string
		rows.Scan(&id, &p)
		paths[id] = p
	}
	rows.Close()
	for id, p := range paths {
		if filepath.Dir(p) == b.Directory {
			os.Remove(p)
			b.Store.DB.Exec("DELETE FROM backups WHERE id=?", id)
		}
	}
}
func (b *Backup) Restore(ctx context.Context, e *Engine, current store.Release, id, job string) (err error) {
	return b.restoreTo(ctx, e, current, current, id, job)
}
func (b *Backup) restoreTo(ctx context.Context, e *Engine, current, desired store.Release, id, job string) (err error) {
	var p string
	if err = b.Store.DB.QueryRow("SELECT path FROM backups WHERE id=? AND repo=?", id, current.Repo.ID).Scan(&p); err != nil {
		return errors.New("backup not found")
	}
	dir, err := os.MkdirTemp(b.Directory, ".restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	cmd := exec.CommandContext(ctx, "/usr/bin/age", "--decrypt", "--identity", b.Identity, p)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		return err
	}
	tr := tar.NewReader(io.LimitReader(pipe, 24<<30))
	var meta BackupMetadata
	files := map[string]string{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			cmd.Process.Kill()
			return err
		}
		if h.Typeflag != tar.TypeReg || filepath.Base(h.Name) != h.Name || h.Size > 20<<30 {
			cmd.Process.Kill()
			return errors.New("unsafe backup archive")
		}
		if h.Name == "metadata.json" {
			if h.Size > 1<<20 {
				return errors.New("backup metadata too large")
			}
			if err = json.NewDecoder(io.LimitReader(tr, h.Size)).Decode(&meta); err != nil {
				return err
			}
		} else if strings.HasSuffix(h.Name, ".data") {
			dest := filepath.Join(dir, h.Name)
			f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return err
			}
			_, err = io.CopyN(f, tr, h.Size)
			f.Close()
			if err != nil {
				return err
			}
			files[strings.TrimSuffix(h.Name, ".data")] = dest
		} else {
			return errors.New("unknown archive member")
		}
	}
	if err = cmd.Wait(); err != nil {
		return errors.New("backup decryption failed")
	}
	if meta.Release.Repo.ID != current.Repo.ID {
		return errors.New("backup ownership mismatch")
	}
	target := desired
	target.ID = job
	target.Created = time.Now()
	target.Volumes = map[string]string{}
	freshVolumes := map[string]bool{}
	keepFresh := false
	defer func() {
		if keepFresh {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		for v := range freshVolumes {
			b.Docker.Run(cleanup, "volume", "rm", Volume(target.Repo.ID, v))
		}
	}()
	for k, v := range current.Volumes {
		target.Volumes[k] = v
	}
	for n, s := range target.Config.Services {
		for _, v := range s.Volumes {
			file, ok := files[v.Name]
			if !ok {
				return errors.New("backup missing volume")
			}
			fresh := v.Name + "-" + job[:8]
			target.Volumes[v.Name] = fresh
			freshVolumes[fresh] = true
			uid := "10001"
			if s.Type == "postgres" || s.Type == "redis" {
				uid = "999"
			}
			if err = b.Docker.Volume(ctx, target.Repo.ID, fresh, uid); err != nil {
				return err
			}
			if s.Type == "postgres" {
				continue
			}
			args := []string{"run", "--rm", "-i", "--network", "none", "--user", uid + ":" + uid, "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--memory", "32m", "--pids-limit", "16", "--mount", "type=volume,src=" + Volume(target.Repo.ID, fresh) + ",dst=/data", b.Docker.Helper}
			if s.Type == "redis" {
				args = append(args, "sh", "-c", "cat > /data/dump.rdb")
			} else {
				args = append(args, "tar", "-C", "/data", "-xf", "-", "--no-same-owner")
			}
			c := b.Docker.Command(ctx, args...)
			in, err := os.Open(file)
			if err != nil {
				return err
			}
			c.Stdin = in
			c.Stderr = io.Discard
			err = c.Run()
			in.Close()
			if err != nil {
				return fmt.Errorf("restore of %s volume failed", n)
			}
			if s.Type == "redis" {
				if err := b.prepareRedisAOF(ctx, target, n); err != nil {
					return err
				}
			}
		}
	}
	// The active SQLite release stays unchanged until the new volumes pass validation.
	if err = b.maintenance(ctx, e, current, target, func() error {
		for n, s := range target.Config.Services {
			if s.Type != "postgres" {
				continue
			}
			if err := b.Docker.Create(ctx, target, n); err != nil {
				return err
			}
			if err := e.waitReady(ctx, target, n, false); err != nil {
				return err
			}
			in, err := os.Open(files[s.Volumes[0].Name])
			if err != nil {
				return err
			}
			c := b.Docker.Command(ctx, "exec", "-i", ServiceName(target, n), "pg_restore", "--clean", "--if-exists", "--no-owner", "-U", dbUser(s), "-d", dbName(s))
			c.Stdin = in
			c.Stderr = io.Discard
			err = c.Run()
			in.Close()
			if err != nil {
				return errors.New("PostgreSQL restore validation failed")
			}
		}
		return nil
	}); err != nil {
		return err
	}
	keepFresh = true
	return nil
}

// Redis prefers AOF when enabled. Convert the restored RDB before the final
// append-only server starts, so an empty new AOF cannot hide the restored data.
func (b *Backup) prepareRedisAOF(ctx context.Context, r store.Release, n string) error {
	s := r.Config.Services[n]
	script := `redis-server --port 0 --unixsocket /tmp/redis.sock --dir /data --save "" --appendonly no --daemonize yes
redis-cli -s /tmp/redis.sock CONFIG SET appendonly yes >/dev/null
for i in $(seq 1 120); do
 info=$(redis-cli -s /tmp/redis.sock INFO persistence | tr -d '\r')
 if printf '%s\n' "$info" | grep -q '^aof_rewrite_in_progress:0$' && printf '%s\n' "$info" | grep -q '^aof_last_bgrewrite_status:ok$'; then
  redis-cli -s /tmp/redis.sock SHUTDOWN NOSAVE
  exit 0
 fi
 sleep 0.25
done
exit 1`
	_, err := b.Docker.Run(ctx, "run", "--rm", "--network", "none", "--user", "999:999", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--memory", "64m", "--pids-limit", "32", "--tmpfs", "/tmp:rw,nosuid,nodev,noexec,size=8388608,mode=1777", "--mount", "type=volume,src="+Volume(r.Repo.ID, volumeName(r, s.Volumes[0].Name))+",dst=/data", s.Image, "sh", "-ec", script)
	if err != nil {
		return errors.New("restored Redis snapshot could not be converted to durable AOF")
	}
	return nil
}
func (b *Backup) maintenance(ctx context.Context, e *Engine, old, target store.Release, prepare func() error) (err error) {
	targetOrder, err := target.Config.Order()
	if err != nil {
		return err
	}
	oldOrder, err := old.Config.Order()
	if err != nil {
		return err
	}
	success := false
	defer func() {
		if success {
			return
		}
		recover, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		for n := range target.Config.Services {
			b.Docker.Remove(recover, ServiceName(target, n))
		}
		for _, n := range oldOrder {
			if b.Docker.Create(recover, old, n) == nil {
				e.waitReady(recover, old, n, false)
			}
		}
		e.Routes(recover, old, old.Repo.ID)
	}()
	if err = e.Routes(ctx, store.Release{}, old.Repo.ID); err != nil {
		return err
	}
	for i := len(oldOrder) - 1; i >= 0; i-- {
		n := oldOrder[i]
		if err = b.Docker.Stop(ctx, ServiceName(old, n)); err != nil {
			return err
		}
		if err = b.Docker.Remove(ctx, ServiceName(old, n)); err != nil {
			return err
		}
	}
	if err = prepare(); err != nil {
		return err
	}
	for _, n := range targetOrder {
		if err = b.Docker.Create(ctx, target, n); err != nil {
			return err
		}
		if err = e.waitReady(ctx, target, n, false); err != nil {
			return err
		}
	}
	if err = e.Routes(ctx, target, target.Repo.ID); err != nil {
		return err
	}
	for n, s := range target.Config.Services {
		if s.Type == "web" {
			if err = e.waitReady(ctx, target, n, true); err != nil {
				return err
			}
		}
	}
	if err = b.Store.Activate(target); err != nil {
		return err
	}
	success = true
	return nil
}
func (e *Engine) Upgrade(ctx context.Context, r store.Release, op store.Operation, job string) error {
	s, ok := r.Config.Services[op.Service]
	if !ok || s.Type != "postgres" && s.Type != "redis" {
		return errors.New("database service required")
	}
	major := func(image string) string {
		before := strings.SplitN(image, "@", 2)[0]
		tag := strings.SplitN(before, ":", 2)
		if len(tag) != 2 {
			return ""
		}
		return strings.SplitN(tag[1], ".", 2)[0]
	}
	if major(s.Image) == "" || major(s.Image) != major(op.Image) {
		return errors.New("cross-major upgrade prohibited")
	}
	paused := []string{}
	defer func() {
		for _, n := range paused {
			e.Docker.Run(context.Background(), "start", n)
		}
	}()
	for n, v := range r.Config.Services {
		if v.Type == "web" || v.Type == "worker" {
			name := ServiceName(r, n)
			if err := e.Docker.Stop(ctx, name); err != nil {
				return err
			}
			paused = append(paused, name)
		}
	}
	backupID, err := e.Backup.Create(ctx, r)
	if err != nil {
		return err
	}
	if err := e.Docker.Pull(ctx, op.Image, "", "", false); err != nil {
		return err
	}
	target := r
	target.ID = job
	target.Created = time.Now()
	target.Config.Services = cloneServices(r.Config.Services)
	s.Image = op.Image
	target.Config.Services[op.Service] = s
	return e.Backup.restoreTo(ctx, e, r, target, backupID, job)
}
func cloneServices[T any](m map[string]T) map[string]T {
	out := map[string]T{}
	for k, v := range m {
		out[k] = v
	}
	return out
}
func (b *Backup) Download(ctx context.Context, id string, repo int64) (*os.File, error) {
	var p string
	if err := b.Store.DB.QueryRow("SELECT path FROM backups WHERE id=? AND repo=?", id, repo).Scan(&p); err != nil {
		return nil, err
	}
	if filepath.Dir(p) != b.Directory {
		return nil, errors.New("invalid backup path")
	}
	return os.Open(p)
}
func (b *Backup) Daily(ctx context.Context) {
	zone, _ := time.LoadLocation("Asia/Taipei")
	last := ""
	for {
		now := time.Now().In(zone)
		day := now.Format("2006-01-02")
		if now.Hour() == 2 && day != last {
			all, err := b.Store.ActiveAll()
			if err == nil {
				for _, r := range all {
					_, err = b.Store.Enqueue(r, "operation", store.Operation{Action: "backup", BackupID: day})
					if err != nil {
						break
					}
				}
				if err == nil {
					last = day
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Minute):
		}
	}
}

var _ = bytes.MinRead
