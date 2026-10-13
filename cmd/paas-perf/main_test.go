package main

import (
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLoadValidatesSubscriptionsAndPacedRequests(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/payload" {
			w.Write([]byte(`{"ok":true}`))
			return
		}
		c, e := websocket.Accept(w, r, nil)
		if e != nil {
			return
		}
		defer c.CloseNow()
		ctx := context.Background()
		_, data, e := c.Read(ctx)
		if e != nil || string(data) != `{"op":"subscribe","topic":"ticks"}` {
			t.Error("subscription request missing")
			return
		}
		if e = c.Write(ctx, websocket.MessageText, []byte(`{"subscribed":true}`)); e != nil {
			return
		}
		ctx = c.CloseRead(ctx)
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		var seq int64
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				seq++
				data, _ := json.Marshal(event{"ticks", seq, strings.Repeat("x", 256)})
				if c.Write(ctx, websocket.MessageText, data) != nil {
					return
				}
			}
		}
	}))
	defer s.Close()
	r := load(s.URL, "", "", 4, 20, time.Second)
	if !r.Passed || r.Established != 4 || r.Completed != 20 || r.Scheduled != 20 || r.MinimumMessages < 30 || r.Errors != 0 || r.SequenceGaps != 0 {
		t.Fatalf("incomplete validation: %+v", r)
	}
}
func TestLoadRejectsHTTPFailures(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "failure", 500) }))
	defer s.Close()
	r := load(s.URL, "", "", 0, 10, time.Second)
	if r.Passed || r.Errors != 10 || r.Completed != 0 || r.Scheduled != 10 {
		t.Fatalf("false success: %+v", r)
	}
}
func TestLoadRejectsFailedSubscriptions(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/payload" {
			w.Write([]byte(`{"ok":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer s.Close()
	r := load(s.URL, "", "", 2, 10, time.Second)
	if r.Passed || r.Established != 0 || r.DialFailures != 2 || r.DialFailureSample == "" || r.MinimumMessages != 0 || r.Completed != 10 {
		t.Fatalf("false subscription success: %+v", r)
	}
}
