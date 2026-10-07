package gateway

import (
	"context"
	"github.com/coder/websocket"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func waitWatchers(t *testing.T, g *Gateway) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for len(g.watchers) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("observer did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}
func TestCachedHTTPReturnsBeforeSlow404AndObservesLateConflict(t *testing.T) {
	var hits atomic.Int32
	var gate atomic.Pointer[chan struct{}]
	var responds atomic.Bool
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("owner")) }))
	defer owner.Close()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if p := gate.Load(); p != nil {
			<-*p
		}
		if responds.Load() {
			w.Write([]byte("late"))
		} else {
			http.NotFound(w, r)
		}
	}))
	defer other.Close()
	g := New(nil)
	g.Update([]Route{fixture(owner.URL, 1), fixture(other.URL, 2)})
	send := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		g.ServeHTTP(w, httptest.NewRequest("GET", "http://endpoint.ryanl.in/api/user", nil))
		return w
	}
	if w := send(); w.Code != 200 || w.Header().Get("X-Paas-Route-Cache") != "miss" {
		t.Fatal(w.Code, w.Header())
	}
	delayed := make(chan struct{})
	gate.Store(&delayed)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- send() }()
	select {
	case w := <-done:
		if w.Code != 200 || w.Header().Get("X-Paas-Route-Cache") != "hit" {
			t.Fatal(w.Code, w.Header())
		}
	case <-time.After(time.Second):
		close(delayed)
		t.Fatal("cached response waited for the 404")
	}
	close(delayed)
	waitWatchers(t, g)
	if hits.Load() != 2 {
		t.Fatal("cache suppressed a request copy", hits.Load())
	}
	lateGate := make(chan struct{})
	gate.Store(&lateGate)
	responds.Store(true)
	go func() { done <- send() }()
	select {
	case w := <-done:
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
	case <-time.After(time.Second):
		close(lateGate)
		t.Fatal("cache waited for late conflict")
	}
	close(lateGate)
	waitWatchers(t, g)
	if g.CacheCounts([]string{"endpoint.ryanl.in"})["endpoint.ryanl.in"] != 0 {
		t.Fatal("late conflict retained cache")
	}
	gate.Store(nil)
	if w := send(); w.Code != 409 {
		t.Fatal("conflict not detected again", w.Code)
	}
}
func TestCacheGenerationIsolationAndInFlightInvalidation(t *testing.T) {
	g := New(nil)
	a := fixture("http://127.0.0.1:1", 1)
	b := fixture("http://127.0.0.1:2", 2)
	b.Service.Domain = "other.ryanl.in"
	g.Update([]Route{a, b})
	request := func(host, method, path string) *http.Request {
		return httptest.NewRequest(method, "http://"+host+path, nil)
	}
	ka := routeKey(request(a.Service.Domain, "GET", "/api?q=1"), g.routes.Load().Domains[a.Service.Domain])
	kb := routeKey(request(b.Service.Domain, "GET", "/api?q=1"), g.routes.Load().Domains[b.Service.Domain])
	g.learn(ka, a)
	g.learn(kb, b)
	for _, r := range []*http.Request{request(a.Service.Domain, "POST", "/api?q=1"), request(a.Service.Domain, "GET", "/api?q=2"), request(a.Service.Domain, "GET", "/different?q=1")} {
		if _, ok := g.cached(routeKey(r, g.routes.Load().Domains[a.Service.Domain])); ok {
			t.Fatal("request key collision")
		}
	}
	g.Update([]Route{b, a}) // an unrelated Docker event with identical routes
	if _, ok := g.cached(ka); !ok {
		t.Fatal("unchanged routes flushed cache")
	}
	if n := g.ClearDomains([]string{a.Service.Domain}); n != 1 {
		t.Fatal(n)
	}
	g.learn(ka, a) // completion from before the invalidation
	if n := g.CacheCounts([]string{a.Service.Domain})[a.Service.Domain]; n != 0 {
		t.Fatal("old completion repopulated cache")
	}
	if _, ok := g.cached(kb); !ok {
		t.Fatal("unrelated domain flushed")
	}
	changed := a
	changed.URL = "http://127.0.0.1:3"
	key := routeKey(request(a.Service.Domain, "GET", "/new"), g.routes.Load().Domains[a.Service.Domain])
	g.learn(key, a)
	g.Update([]Route{changed, b})
	if _, ok := g.cached(key); ok {
		t.Fatal("changed backend retained cache")
	}
	if _, ok := g.cached(kb); !ok {
		t.Fatal("backend change flushed unrelated domain")
	}
}
func TestCacheBoundedWithSignedProbeAndOversizedKey(t *testing.T) {
	g := New([]byte("key"))
	a := fixture("http://127.0.0.1:1", 1)
	g.Update([]Route{a})
	for i := 0; i < cacheLimit+5; i++ {
		req := httptest.NewRequest("GET", "http://endpoint.ryanl.in/"+strings.Repeat("x", i%2)+time.Unix(int64(i), 0).Format(time.RFC3339), nil)
		g.learn(routeKey(req, g.routes.Load().Domains[a.Service.Domain]), a)
	}
	if len(g.cache) != cacheLimit {
		t.Fatal("cache limit not enforced", len(g.cache))
	}
	req := httptest.NewRequest("GET", "http://endpoint.ryanl.in/healthz", nil)
	g.Sign(req, 1, "web")
	_, _, probe := g.selectRoutes(req)
	if !probe {
		t.Fatal("probe not identified")
	}
	tooLong := routeKey(httptest.NewRequest("GET", "http://endpoint.ryanl.in/"+strings.Repeat("x", maxCacheKey), nil), g.routes.Load().Domains[a.Service.Domain])
	g.learn(tooLong, a)
	if _, ok := g.cached(tooLong); ok {
		t.Fatal("oversized key retained")
	}
}
func TestCachedWebSocketReturnsBeforeSlow404(t *testing.T) {
	var gate atomic.Pointer[chan struct{}]
	var hits atomic.Int32
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if e != nil {
			return
		}
		defer c.CloseNow()
		typ, b, e := c.Read(r.Context())
		if e == nil {
			c.Write(r.Context(), typ, b)
		}
	}))
	defer owner.Close()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if p := gate.Load(); p != nil {
			<-*p
		}
		http.NotFound(w, r)
	}))
	defer other.Close()
	g := New(nil)
	g.Update([]Route{fixture(owner.URL, 1), fixture(other.URL, 2)})
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { r.Host = "endpoint.ryanl.in"; g.ServeHTTP(w, r) }))
	defer front.Close()
	dial := func() string {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		c, resp, e := websocket.Dial(ctx, "ws"+strings.TrimPrefix(front.URL, "http")+"/ws", nil)
		if e != nil {
			t.Error(e)
			return ""
		}
		defer c.CloseNow()
		c.Write(ctx, websocket.MessageText, []byte("echo"))
		_, b, e := c.Read(ctx)
		if e != nil || string(b) != "echo" {
			t.Error(e, string(b))
		}
		return resp.Header.Get("X-Paas-Route-Cache")
	}
	if v := dial(); v != "miss" {
		t.Fatal(v)
	}
	delayed := make(chan struct{})
	gate.Store(&delayed)
	if v := dial(); v != "hit" {
		close(delayed)
		t.Fatal(v)
	}
	close(delayed)
	waitWatchers(t, g)
	if hits.Load() != 2 {
		t.Fatal("WebSocket copy suppressed")
	}
}
func TestCached404FallsBackWithoutRepeatingWrite(t *testing.T) {
	var moved atomic.Bool
	var ah, bh atomic.Int32
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ah.Add(1)
		if moved.Load() {
			http.NotFound(w, r)
		} else {
			w.Write([]byte("a"))
		}
	}))
	defer a.Close()
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bh.Add(1)
		if moved.Load() {
			w.Write([]byte("b"))
		} else {
			http.NotFound(w, r)
		}
	}))
	defer b.Close()
	g := New(nil)
	g.Update([]Route{fixture(a.URL, 1), fixture(b.URL, 2)})
	for _, expected := range []string{"a", "b"} {
		w := httptest.NewRecorder()
		g.ServeHTTP(w, httptest.NewRequest("POST", "http://endpoint.ryanl.in/api", strings.NewReader("one-write")))
		result, _ := io.ReadAll(w.Result().Body)
		if string(result) != expected {
			t.Fatal(w.Code, string(result))
		}
		moved.Store(true)
	}
	if ah.Load() != 2 || bh.Load() != 2 {
		t.Fatal("fallback repeated a write")
	}
}
