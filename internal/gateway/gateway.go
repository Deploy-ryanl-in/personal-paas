// Package gateway implements bounded HTTP and WebSocket copies for explicitly
// shared domains. Traefik continues to own TLS and the public ingress.
package gateway

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strconv"
	"strings"
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
type snapshot struct{ Domains map[string][]Route }
type Gateway struct {
	routes    atomic.Pointer[snapshot]
	key       []byte
	slots     chan struct{}
	transport *http.Transport
}

func New(key []byte) *Gateway {
	g := &Gateway{key: append([]byte{}, key...), slots: make(chan struct{}, 32), transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ResponseHeaderTimeout: 10 * time.Second, MaxIdleConns: 64, MaxIdleConnsPerHost: 8, IdleConnTimeout: 60 * time.Second}}
	g.Update(nil)
	return g
}
func (g *Gateway) Update(routes []Route) {
	s := &snapshot{Domains: map[string][]Route{}}
	for _, r := range routes {
		s.Domains[r.Service.Domain] = append(s.Domains[r.Service.Domain], r)
	}
	for _, rs := range s.Domains {
		sort.Slice(rs, func(i, j int) bool {
			if rs[i].RepositoryID == rs[j].RepositoryID {
				return rs[i].ServiceName < rs[j].ServiceName
			}
			return rs[i].RepositoryID < rs[j].RepositoryID
		})
	}
	g.routes.Store(s)
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
func (g *Gateway) selectRoutes(req *http.Request) []Route {
	host := strings.ToLower(req.Host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	routes := g.routes.Load().Domains[host]
	probe := req.Header.Get("X-Paas-Probe")
	sig := req.Header.Get("X-Paas-Probe-Signature")
	if probe != "" && len(g.key) > 0 && hmac.Equal([]byte(sig), []byte(g.signature(req.Host+"\n"+req.URL.Path+"\n"+probe))) {
		fields := strings.Split(probe, ":")
		if len(fields) == 3 {
			minute, err := strconv.ParseInt(fields[2], 10, 64)
			if err == nil && minute >= time.Now().Unix()/60-1 && minute <= time.Now().Unix()/60 {
				for _, route := range routes {
					if fmt.Sprint(route.RepositoryID) == fields[0] && route.ServiceName == fields[1] && req.URL.Path == route.Service.Health.Path {
						return []Route{route}
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
	return matching
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
	routes := g.selectRoutes(req)
	req.Header.Del("X-Paas-Probe")
	req.Header.Del("X-Paas-Probe-Signature")
	if len(routes) == 0 {
		http.NotFound(w, req)
		return
	}
	if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
		g.websocket(w, req, routes)
		return
	}
	if len(routes) == 1 {
		g.proxy(routes[0]).ServeHTTP(w, req)
		return
	}
	// All matching applications receive this request exactly once. Only HTTP 404
	// means "unhandled". Conflicts do not undo application-side writes.
	body, err := io.ReadAll(http.MaxBytesReader(w, req.Body, MaxBody))
	if err != nil {
		http.Error(w, "shared request body exceeds 1 MiB", http.StatusRequestEntityTooLarge)
		return
	}
	type result struct {
		response *http.Response
		err      error
		route    Route
	}
	results := make(chan result, len(routes))
	for _, route := range routes {
		go func(route Route) { resp, err := g.dispatch(req, route, body); results <- result{resp, err, route} }(route)
	}
	responses := []*http.Response{}
	failures := false
	for range routes {
		v := <-results
		if v.err != nil {
			failures = true
			slog.Warn("shared HTTP participant failed", "repository", v.route.RepositoryID, "service", v.route.ServiceName)
			continue
		}
		if v.response.StatusCode == http.StatusNotFound {
			v.response.Body.Close()
			continue
		}
		responses = append(responses, v.response)
	}
	defer func() {
		for _, response := range responses {
			response.Body.Close()
		}
	}()
	w.Header().Set("X-Paas-Recipients", strconv.Itoa(len(routes)))
	if len(responses) > 1 {
		conflict(w)
		return
	}
	// A timed out participant might also handle the path: never silently choose
	// another reply when conflict detection could not finish.
	if failures {
		http.Error(w, "shared participant unavailable", 502)
		return
	}
	if len(responses) == 0 {
		http.NotFound(w, req)
		return
	}
	proxy := g.proxy(routes[0])
	proxy.Transport = responseTransport{responses[0]}
	req.Body = http.NoBody
	proxy.ServeHTTP(w, req)
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
func (g *Gateway) websocket(w http.ResponseWriter, req *http.Request, routes []Route) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	backends := []*websocket.Conn{}
	responses := []*http.Response{}
	defer func() {
		for _, c := range backends {
			c.CloseNow()
		}
		for _, r := range responses {
			if r.Body != nil {
				r.Body.Close()
			}
		}
	}()
	protocols := []string{}
	for _, v := range strings.Split(req.Header.Get("Sec-WebSocket-Protocol"), ",") {
		if v = strings.TrimSpace(v); v != "" {
			protocols = append(protocols, v)
		}
	}
	failed := false
	for _, route := range routes {
		target, _ := url.Parse(route.URL)
		target.Scheme = "ws"
		target.Path = req.URL.Path
		target.RawPath = req.URL.RawPath
		target.RawQuery = req.URL.RawQuery
		headers := req.Header.Clone()
		removeHop(headers)
		for key := range headers {
			if strings.HasPrefix(strings.ToLower(key), "sec-websocket-") {
				headers.Del(key)
			}
		}
		headers.Set("Host", req.Host)
		dialCtx, stop := context.WithTimeout(ctx, 8*time.Second)
		c, resp, err := websocket.Dial(dialCtx, target.String(), &websocket.DialOptions{HTTPClient: &http.Client{Transport: g.transport}, HTTPHeader: headers, Subprotocols: protocols})
		stop()
		if err != nil {
			if resp == nil {
				failed = true
				continue
			}
			if resp.StatusCode == 404 {
				if resp.Body != nil {
					resp.Body.Close()
				}
				continue
			}
			responses = append(responses, resp)
			continue
		}
		c.SetReadLimit(MaxMessage)
		backends = append(backends, c)
	}
	if len(backends)+len(responses) > 1 {
		conflict(w)
		return
	}
	if failed {
		http.Error(w, "shared WebSocket participant unavailable", 502)
		return
	}
	if len(responses) == 1 {
		response := responses[0]
		removeHop(response.Header)
		for k, v := range response.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(response.StatusCode)
		io.Copy(w, io.LimitReader(response.Body, MaxBody))
		return
	}
	if len(backends) == 0 {
		http.NotFound(w, req)
		return
	}
	backend := backends[0]
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
