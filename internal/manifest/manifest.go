package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
)

type Manifest struct {
	SchemaVersion int                `json:"schemaVersion"`
	Name          string             `json:"name"`
	State         string             `json:"state"`
	DeployBranch  string             `json:"deployBranch"`
	Services      map[string]Service `json:"services"`
}
type Build struct {
	Context    string            `json:"context"`
	Dockerfile string            `json:"dockerfile"`
	PublicArgs map[string]string `json:"publicArgs,omitempty"`
}
type Health struct {
	Path   string `json:"path"`
	Status int    `json:"status"`
}
type Resources struct {
	MemoryMiB int     `json:"memoryMiB"`
	CPU       float64 `json:"cpu"`
	Pids      int     `json:"pids"`
}
type Volume struct {
	Name   string `json:"name"`
	Target string `json:"target"`
}
type Routing struct {
	SharedGroup string   `json:"sharedGroup"`
	Paths       []string `json:"paths,omitempty"`
}
type Service struct {
	Type        string            `json:"type"`
	Build       *Build            `json:"build,omitempty"`
	Image       string            `json:"image,omitempty"`
	Port        int               `json:"port,omitempty"`
	Domain      string            `json:"domain,omitempty"`
	Routing     *Routing          `json:"routing,omitempty"`
	Health      *Health           `json:"health,omitempty"`
	Resources   Resources         `json:"resources"`
	Environment map[string]string `json:"environment,omitempty"`
	SecretRefs  map[string]string `json:"secretRefs,omitempty"`
	Volumes     []Volume          `json:"volumes,omitempty"`
	DependsOn   []string          `json:"dependsOn,omitempty"`
}
type Identity struct {
	ID            int64  `json:"id"`
	OwnerID       int64  `json:"ownerId"`
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	FullName      string `json:"fullName"`
	DefaultBranch string `json:"defaultBranch"`
}

var label = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)
var dns = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var envName = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`)
var mountTarget = regexp.MustCompile(`^/[a-zA-Z0-9_./-]+$`)
var digest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var forbidden = map[string]bool{"ping": true, "proxy": true, "ui": true, "deploy": true, "mail": true, "www": true, "api": true, "admin": true, "paas": true, "traefik": true, "localhost": true}

// Decode rejects duplicate JSON keys before Go's decoder can silently replace them.
func Decode(data []byte) (Manifest, error) {
	var m Manifest
	if len(data) > 65536 {
		return m, errors.New("manifest exceeds 64 KiB")
	}
	err := DecodeStrict(data, &m)
	return m, err
}

// DecodeStrict rejects duplicate keys, unknown fields and trailing JSON.
func DecodeStrict(data []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	if err := unique(d); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing JSON")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	return d.Decode(out)
}
func unique(d *json.Decoder) error {
	t, e := d.Token()
	if e != nil {
		return e
	}
	switch t {
	case json.Delim('{'):
		seen := map[string]bool{}
		for d.More() {
			k, e := d.Token()
			if e != nil {
				return e
			}
			s, ok := k.(string)
			if !ok || seen[s] {
				return fmt.Errorf("duplicate/invalid key %v", k)
			}
			seen[s] = true
			if e = unique(d); e != nil {
				return e
			}
		}
		_, e = d.Token()
		return e
	case json.Delim('['):
		for d.More() {
			if e = unique(d); e != nil {
				return e
			}
		}
		_, e = d.Token()
		return e
	}
	return nil
}
func (m *Manifest) Validate() error {
	if m.SchemaVersion != 1 {
		return errors.New("unsupported schemaVersion")
	}
	if m.Name != "auto" && !label.MatchString(m.Name) {
		return errors.New("invalid app name")
	}
	if m.State != "present" && m.State != "absent" {
		return errors.New("state must be present or absent")
	}
	if m.DeployBranch == "" || len(m.DeployBranch) > 100 || strings.ContainsAny(m.DeployBranch, " \x00\r\n~^:?*[\\") || strings.Contains(m.DeployBranch, "..") {
		return errors.New("invalid deployBranch")
	}
	if len(m.Services) < 1 || len(m.Services) > 8 {
		return errors.New("require 1..8 services")
	}
	for name, s := range m.Services {
		if !label.MatchString(name) {
			return fmt.Errorf("invalid service %s", name)
		}
		if s.Resources.MemoryMiB < 32 || s.Resources.MemoryMiB > 768 || s.Resources.CPU <= 0 || s.Resources.CPU > 1 || s.Resources.Pids < 16 || s.Resources.Pids > 256 {
			return fmt.Errorf("invalid resources for %s", name)
		}
		switch s.Type {
		case "web", "worker":
			if s.Build == nil || s.Image != "" {
				return errors.New("application services require build; image is server-generated")
			}
			for _, p := range []string{s.Build.Context, s.Build.Dockerfile} {
				if p == "" || strings.HasPrefix(p, "/") || path.Clean(p) != p || p == ".." || strings.HasPrefix(p, "../") || strings.ContainsAny(p, "\x00\r\n\\") {
					return errors.New("invalid build path")
				}
			}
			for k, v := range s.Build.PublicArgs {
				if !envName.MatchString(k) || len(v) > 4096 {
					return errors.New("invalid public build arg")
				}
			}
		case "postgres", "redis":
			if s.Build != nil || !strings.Contains(s.Image, "@sha256:") || !digest.MatchString(strings.SplitN(s.Image, "@", 2)[1]) {
				return errors.New("database image must be an approved digest")
			}
			if len(s.Volumes) != 1 {
				return errors.New("database requires exactly one stable volume")
			}
			if s.Type == "postgres" && (s.Volumes[0].Target != "/var/lib/postgresql" || s.SecretRefs["POSTGRES_PASSWORD"] == "") {
				return errors.New("postgres requires password reference and /var/lib/postgresql volume")
			}
			if s.Type == "redis" && s.Volumes[0].Target != "/data" {
				return errors.New("redis volume must target /data")
			}
		default:
			return errors.New("unsupported service type")
		}
		if s.Type == "web" {
			if s.Port < 1024 || s.Port > 65535 || s.Domain == "" || s.Health == nil || s.Health.Status < 200 || s.Health.Status > 299 || !strings.HasPrefix(s.Health.Path, "/") || strings.HasPrefix(s.Health.Path, "//") || strings.ContainsAny(s.Health.Path, "?#\r\n") {
				return errors.New("web requires domain, unprivileged port and health path")
			}
			if s.Routing != nil {
				if !label.MatchString(s.Routing.SharedGroup) || len(s.Routing.Paths) > 16 {
					return errors.New("invalid shared route group or path count")
				}
				seen := map[string]bool{}
				for _, filter := range s.Routing.Paths {
					base := strings.TrimSuffix(filter, "/*")
					if !strings.HasPrefix(base, "/") || strings.HasPrefix(base, "//") || path.Clean(base) != base || strings.ContainsAny(base, "*?#\\`\r\n\x00 %") || seen[filter] {
						return errors.New("route paths require a clean absolute prefix with optional trailing /*")
					}
					seen[filter] = true
				}
			}
		} else if s.Domain != "" || s.Port != 0 || s.Health != nil || s.Routing != nil {
			return errors.New("only web services may expose HTTP")
		}
		for k, v := range s.Environment {
			if !envName.MatchString(k) || len(v) > 4096 || strings.ContainsAny(v, "\x00\r\n") {
				return errors.New("invalid environment")
			}
			if _, ok := s.SecretRefs[k]; ok {
				return errors.New("environment/secret overlap")
			}
		}
		for k, v := range s.SecretRefs {
			if !envName.MatchString(k) || !envName.MatchString(v) {
				return errors.New("invalid secret reference")
			}
		}
		for _, v := range s.Volumes {
			if !label.MatchString(v.Name) || !mountTarget.MatchString(v.Target) || path.Clean(v.Target) != v.Target || v.Target == "/" {
				return errors.New("invalid volume")
			}
			for _, p := range []string{"/proc", "/sys", "/dev", "/run", "/etc", "/usr", "/bin", "/sbin"} {
				if v.Target == p || strings.HasPrefix(v.Target, p+"/") {
					return errors.New("protected mount target")
				}
			}
		}
	}
	_, e := m.Order()
	return e
}
func (m Manifest) Order() ([]string, error) {
	out := []string{}
	seen := map[string]int{}
	var visit func(string) error
	visit = func(n string) error {
		if seen[n] == 1 {
			return errors.New("dependency cycle")
		}
		if seen[n] == 2 {
			return nil
		}
		s, ok := m.Services[n]
		if !ok {
			return errors.New("unknown dependency")
		}
		seen[n] = 1
		for _, dep := range s.DependsOn {
			if err := visit(dep); err != nil {
				return err
			}
		}
		seen[n] = 2
		out = append(out, n)
		return nil
	}
	names := []string{}
	for n := range m.Services {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if e := visit(n); e != nil {
			return nil, e
		}
	}
	return out, nil
}
func AutoName(name string) (string, error) {
	var b strings.Builder
	for _, c := range strings.ToLower(name) {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' {
			b.WriteRune(c)
		} else {
			b.WriteByte('-')
		}
	}
	s := strings.Trim(b.String(), "-")
	if !dns.MatchString(s) || forbidden[s] {
		return "", errors.New("repository name requires an explicit safe domain")
	}
	return s, nil
}

// CI can validate explicit domains without knowing the VPS's immutable auto bindings.
// Resolving auto names and detecting existing domain ownership remain server decisions.
func namespace(options []string) (string, string) {
	domain, control := "ryanl.in", "deploy.ryanl.in"
	if len(options) > 0 {
		domain = options[0]
	}
	if len(options) > 1 {
		control = options[1]
	}
	return domain, control
}
func ValidateDeclaredDomains(m Manifest, options ...string) error {
	domain, control := namespace(options)
	for _, s := range m.Services {
		if s.Type != "web" || s.Domain == "auto" {
			continue
		}
		p := strings.TrimSuffix(s.Domain, "."+domain)
		if s.Domain != p+"."+domain || s.Domain == control || !dns.MatchString(p) || forbidden[p] {
			return errors.New("domain outside allowed namespace or reserved")
		}
	}
	return ValidateRoutes(m.Services)
}
func Resolve(m Manifest, id Identity, existing map[string]string, options ...string) (Manifest, error) {
	domain, control := namespace(options)
	if m.Name == "auto" {
		if existing["__name"] != "" {
			m.Name = existing["__name"]
		} else {
			n, e := AutoName(id.Name)
			if e != nil || !label.MatchString(n) {
				n = fmt.Sprintf("app-%d", id.ID)
			}
			m.Name = n
		}
	}
	for n, s := range m.Services {
		if s.Type != "web" {
			continue
		}
		if s.Domain == "auto" {
			if existing[n] != "" {
				s.Domain = existing[n]
			} else {
				v, e := AutoName(id.Name)
				if e != nil {
					return m, e
				}
				s.Domain = v + "." + domain
			}
		}
		p := strings.TrimSuffix(s.Domain, "."+domain)
		if s.Domain != p+"."+domain || s.Domain == control || !dns.MatchString(p) || forbidden[p] {
			return m, errors.New("domain outside allowed namespace or reserved")
		}
		m.Services[n] = s
	}
	return m, ValidateRoutes(m.Services)
}

// Paths filter deliveries, not proxy rewrites. A bare prefix matches its path
// segment and descendants; /* matches descendants only. Empty filters match all.
func (s Service) MatchPath(p string) int {
	if s.Routing == nil || len(s.Routing.Paths) == 0 {
		return 0
	}
	best := -1
	for _, filter := range s.Routing.Paths {
		base := strings.TrimSuffix(filter, "/*")
		wild := strings.HasSuffix(filter, "/*")
		if (!wild && (p == base || base == "/")) || strings.HasPrefix(p, strings.TrimSuffix(base, "/")+"/") {
			score := len(base) * 2
			if !wild {
				score++
			}
			if score > best {
				best = score
			}
		}
	}
	return best
}
func ValidateRoutes(services map[string]Service) error {
	names := []string{}
	for n, s := range services {
		if s.Type == "web" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for i, n := range names {
		for _, other := range names[i+1:] {
			a, b := services[n], services[other]
			if a.Domain != b.Domain || a.Domain == "auto" {
				continue
			}
			if a.Routing == nil || b.Routing == nil || a.Routing.SharedGroup != b.Routing.SharedGroup {
				return errors.New("shared domain requires matching explicit sharedGroup on every participant")
			}
		}
	}
	return nil
}
func ImagePath(id Identity, service string) string {
	return fmt.Sprintf("ghcr.io/%s/paas-%d-%s", strings.ToLower(id.Owner), id.ID, service)
}
func ValidateImage(id Identity, service, image string) error {
	prefix := ImagePath(id, service) + "@"
	if !strings.HasPrefix(image, prefix) || !digest.MatchString(strings.TrimPrefix(image, prefix)) {
		return errors.New("image ownership/digest mismatch")
	}
	return nil
}
func (m Manifest) Memory() int {
	n := 0
	for _, s := range m.Services {
		n += s.Resources.MemoryMiB
	}
	return n
}
