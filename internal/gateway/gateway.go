// Package gateway implements bounded HTTP and WebSocket copies for explicitly
// shared domains. Traefik continues to own TLS and the public ingress.
package gateway

import (
	"bytes"
	"container/list"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Deploy-ryanl-in/personal-paas/internal/manifest"
	"github.com/coder/websocket"
)

const MaxBody = 1 << 20
const MaxMessage = 64 << 10

type Route struct {
	RepositoryID int64
	ServiceName  string
	Service      manifest.Service
	URL          string
}
type domainRoutes struct {
	Routes      []Route
	Fingerprint string
	Generation  *cacheGeneration
}
type cacheGeneration struct{ marker byte }
type snapshot struct{ Domains map[string]domainRoutes }
type Gateway struct {
	routes    atomic.Pointer[snapshot]
	key       []byte
	slots     chan struct{}
	watchers  chan struct{}
	transport *http.Transport
	mu        sync.Mutex
	cache     map[cacheKey]*list.Element
	lru       list.List
}

func New(key []byte) *Gateway {
	g := &Gateway{key: append([]byte{}, key...), slots: make(chan struct{}, 32), watchers: make(chan struct{}, 32), cache: map[cacheKey]*list.Element{}, transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ResponseHeaderTimeout: 10 * time.Second, MaxResponseHeaderBytes: 64 << 10, MaxIdleConns: 64, MaxIdleConnsPerHost: 8, IdleConnTimeout: 60 * time.Second}}
	g.Update(nil)
	return g
}
func (g *Gateway) Update(routes []Route) {
	g.mu.Lock()
	defer g.mu.Unlock()
	s := &snapshot{Domains: map[string]domainRoutes{}}
	for _, r := range routes {
		d := s.Domains[r.Service.Domain]
		d.Routes = append(d.Routes, r)
		s.Domains[r.Service.Domain] = d
	}
	old := g.routes.Load()
	for name, domain := range s.Domains {
		rs := domain.Routes
		sort.Slice(rs, func(i, j int) bool {
			if rs[i].RepositoryID == rs[j].RepositoryID {
				return rs[i].ServiceName < rs[j].ServiceName
			}
			return rs[i].RepositoryID < rs[j].RepositoryID
		})
		data, _ := json.Marshal(rs)
		domain.Fingerprint = string(data)
		domain.Generation = &cacheGeneration{}
		if old != nil && old.Domains[name].Fingerprint == domain.Fingerprint {
			domain.Generation = old.Domains[name].Generation
		}
		s.Domains[name] = domain
	}
	g.routes.Store(s)
	g.pruneGenerationsLocked(s)
}
func (g *Gateway) Sign(req *http.Request, repo int64, service string) {
	token := fmt.Sprintf("%d:%s:%d", repo, service, time.Now().Unix()/60)
	req.Header.Set("X-Paas-Probe", token)
	req.Header.Set("X-Paas-Probe-Signature", g.signature(req.Host+"\n"+req.URL.Path+"\n"+token))
}
func (g *Gateway) signature(message string) string {
	h := hmac.New(sha256.New, g.key)
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))
}
func requestDomain(req *http.Request) string {
	host := strings.ToLower(req.Host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return host
}
func (g *Gateway) selectRoutes(req *http.Request) ([]Route, domainRoutes, bool) {
	domain := g.routes.Load().Domains[requestDomain(req)]
	routes := domain.Routes
	probe := req.Header.Get("X-Paas-Probe")
	sig := req.Header.Get("X-Paas-Probe-Signature")
	if probe != "" && len(g.key) > 0 && hmac.Equal([]byte(sig), []byte(g.signature(req.Host+"\n"+req.URL.Path+"\n"+probe))) {
		fields := strings.Split(probe, ":")
		if len(fields) == 3 {
			minute, err := strconv.ParseInt(fields[2], 10, 64)
			if err == nil && minute >= time.Now().Unix()/60-1 && minute <= time.Now().Unix()/60 {
				for _, route := range routes {
					if fmt.Sprint(route.RepositoryID) == fields[0] && route.ServiceName == fields[1] && req.URL.Path == route.Service.Health.Path {
						return []Route{route}, domain, true
					}
				}
			}
		}
	}
	matching := []Route{}
	for _, r := range routes {
		if r.Service.MatchPath(req.URL.Path) >= 0 {
			matching = append(matching, r)
		}
	}
	return matching, domain, false
}
func conflict(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusConflict)
	w.Write([]byte(`{"error":"multi_service_conflict","detail":"多服务冲突"}`))
}
func (g *Gateway) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	select {
	case g.slots <- struct{}{}:
		defer func() { <-g.slots }()
	default:
		http.Error(w, "shared ingress busy", 503)
		return
	}
	routes, domain, probe := g.selectRoutes(req)
	key := routeKey(req, domain)
	if probe {
		key.Generation = nil
	}
	req.Header.Del("X-Paas-Probe")
	req.Header.Del("X-Paas-Probe-Signature")
	if len(routes) == 0 {
		http.NotFound(w, req)
		return
	}
	if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
		g.websocket(w, req, routes, key)
		return
	}
	if len(routes) == 1 {
		g.proxy(routes[0]).ServeHTTP(w, req)
		return
	}
	g.sharedHTTP(w, req, routes, key)
}

type responseTransport struct{ response *http.Response }

func (t responseTransport) RoundTrip(*http.Request) (*http.Response, error) { return t.response, nil }
func (g *Gateway) proxy(route Route) *httputil.ReverseProxy {
	target, _ := url.Parse(route.URL)
	return &httputil.ReverseProxy{Transport: g.transport, FlushInterval: -1, Rewrite: func(pr *httputil.ProxyRequest) {
		pr.SetURL(target)
		pr.Out.Host = pr.In.Host
		pr.SetXForwarded()
		if value := pr.In.Header.Get("X-Forwarded-Proto"); value != "" {
			pr.Out.Header.Set("X-Forwarded-Proto", value)
		}
	}, ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) { http.Error(w, "application unavailable", 502) }}
}
func removeHop(headers http.Header) {
	for _, name := range strings.Split(headers.Get("Connection"), ",") {
		headers.Del(strings.TrimSpace(name))
	}
	for _, name := range []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade"} {
		headers.Del(name)
	}
}
func (g *Gateway) dispatch(req *http.Request, route Route, body []byte) (*http.Response, error) {
	copy := req.Clone(req.Context())
	u := *req.URL
	target, _ := url.Parse(route.URL)
	u.Scheme = target.Scheme
	u.Host = target.Host
	copy.URL = &u
	copy.RequestURI = ""
	copy.Body = io.NopCloser(bytes.NewReader(body))
	copy.ContentLength = int64(len(body))
	removeHop(copy.Header)
	return g.transport.RoundTrip(copy)
}
func (g *Gateway) bridge(w http.ResponseWriter, req *http.Request, backend *websocket.Conn) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer backend.CloseNow()
	accepted := []string{}
	if protocol := backend.Subprotocol(); protocol != "" {
		accepted = append(accepted, protocol)
	}
	client, err := websocket.Accept(w, req, &websocket.AcceptOptions{Subprotocols: accepted})
	if err != nil {
		return
	}
	defer client.CloseNow()
	client.SetReadLimit(MaxMessage)
	done := make(chan error, 2)
	pump := func(dst, src *websocket.Conn) {
		for {
			typ, data, err := src.Read(ctx)
			if err != nil {
				done <- err
				return
			}
			writeCtx, stop := context.WithTimeout(ctx, 10*time.Second)
			err = dst.Write(writeCtx, typ, data)
			stop()
			if err != nil {
				done <- err
				return
			}
		}
	}
	go pump(client, backend)
	go pump(backend, client)
	first := <-done
	cancel()
	<-done
	code := websocket.CloseStatus(first)
	if code < 1000 || code == 1005 || code == 1006 || errors.Is(first, context.Canceled) {
		code = websocket.StatusGoingAway
	}
	client.Close(code, "")
	backend.Close(code, "")
}
