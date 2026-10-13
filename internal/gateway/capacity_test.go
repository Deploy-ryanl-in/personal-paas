package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func subscriptionBackend(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.Write([]byte("ok"))
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx := c.CloseRead(context.Background())
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				write, stop := context.WithTimeout(ctx, time.Second)
				err = c.Write(write, websocket.MessageText, []byte("tick"))
				stop()
				if err != nil {
					return
				}
			}
		}
	}))
}

func TestLongLivedWebSocketsDoNotBlockHTTP(t *testing.T) {
	backend := subscriptionBackend(t)
	defer backend.Close()
	g := New(nil)
	g.Update([]Route{fixture(backend.URL, 1)})
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { r.Host = "endpoint.ryanl.in"; g.ServeHTTP(w, r) }))
	defer front.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connections := []*websocket.Conn{}
	defer func() {
		for _, c := range connections {
			c.CloseNow()
		}
	}()
	for i := 0; i < 40; i++ {
		c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(front.URL, "http")+"/subscribe", nil)
		if err != nil {
			t.Fatalf("subscription %d: %v", i+1, err)
		}
		connections = append(connections, c)
	}
	request, _ := http.NewRequestWithContext(ctx, "GET", front.URL+"/healthz", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("HTTP starved by subscriptions: %d", response.StatusCode)
	}
	for i, c := range connections {
		_, data, err := c.Read(ctx)
		if err != nil || string(data) != "tick" {
			t.Fatalf("subscription %d stopped: %v %q", i, err, data)
		}
	}
}

func TestWebSocketCapacityRefusalAndRelease(t *testing.T) {
	backend := subscriptionBackend(t)
	defer backend.Close()
	g := New(nil)
	g.wsSlots = make(chan struct{}, 3)
	g.Update([]Route{fixture(backend.URL, 1)})
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { r.Host = "endpoint.ryanl.in"; g.ServeHTTP(w, r) }))
	defer front.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connections := []*websocket.Conn{}
	defer func() {
		for _, c := range connections {
			c.CloseNow()
		}
	}()
	dial := func() (*websocket.Conn, *http.Response, error) {
		return websocket.Dial(ctx, "ws"+strings.TrimPrefix(front.URL, "http")+"/subscribe", nil)
	}
	for i := 0; i < 3; i++ {
		c, _, err := dial()
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, c)
	}
	c, response, err := dial()
	if c != nil {
		c.CloseNow()
	}
	if err == nil || response == nil || response.StatusCode != 503 {
		t.Fatal("capacity overflow was not refused", err, response)
	}
	connections[0].CloseNow()
	deadline := time.Now().Add(time.Second)
	for len(g.wsSlots) == 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	c, _, err = dial()
	if err != nil {
		t.Fatal("closed connection did not release admission", err)
	}
	connections = append(connections, c)
	_, data, err := c.Read(ctx)
	if err != nil || string(data) != "tick" {
		t.Fatal("replacement subscription does not receive events", err)
	}
}

func TestHTTPSaturationDoesNotConsumeWebSocketCapacity(t *testing.T) {
	backend := subscriptionBackend(t)
	defer backend.Close()
	g := New(nil)
	g.Update([]Route{fixture(backend.URL, 1)})
	for i := 0; i < cap(g.slots); i++ {
		g.slots <- struct{}{}
	}
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { r.Host = "endpoint.ryanl.in"; g.ServeHTTP(w, r) }))
	defer front.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(front.URL, "http")+"/subscribe", nil)
	if err != nil {
		t.Fatal("HTTP capacity blocked WebSocket admission", err)
	}
	defer c.CloseNow()
	_, data, err := c.Read(ctx)
	if err != nil || string(data) != "tick" {
		t.Fatal(err)
	}
}

func TestWebSocketPreservesPublicHostAndOrigin(t *testing.T) {
	backend := subscriptionBackend(t)
	defer backend.Close()
	g := New(nil)
	g.Update([]Route{fixture(backend.URL, 1)})
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { g.ServeHTTP(w, r) }))
	defer front.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(front.URL, "http")+"/subscribe", &websocket.DialOptions{Host: "endpoint.ryanl.in", HTTPHeader: http.Header{"Origin": []string{"https://endpoint.ryanl.in"}}})
	if err != nil {
		t.Fatal("public Origin rejected after proxying Host", err)
	}
	defer c.CloseNow()
	_, data, err := c.Read(ctx)
	if err != nil || string(data) != "tick" {
		t.Fatal("subscription did not survive origin verification", err)
	}
}
