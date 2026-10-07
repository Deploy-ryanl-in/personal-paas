package runtime

import (
	"sort"

	"github.com/Deploy-ryanl-in/personal-paas/internal/manifest"
)

// Include the previous binding even when a commit renames or removes a web
// service, and the proposed binding before a new shared participant starts.
func (e *Engine) CacheDomains(repo int64, proposed manifest.Manifest) ([]string, error) {
	previous, err := e.Store.Domains(repo)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for service, domain := range previous {
		if service != "__name" {
			names[domain] = true
		}
	}
	if release, err := e.Store.LatestRelease(repo); err == nil {
		for _, s := range release.Config.Services {
			if s.Type == "web" {
				names[s.Domain] = true
			}
		}
	}
	for _, s := range proposed.Services {
		if s.Type == "web" {
			names[s.Domain] = true
		}
	}
	out := []string{}
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}
func (e *Engine) ClearRouteCache(repo int64, proposed manifest.Manifest) ([]string, int, error) {
	domains, err := e.CacheDomains(repo, proposed)
	if err != nil {
		return nil, 0, err
	}
	if e.Proxy == nil || e.Proxy.Gateway == nil {
		return domains, 0, nil
	}
	return domains, e.Proxy.Gateway.ClearDomains(domains), nil
}
