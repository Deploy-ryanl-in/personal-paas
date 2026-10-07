package gateway

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
)

type websocketResult struct {
	route    Route
	conn     *websocket.Conn
	response *http.Response
	err      error
}

func (v websocketResult) close() {
	if v.conn != nil {
		v.conn.CloseNow()
	}
	if v.response != nil && v.response.Body != nil {
		v.response.Body.Close()
	}
}
func (g *Gateway) dialWebSocket(req *http.Request, route Route) websocketResult {
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
	protocols := []string{}
	for _, v := range strings.Split(req.Header.Get("Sec-WebSocket-Protocol"), ",") {
		if v = strings.TrimSpace(v); v != "" {
			protocols = append(protocols, v)
		}
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(req.Context()), 8*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, target.String(), &websocket.DialOptions{HTTPClient: &http.Client{Transport: g.transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, HTTPHeader: headers, Subprotocols: protocols})
	if c != nil {
		c.SetReadLimit(MaxMessage)
	}
	return websocketResult{route, c, resp, err}
}
func (g *Gateway) websocket(w http.ResponseWriter, req *http.Request, routes []Route, key cacheKey) {
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
	results := make(chan websocketResult, len(routes))
	for _, route := range routes {
		go func(route Route) { results <- g.dialWebSocket(req, route) }(route)
	}
	responders := []websocketResult{}
	defer func() {
		for _, v := range responders {
			v.close()
		}
	}()
	failure := false
	w.Header().Set("X-Paas-Recipients", strconv.Itoa(len(routes)))
	w.Header().Set("X-Paas-Route-Cache", "miss")
	for remaining := len(routes); remaining > 0; {
		v := <-results
		remaining--
		if v.conn == nil && (v.response == nil || v.response.StatusCode == 404) {
			if v.response == nil {
				failure = true
				g.forget(key)
			}
			v.close()
			if hit && sameRoute(v.route, cached) {
				g.forget(key)
			}
			continue
		}
		responders = append(responders, v)
		if hit && sameRoute(v.route, cached) && v.conn != nil && len(responders) == 1 && !failure {
			w.Header().Set("X-Paas-Route-Cache", "hit")
			detached = true
			go g.observeWebSockets(key, results, remaining)
			g.bridge(w, req, v.conn)
			return
		}
	}
	if len(responders) > 1 {
		g.forget(key)
		conflict(w)
		return
	}
	if failure {
		http.Error(w, "shared WebSocket participant unavailable", 502)
		return
	}
	if len(responders) == 0 {
		g.forget(key)
		http.NotFound(w, req)
		return
	}
	v := responders[0]
	if v.conn != nil {
		if len(routes) > 1 {
			g.learn(key, v.route)
		}
		g.bridge(w, req, v.conn)
		return
	}
	g.forget(key)
	removeHop(v.response.Header)
	for k, h := range v.response.Header {
		w.Header()[k] = h
	}
	w.WriteHeader(v.response.StatusCode)
	io.Copy(w, io.LimitReader(v.response.Body, MaxBody))
}
func (g *Gateway) observeWebSockets(key cacheKey, results <-chan websocketResult, remaining int) {
	defer func() { <-g.watchers }()
	for ; remaining > 0; remaining-- {
		v := <-results
		if v.conn != nil || v.response == nil || v.response.StatusCode != 404 {
			g.forget(key)
			slog.Warn("route cache invalidated after late WebSocket response or failure", "domain", key.Domain, "repository", v.route.RepositoryID, "service", v.route.ServiceName)
		}
		v.close()
	}
}
