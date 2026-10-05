package github

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type requestTransport func(*http.Request) (*http.Response, error)

func (f requestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestJobReadTokenIsRequestScopedAndRepositoryBound(t *testing.T) {
	c := Client{HTTP: &http.Client{Transport: requestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.github.com" || r.URL.Path != "/repos/RyanStanLin/private-demo" || r.Method != "GET" || r.Header.Get("Authorization") != "Bearer scoped-read-token" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":42,"name":"private-demo","full_name":"RyanStanLin/private-demo","default_branch":"main","owner":{"id":93820487,"login":"RyanStanLin"}}`)), Header: make(http.Header)}, nil
	})}}
	ctx := WithReadToken(context.Background(), "scoped-read-token")
	id, err := c.Identity(ctx, "RyanStanLin/private-demo", 42)
	if err != nil || id.OwnerID != 93820487 {
		t.Fatal(id, err)
	}
	if _, err = c.Identity(ctx, "RyanStanLin/private-demo", 99); err == nil {
		t.Fatal("accepted repository ID mismatch")
	}
	if len(c.tokens) != 0 {
		t.Fatal("job token was cached")
	}
	if _, err = c.Token(context.Background(), "RyanStanLin/private-demo", 42); err == nil {
		t.Fatal("request token escaped its context")
	}
}
