package gateway

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strconv"
)

type httpResult struct {
	response *http.Response
	err      error
	route    Route
}
type cancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b cancelBody) Close() error { defer b.cancel(); return b.ReadCloser.Close() }
func sameRoute(a, b Route) bool {
	return a.RepositoryID == b.RepositoryID && a.ServiceName == b.ServiceName && a.URL == b.URL
}

// Every participant still receives one copy. A cache hit only removes the wait
// for other participants; it never retries a write or caches response data.
func (g *Gateway) sharedHTTP(w http.ResponseWriter, req *http.Request, routes []Route, key cacheKey) {
	body, err := io.ReadAll(http.MaxBytesReader(w, req.Body, MaxBody))
	if err != nil {
		http.Error(w, "shared request body exceeds 1 MiB", 413)
		return
	}
	cached, hit := g.cached(key)
	if hit {
		select {
		case g.watchers <- struct{}{}:
		default:
			hit = false
		}
	}
	detached := false
	if hit {
		defer func() {
			if !detached {
				<-g.watchers
			}
		}()
	}
	results := make(chan httpResult, len(routes))
	for _, route := range routes {
		request := req
		if hit && !sameRoute(route, cached) {
			ctx, cancel := context.WithCancel(context.WithoutCancel(req.Context()))
			request = req.Clone(ctx)
			go func(route Route, request *http.Request) {
				resp, err := g.dispatch(request, route, body)
				if err != nil {
					cancel()
				} else {
					resp.Body = cancelBody{resp.Body, cancel}
				}
				results <- httpResult{resp, err, route}
			}(route, request)
		} else {
			go func(route Route) { resp, err := g.dispatch(req, route, body); results <- httpResult{resp, err, route} }(route)
		}
	}
	responses := []httpResult{}
	defer func() {
		for _, v := range responses {
			v.response.Body.Close()
		}
	}()
	failures := false
	w.Header().Set("X-Paas-Recipients", strconv.Itoa(len(routes)))
	w.Header().Set("X-Paas-Route-Cache", "miss")
	for remaining := len(routes); remaining > 0; {
		v := <-results
		remaining--
		if v.err != nil {
			failures = true
			g.forget(key)
			slog.Warn("shared HTTP participant failed", "repository", v.route.RepositoryID, "service", v.route.ServiceName)
			continue
		}
		if v.response.StatusCode == 404 {
			v.response.Body.Close()
			if hit && sameRoute(v.route, cached) {
				g.forget(key)
			}
			continue
		}
		responses = append(responses, v)
		if hit && sameRoute(v.route, cached) && v.response.StatusCode >= 200 && v.response.StatusCode < 300 && len(responses) == 1 && !failures {
			w.Header().Set("X-Paas-Route-Cache", "hit")
			detached = true
			go g.observeHTTP(key, results, remaining)
			g.forwardResponse(w, req, v)
			return
		}
	}
	if len(responses) > 1 {
		g.forget(key)
		conflict(w)
		return
	}
	if failures {
		http.Error(w, "shared participant unavailable", 502)
		return
	}
	if len(responses) == 0 {
		g.forget(key)
		http.NotFound(w, req)
		return
	}
	v := responses[0]
	if v.response.StatusCode >= 200 && v.response.StatusCode < 300 {
		g.learn(key, v.route)
	} else {
		g.forget(key)
	}
	g.forwardResponse(w, req, v)
}
func (g *Gateway) forwardResponse(w http.ResponseWriter, req *http.Request, v httpResult) {
	proxy := g.proxy(v.route)
	proxy.Transport = responseTransport{v.response}
	forward := req.Clone(req.Context())
	forward.Body = http.NoBody
	proxy.ServeHTTP(w, forward)
}
func (g *Gateway) observeHTTP(key cacheKey, results <-chan httpResult, remaining int) {
	defer func() { <-g.watchers }()
	for ; remaining > 0; remaining-- {
		v := <-results
		if v.err != nil || v.response.StatusCode != 404 {
			g.forget(key)
			slog.Warn("route cache invalidated after late response or failure", "domain", key.Domain, "repository", v.route.RepositoryID, "service", v.route.ServiceName)
		}
		if v.response != nil {
			v.response.Body.Close()
		}
	}
}
