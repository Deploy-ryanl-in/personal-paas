package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Deploy-ryanl-in/personal-paas/internal/gateway"
	"github.com/Deploy-ryanl-in/personal-paas/internal/manifest"
	"github.com/Deploy-ryanl-in/personal-paas/internal/store"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const Managed = "in.ryanl.paas.managed=true"

type Docker struct {
	Socket         string
	Private        string
	RegistryConfig string
	Helper         string
	HTTP           *http.Client
	Gateway        *gateway.Gateway
}
type Container struct {
	ID     string `json:"Id"`
	Name   string
	Config struct {
		Labels map[string]string
		Image  string
		User   string
	}
	Mounts []struct{ Type, Name, Destination string }
	State  struct {
		Running bool
		Status  string
	}
	NetworkSettings struct {
		Ports map[string][]struct{ HostIP, HostPort string }
	}
	HostConfig struct {
		Memory         int64
		Privileged     bool
		NetworkMode    string
		NanoCpus       int64
		PidsLimit      int
		ReadonlyRootfs bool
		CapDrop        []string
		SecurityOpt    []string
		PortBindings   map[string][]struct{ HostIP, HostPort string }
	}
}

func (d *Docker) Command(ctx context.Context, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, "docker", append([]string{"--host", d.Socket, "--config", d.RegistryConfig}, args...)...)
	return c
}
func (d *Docker) Run(ctx context.Context, args ...string) ([]byte, error) {
	c := d.Command(ctx, args...)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	b, e := c.Output()
	if e != nil {
		return nil, fmt.Errorf("Docker %s failed", args[0])
	}
	return b, nil
}
func Name(repo int64, service, release string) string {
	if release != "stable" && len(release) > 12 {
		release = release[:12]
	}
	return fmt.Sprintf("paas-%d-%s-%s", repo, service, release)
}
func Network(repo int64) string             { return fmt.Sprintf("paas-%d", repo) }
func Volume(repo int64, name string) string { return fmt.Sprintf("paas-%d-%s", repo, name) }
func (d *Docker) Inspect(ctx context.Context, name string) (Container, error) {
	var list []Container
	b, e := d.Run(ctx, "container", "inspect", name)
	if e != nil {
		return Container{}, e
	}
	e = json.Unmarshal(b, &list)
	if e != nil || len(list) != 1 {
		return Container{}, errors.New("invalid Docker inspect")
	}
	return list[0], nil
}
func (d *Docker) List(ctx context.Context, repo int64) ([]Container, error) {
	args := []string{"ps", "-aq", "--filter", "label=" + Managed}
	if repo > 0 {
		args = append(args, "--filter", fmt.Sprintf("label=in.ryanl.paas.repository=%d", repo))
	}
	b, e := d.Run(ctx, args...)
	if e != nil {
		return nil, e
	}
	out := []Container{}
	for _, id := range strings.Fields(string(b)) {
		c, e := d.Inspect(ctx, id)
		if e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, nil
}
func (d *Docker) AssertRootless(ctx context.Context) error {
	b, e := d.Run(ctx, "info", "--format", "{{json .}}")
	if e != nil {
		return e
	}
	var info struct {
		SecurityOptions []string
		MemoryLimit     bool
		PidsLimit       bool
		CpuCfsQuota     bool
		CgroupVersion   string
	}
	if e = json.Unmarshal(b, &info); e != nil {
		return e
	}
	rootless := false
	for _, s := range info.SecurityOptions {
		if strings.Contains(s, "rootless") {
			rootless = true
		}
	}
	if !rootless || !info.MemoryLimit || !info.PidsLimit || !info.CpuCfsQuota || info.CgroupVersion != "2" {
		return errors.New("require rootless Docker with enforced cgroup v2 memory/CPU/PID limits")
	}
	return nil
}
func (d *Docker) EnsureNetwork(ctx context.Context, repo int64) error {
	if _, e := d.Run(ctx, "network", "inspect", Network(repo)); e == nil {
		return nil
	}
	_, e := d.Run(ctx, "network", "create", "--label", Managed, "--label", fmt.Sprintf("in.ryanl.paas.repository=%d", repo), Network(repo))
	return e
}
func (d *Docker) Volume(ctx context.Context, repo int64, name, uid string) error {
	v := Volume(repo, name)
	if _, e := d.Run(ctx, "volume", "inspect", v); e == nil {
		return nil
	}
	if _, e := d.Run(ctx, "volume", "create", "--label", Managed, "--label", fmt.Sprintf("in.ryanl.paas.repository=%d", repo), v); e != nil {
		return e
	}
	_, e := d.Run(ctx, "run", "--rm", "--network", "none", "--cap-drop", "ALL", "--cap-add", "CHOWN", "--security-opt", "no-new-privileges", "--memory", "32m", "--pids-limit", "16", "--mount", "type=volume,src="+v+",dst=/data", d.Helper, "chown", uid+":"+uid, "/data")
	return e
}
func ServiceName(r store.Release, n string) string {
	s := r.Config.Services[n]
	revision := r.ID
	if s.Type == "postgres" || s.Type == "redis" {
		revision = "stable"
	}
	return Name(r.Repo.ID, n, revision)
}
func (d *Docker) Pull(ctx context.Context, image, repo, sha string, application bool) error {
	if _, e := d.Run(ctx, "pull", image); e != nil {
		return e
	}
	if !application {
		return nil
	}
	b, e := d.Run(ctx, "image", "inspect", image, "--format", "{{json .Config.Labels}}")
	if e != nil {
		return e
	}
	var labels map[string]string
	if e = json.Unmarshal(b, &labels); e != nil {
		return e
	}
	if labels["org.opencontainers.image.source"] != "https://github.com/"+repo || labels["org.opencontainers.image.revision"] != sha {
		return errors.New("OCI source/revision mismatch")
	}
	return nil
}
func (d *Docker) Create(ctx context.Context, r store.Release, n string) error {
	s := r.Config.Services[n]
	name := ServiceName(r, n)
	if c, e := d.Inspect(ctx, name); e == nil {
		if (s.Type == "postgres" || s.Type == "redis") && !d.DatabaseMatches(c, r, n) {
			return errors.New("stable database image or volume differs from the recorded release")
		}
		if c.Config.Labels["in.ryanl.paas.release"] != r.ID && s.Type != "postgres" && s.Type != "redis" {
			return errors.New("container ownership mismatch")
		}
		if err := d.VerifyLayout(c, r, n); err != nil {
			return err
		}
		if err := d.Verify(c, s); err != nil {
			return err
		}
		if !c.State.Running {
			_, e = d.Run(ctx, "start", name)
			return e
		}
		return nil
	}
	uid := "10001"
	if s.Type == "postgres" {
		uid = "999"
	}
	if s.Type == "redis" {
		uid = "999"
	}
	for _, v := range s.Volumes {
		if e := d.Volume(ctx, r.Repo.ID, volumeName(r, v.Name), uid); e != nil {
			return e
		}
	}
	if e := os.MkdirAll(d.Private, 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(d.Private, "env-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	env := map[string]string{}
	for k, v := range s.Environment {
		env[k] = v
	}
	for k, ref := range s.SecretRefs {
		v, ok := r.Secrets[ref]
		if !ok {
			return fmt.Errorf("missing secret reference %s", ref)
		}
		if strings.ContainsAny(v, "\r\n\x00") {
			return errors.New("secret must be a single line")
		}
		env[k] = v
	}
	if s.Type == "web" {
		env["PORT"] = strconv.Itoa(s.Port)
		env["HOSTNAME"] = "0.0.0.0"
	}
	keys := []string{}
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(f, "%s=%s\n", k, env[k])
	}
	if e = f.Close(); e != nil {
		return e
	}
	args := []string{"create", "--name", name, "--label", Managed, "--label", fmt.Sprintf("in.ryanl.paas.repository=%d", r.Repo.ID), "--label", "in.ryanl.paas.service=" + n, "--label", "in.ryanl.paas.release=" + r.ID, "--label", "in.ryanl.paas.commit=" + r.Commit, "--network", Network(r.Repo.ID), "--network-alias", n, "--user", uid + ":" + uid, "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--memory", fmt.Sprintf("%dm", s.Resources.MemoryMiB), "--memory-swap", fmt.Sprintf("%dm", s.Resources.MemoryMiB), "--cpus", strconv.FormatFloat(s.Resources.CPU, 'f', 3, 64), "--pids-limit", strconv.Itoa(s.Resources.Pids), "--restart", "unless-stopped", "--log-driver", "json-file", "--log-opt", "max-size=5m", "--log-opt", "max-file=3", "--env-file", f.Name(), "--tmpfs", "/tmp:rw,nosuid,nodev,noexec,size=33554432,mode=1777", "--tmpfs", "/run:rw,nosuid,nodev,noexec,size=8388608,mode=1777"}
	if s.Type == "web" {
		args = append(args, "--publish", fmt.Sprintf("127.0.0.1::%d/tcp", s.Port))
	}
	for _, v := range s.Volumes {
		args = append(args, "--mount", "type=volume,src="+Volume(r.Repo.ID, volumeName(r, v.Name))+",dst="+v.Target)
	}
	image := s.Image
	if image == "" {
		image = r.Images[n]
	}
	args = append(args, image)
	if s.Type == "redis" {
		// Credentials stay in the encrypted release/env file and container tmpfs;
		// never place a password in host process arguments or Redis logs.
		args = append(args, "sh", "-eu", "-c", `umask 077
printf '' > /tmp/redis-auth.conf
if [ -n "${REDIS_PASSWORD:-}" ]; then
 escaped=$(printf '%s' "$REDIS_PASSWORD" | sed 's/\\/\\\\/g; s/"/\\"/g')
 printf 'requirepass "%s"\n' "$escaped" > /tmp/redis-auth.conf
fi
exec redis-server /tmp/redis-auth.conf --appendonly yes --appendfsync everysec --save 60 1 --maxmemory "$1" --maxmemory-policy noeviction`, "paas-redis", fmt.Sprintf("%dmb", s.Resources.MemoryMiB/2))
	}
	if _, e = d.Run(ctx, args...); e != nil {
		return e
	}
	c, e := d.Inspect(ctx, name)
	if e != nil {
		return e
	}
	if e = d.VerifyLayout(c, r, n); e != nil {
		return e
	}
	if e = d.Verify(c, s); e != nil {
		return e
	}
	_, e = d.Run(ctx, "start", name)
	return e
}
func (d *Docker) DatabaseMatches(c Container, r store.Release, n string) bool {
	s := r.Config.Services[n]
	if c.Config.Image != s.Image || len(c.Mounts) != len(s.Volumes) {
		return false
	}
	for _, v := range s.Volumes {
		found := false
		for _, mount := range c.Mounts {
			if mount.Type == "volume" && mount.Name == Volume(r.Repo.ID, volumeName(r, v.Name)) && mount.Destination == v.Target {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func volumeName(r store.Release, n string) string {
	if r.Volumes[n] != "" {
		return r.Volumes[n]
	}
	return n
}
func (d *Docker) Verify(c Container, s manifest.Service) error {
	h := c.HostConfig
	uid := "10001:10001"
	if s.Type == "postgres" || s.Type == "redis" {
		uid = "999:999"
	}
	if c.Config.User != uid || h.Privileged {
		return errors.New("container user or privilege boundary violated")
	}
	if h.Memory != int64(s.Resources.MemoryMiB)*1024*1024 || h.NanoCpus != int64(s.Resources.CPU*1e9) || h.PidsLimit != s.Resources.Pids || !h.ReadonlyRootfs || len(h.CapDrop) != 1 || h.CapDrop[0] != "ALL" {
		return errors.New("container security/resource limits not applied")
	}
	safe := false
	for _, v := range h.SecurityOpt {
		if strings.HasPrefix(v, "no-new-privileges") {
			safe = true
		}
	}
	if !safe {
		return errors.New("container may gain privileges")
	}
	for _, ports := range h.PortBindings {
		for _, p := range ports {
			if p.HostIP != "127.0.0.1" {
				return errors.New("container publicly exposes a port")
			}
		}
	}
	return nil
}

// Reject image-declared anonymous volumes, bind mounts and network escapes.
func (d *Docker) VerifyLayout(c Container, r store.Release, n string) error {
	if c.HostConfig.NetworkMode != Network(r.Repo.ID) {
		return errors.New("container network isolation differs")
	}
	expected := map[string]string{}
	for _, v := range r.Config.Services[n].Volumes {
		expected[v.Target] = Volume(r.Repo.ID, volumeName(r, v.Name))
	}
	for _, mount := range c.Mounts {
		if mount.Type == "tmpfs" && (mount.Destination == "/tmp" || mount.Destination == "/run") {
			continue
		}
		name, ok := expected[mount.Destination]
		if mount.Type != "volume" || !ok || name != mount.Name {
			return errors.New("undeclared or host mount prohibited")
		}
		delete(expected, mount.Destination)
	}
	if len(expected) > 0 {
		return errors.New("declared volume missing")
	}
	return nil
}
func (d *Docker) Port(ctx context.Context, r store.Release, n string) (int, error) {
	c, e := d.Inspect(ctx, ServiceName(r, n))
	if e != nil {
		return 0, e
	}
	p := c.NetworkSettings.Ports[fmt.Sprintf("%d/tcp", r.Config.Services[n].Port)]
	if !c.State.Running || len(p) != 1 || p[0].HostIP != "127.0.0.1" {
		return 0, errors.New("missing loopback binding")
	}
	return strconv.Atoi(p[0].HostPort)
}
func (d *Docker) Ready(ctx context.Context, r store.Release, n string, external bool) error {
	s := r.Config.Services[n]
	c, e := d.Inspect(ctx, ServiceName(r, n))
	if e != nil || !c.State.Running {
		return errors.New("container not running")
	}
	switch s.Type {
	case "web":
		u := "https://" + s.Domain + s.Health.Path
		if !external {
			p, e := d.Port(ctx, r, n)
			if e != nil {
				return e
			}
			u = fmt.Sprintf("http://127.0.0.1:%d%s", p, s.Health.Path)
		}
		req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
		if external && d.Gateway != nil {
			d.Gateway.Sign(req, r.Repo.ID, n)
		}
		resp, e := d.HTTP.Do(req)
		if e != nil {
			return errors.New("HTTP health check failed")
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if resp.StatusCode != s.Health.Status {
			return fmt.Errorf("health returned HTTP %d", resp.StatusCode)
		}
	case "postgres":
		_, e = d.Run(ctx, "exec", ServiceName(r, n), "pg_isready", "-h", "127.0.0.1", "-U", dbUser(s), "-d", dbName(s))
		return e
	case "redis":
		b, e := d.Redis(ctx, r, n, "PING")
		if e != nil || strings.TrimSpace(string(b)) != "PONG" {
			return errors.New("Redis not ready")
		}
	}
	return nil
}
func (d *Docker) Redis(ctx context.Context, r store.Release, n string, args ...string) ([]byte, error) {
	command := []string{"exec", ServiceName(r, n), "sh", "-eu", "-c", `export REDISCLI_AUTH="${REDIS_PASSWORD:-}"; exec redis-cli "$@"`, "paas-redis-cli"}
	return d.Run(ctx, append(command, args...)...)
}
func dbUser(s manifest.Service) string {
	if s.Environment["POSTGRES_USER"] != "" {
		return s.Environment["POSTGRES_USER"]
	}
	return "postgres"
}
func dbName(s manifest.Service) string {
	if s.Environment["POSTGRES_DB"] != "" {
		return s.Environment["POSTGRES_DB"]
	}
	return dbUser(s)
}
func (d *Docker) Stop(ctx context.Context, name string) error {
	_, e := d.Run(ctx, "stop", "--time", "30", name)
	return e
}
func (d *Docker) Remove(ctx context.Context, name string) error {
	_, e := d.Run(ctx, "rm", "--force", name)
	return e
}
func (d *Docker) Logs(ctx context.Context, r store.Release, n string) (string, error) {
	if _, ok := r.Config.Services[n]; !ok {
		return "", errors.New("unknown service")
	}
	b, e := d.Command(ctx, "logs", "--tail", "200", ServiceName(r, n)).CombinedOutput()
	if e != nil {
		return "", e
	}
	s := string(b)
	for _, secret := range r.Secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[REDACTED]")
		}
	}
	if len(s) > 128*1024 {
		s = s[len(s)-128*1024:]
	}
	return s, nil
}
func NoRedirectHTTP() *http.Client {
	return &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(r *http.Request, v []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext, TLSHandshakeTimeout: 3 * time.Second}}
}
func (d *Docker) EnsureRegistryDir() error {
	return os.MkdirAll(filepath.Clean(d.RegistryConfig), 0700)
}
