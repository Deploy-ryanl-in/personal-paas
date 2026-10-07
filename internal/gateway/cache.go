package gateway

import (
	"net/http"
	"strings"
	"time"
)

const cacheLimit = 4096
const cacheTTL = 30 * time.Minute
const maxCacheKey = 2048

type cacheKey struct {
	Domain                              string
	Generation                          *cacheGeneration
	Method, URI, Protocol, Subprotocols string
}
type cacheEntry struct {
	Key     cacheKey
	Route   Route
	Expires time.Time
}

func routeKey(req *http.Request, domain domainRoutes) cacheKey {
	protocol := "http"
	if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
		protocol = "websocket"
	}
	return cacheKey{Domain: requestDomain(req), Generation: domain.Generation, Method: req.Method, URI: req.URL.RequestURI(), Protocol: protocol, Subprotocols: req.Header.Get("Sec-WebSocket-Protocol")}
}
func (g *Gateway) cached(key cacheKey) (Route, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	e := g.cache[key]
	if e == nil {
		return Route{}, false
	}
	entry := e.Value.(cacheEntry)
	if time.Now().After(entry.Expires) || g.routes.Load().Domains[key.Domain].Generation != key.Generation {
		g.removeLocked(key)
		return Route{}, false
	}
	g.lru.MoveToFront(e)
	return entry.Route, true
}
func (g *Gateway) learn(key cacheKey, route Route) {
	if key.Generation == nil || len(key.Method)+len(key.URI)+len(key.Subprotocols) > maxCacheKey {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	// An in-flight request from before a push or route switch cannot reinsert its result.
	if g.routes.Load().Domains[key.Domain].Generation != key.Generation {
		return
	}
	g.removeLocked(key)
	g.cache[key] = g.lru.PushFront(cacheEntry{key, route, time.Now().Add(cacheTTL)})
	for len(g.cache) > cacheLimit {
		g.removeLocked(g.lru.Back().Value.(cacheEntry).Key)
	}
}
func (g *Gateway) forget(key cacheKey) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.removeLocked(key)
}
func (g *Gateway) removeLocked(key cacheKey) {
	if e := g.cache[key]; e != nil {
		g.lru.Remove(e)
		delete(g.cache, key)
	}
}
func (g *Gateway) pruneGenerationsLocked(s *snapshot) {
	for key := range g.cache {
		if s.Domains[key.Domain].Generation != key.Generation {
			g.removeLocked(key)
		}
	}
}

// ClearDomains replaces only the named generations, even if their live routes
// have not changed. Clearing a shared name invalidates every participating app.
func (g *Gateway) ClearDomains(domains []string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	old := g.routes.Load()
	s := &snapshot{Domains: make(map[string]domainRoutes, len(old.Domains))}
	for name, d := range old.Domains {
		s.Domains[name] = d
	}
	for _, name := range domains {
		if d, ok := s.Domains[name]; ok {
			d.Generation = &cacheGeneration{}
			s.Domains[name] = d
		}
	}
	before := len(g.cache)
	g.routes.Store(s)
	g.pruneGenerationsLocked(s)
	return before - len(g.cache)
}

func (g *Gateway) CacheCounts(domains []string) map[string]int {
	g.mu.Lock()
	defer g.mu.Unlock()
	counts := map[string]int{}
	for _, domain := range domains {
		counts[domain] = 0
	}
	for key, e := range g.cache {
		if time.Now().After(e.Value.(cacheEntry).Expires) {
			g.removeLocked(key)
			continue
		}
		if _, ok := counts[key.Domain]; ok {
			counts[key.Domain]++
		}
	}
	return counts
}
