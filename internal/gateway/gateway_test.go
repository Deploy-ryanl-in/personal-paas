package gateway

import (
	"context"
	"github.com/Deploy-ryanl-in/personal-paas/internal/manifest"
	"github.com/coder/websocket"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixture(url string, id int64, paths ...string) Route {
	return Route{RepositoryID: id, ServiceName: "web", URL: url, Service: manifest.Service{Type: "web", Domain: "endpoint.ryanl.in", Routing: &manifest.Routing{SharedGroup: "endpoint", Paths: paths}, Health: &manifest.Health{Path: "/healthz", Status: 200}}}
}
func TestSharedHTTPDispatchAndConflict(t *testing.T) {
	var userHits, appHits atomic.Int32
	user := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userHits.Add(1)
		b, _ := io.ReadAll(r.Body)
		if string(b) != "payload" {
			t.Error("body lost")
		}
		if r.URL.Path == "/api/adduser" {
			w.Write([]byte("user"))
			return
		}
		if r.URL.Path == "/common" {
			w.Write([]byte("user"))
			return
		}
		http.NotFound(w, r)
	}))
	defer user.Close()
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		appHits.Add(1)
		if r.URL.Path == "/common" {
			w.Write([]byte("app"))
			return
		}
		http.NotFound(w, r)
	}))
	defer app.Close()
	g := New([]byte("test-key"))
	g.Update([]Route{fixture(user.URL, 1), fixture(app.URL, 2)})
	send := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://endpoint.ryanl.in"+path, strings.NewReader("payload"))
		w := httptest.NewRecorder()
		g.ServeHTTP(w, req)
		return w
	}
	if w := send("/api/adduser"); w.Code != 200 || w.Body.String() != "user" {
		t.Fatal(w.Code, w.Body.String())
	}
	if userHits.Load() != 1 || appHits.Load() != 1 {
		t.Fatal("request was not copied exactly once")
	}
	if w := send("/common"); w.Code != 409 || !strings.Contains(w.Body.String(), "multi_service_conflict") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := send("/unknown"); w.Code != 404 {
		t.Fatal(w.Code)
	}
	g.Update([]Route{fixture(user.URL, 1, "/api/*"), fixture(app.URL, 2, "/api/app")})
	before := appHits.Load()
	if w := send("/api/adduser"); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if appHits.Load() != before {
		t.Fatal("path filter ignored")
	}
}
func TestSignedProbeSelectsOnlyExactHealthBackend(t *testing.T) {
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("a")) }))
	defer a.Close()
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Paas-Probe") != "" {
			t.Error("probe leaked")
		}
		w.Write([]byte("b"))
	}))
	defer b.Close()
	g := New([]byte("key"))
	g.Update([]Route{fixture(a.URL, 1, "/api/user"), fixture(b.URL, 2, "/api/app")})
	req := httptest.NewRequest("GET", "https://endpoint.ryanl.in/healthz", nil)
	g.Sign(req, 2, "web")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, req)
	if w.Code != 200 || w.Body.String() != "b" {
		t.Fatal(w.Code, w.Body.String())
	}
	req = httptest.NewRequest("GET", "https://endpoint.ryanl.in/healthz", nil)
	g.Sign(req, 2, "web")
	req.URL.Path = "/secret"
	w = httptest.NewRecorder()
	g.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatal("probe signature not path-bound")
	}
}
func TestWebSocketDispatchAndConflict(t *testing.T) {
	backend := func(path string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != path {
				http.NotFound(w, r)
				return
			}
			c, e := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
			if e != nil {
				return
			}
			defer c.CloseNow()
			for {
				typ, data, e := c.Read(context.Background())
				if e != nil {
					return
				}
				if e = c.Write(context.Background(), typ, data); e != nil {
					return
				}
			}
		}))
	}
	a, b := backend("/ws/user"), backend("/ws/app")
	defer a.Close()
	defer b.Close()
	g := New([]byte("key"))
	g.Update([]Route{fixture(a.URL, 1), fixture(b.URL, 2)})
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { r.Host = "endpoint.ryanl.in"; g.ServeHTTP(w, r) }))
	defer front.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, e := websocket.Dial(ctx, "ws"+strings.TrimPrefix(front.URL, "http")+"/ws/user", nil)
	if e != nil {
		t.Fatal(e)
	}
	defer c.CloseNow()
	if e = c.Write(ctx, websocket.MessageText, []byte("hello")); e != nil {
		t.Fatal(e)
	}
	_, msg, e := c.Read(ctx)
	if e != nil || string(msg) != "hello" {
		t.Fatal(e, string(msg))
	}
	c.Close(websocket.StatusNormalClosure, "")
	g.Update([]Route{fixture(a.URL, 1), fixture(a.URL, 2)})
	_, resp, e := websocket.Dial(ctx, "ws"+strings.TrimPrefix(front.URL, "http")+"/ws/user", nil)
	if e == nil || resp == nil || resp.StatusCode != 409 {
		t.Fatal("ambiguous upgrade accepted", e, resp)
	}
}

func TestWebSocketBackendCannotRedirectGatewayOutsideItsContainer(t *testing.T) {
	var outsideHits atomic.Int32
	outside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { outsideHits.Add(1); w.WriteHeader(401) }))
	defer outside.Close()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, outside.URL+"/sensitive", http.StatusFound)
	}))
	defer backend.Close()
	g := New([]byte("key"))
	g.Update([]Route{fixture(backend.URL, 1)})
	request := httptest.NewRequest("GET", "http://endpoint.ryanl.in/ws", nil)
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Connection", "Upgrade")
	response := httptest.NewRecorder()
	g.ServeHTTP(response, request)
	if outsideHits.Load() != 0 {
		t.Fatal("gateway followed an application redirect outside its backend")
	}
	if response.Code != http.StatusFound {
		t.Fatal("unexpected response", response.Code)
	}
}
