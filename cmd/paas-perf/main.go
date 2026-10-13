// paas-perf is an opt-in subscription fixture and paced load client.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

type event struct {
	Topic    string `json:"topic"`
	Sequence int64  `json:"sequence"`
	Payload  string `json:"payload"`
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		serve()
		return
	}
	base := flag.String("url", "http://127.0.0.1:8080", "HTTP origin")
	host := flag.String("host", "", "Host override for loopback gateway")
	dial := flag.String("dial", "", "Optional address to dial while retaining URL Host and TLS SNI")
	connections := flag.Int("connections", 1000, "Acknowledged subscriptions")
	rps := flag.Int("rps", 100, "Paced HTTP requests/sec")
	duration := flag.Duration("duration", 120*time.Second, "Sustained window after ramp")
	output := flag.String("output", "", "JSON result file")
	flag.Parse()
	if *connections < 0 || *connections > 10000 || *rps < 1 || *rps > 10000 || *duration < time.Second {
		panic("invalid load parameters")
	}
	result := load(*base, *host, *dial, *connections, *rps, *duration)
	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
	if *output != "" {
		if err := os.WriteFile(*output, append(data, '\n'), 0600); err != nil {
			panic(err)
		}
	}
	if !result.Passed {
		os.Exit(1)
	}
}

func serve() {
	var mu sync.RWMutex
	clients := map[chan []byte]bool{}
	var sent, dropped atomic.Int64
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		var sequence int64
		for range ticker.C {
			sequence++
			data, _ := json.Marshal(event{"ticks", sequence, strings.Repeat("x", 256)})
			mu.RLock()
			for q := range clients {
				select {
				case q <- data:
				default:
					dropped.Add(1)
				}
			}
			mu.RUnlock()
		}
	}()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("/api/payload", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if os.Getenv("PERF_ROLE") == "peer" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true,"payload":"` + strings.Repeat("x", 512) + `"}`))
	})
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		mu.RLock()
		n := len(clients)
		mu.RUnlock()
		json.NewEncoder(w).Encode(map[string]int64{"active": int64(n), "sent": sent.Load(), "dropped": dropped.Load()})
	})
	mux.HandleFunc("/subscribe", func(w http.ResponseWriter, r *http.Request) {
		if os.Getenv("PERF_ROLE") == "peer" {
			http.NotFound(w, r)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		c.SetReadLimit(1024)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, body, err := c.Read(ctx)
		cancel()
		var request struct {
			Operation string `json:"op"`
			Topic     string `json:"topic"`
		}
		if err != nil || json.Unmarshal(body, &request) != nil || request.Operation != "subscribe" || request.Topic != "ticks" {
			c.Close(websocket.StatusPolicyViolation, "subscribe to ticks")
			return
		}
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		err = c.Write(ctx, websocket.MessageText, []byte(`{"subscribed":true,"topic":"ticks"}`))
		cancel()
		if err != nil {
			return
		}
		ctx = c.CloseRead(context.Background())
		q := make(chan []byte, 4)
		mu.Lock()
		clients[q] = true
		mu.Unlock()
		defer func() { mu.Lock(); delete(clients, q); mu.Unlock() }()
		for {
			select {
			case <-ctx.Done():
				return
			case data := <-q:
				writeCtx, stop := context.WithTimeout(ctx, 5*time.Second)
				err = c.Write(writeCtx, websocket.MessageText, data)
				stop()
				if err != nil {
					return
				}
				sent.Add(1)
			}
		}
	})
	s := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	if err := s.ListenAndServe(); err != nil {
		panic(err)
	}
}

type report struct {
	URL             string  `json:"url"`
	Requested       int     `json:"requestedSubscriptions"`
	Established     int     `json:"establishedSubscriptions"`
	DialFailures    int     `json:"dialFailures"`
	Disconnects     int64   `json:"unexpectedDisconnects"`
	SequenceGaps    int64   `json:"sequenceGaps"`
	Messages        int64   `json:"subscriptionMessages"`
	MinimumMessages int64   `json:"minimumMessagesPerSubscriber"`
	TargetRPS       int     `json:"targetRps"`
	Scheduled       int64   `json:"httpScheduled"`
	Completed       int64   `json:"httpCompleted"`
	Errors          int64   `json:"httpErrors"`
	SchedulerDrops  int64   `json:"schedulerDrops"`
	EffectiveRPS    float64 `json:"effectiveRps"`
	P50             float64 `json:"httpP50Ms"`
	P95             float64 `json:"httpP95Ms"`
	P99             float64 `json:"httpP99Ms"`
	WindowSeconds   float64 `json:"windowSeconds"`
	RampSeconds     float64 `json:"rampSeconds"`
	Passed          bool    `json:"passed"`
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sort.Float64s(values)
	i := int(float64(len(values)-1) * p)
	return values[i]
}

func load(base, host, dial string, count, rps int, duration time.Duration) report {
	transport := &http.Transport{Proxy: nil, MaxIdleConns: 256, MaxIdleConnsPerHost: 128, MaxConnsPerHost: 256, ResponseHeaderTimeout: 10 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ForceAttemptHTTP2: true}
	if dial != "" {
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, dial)
		}
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var active, disconnects, gaps, messages atomic.Int64
	var measuring atomic.Bool
	counts := make([]atomic.Int64, count)
	connections := make([]*websocket.Conn, count)
	var dialFailures atomic.Int64
	var readers, dials sync.WaitGroup
	start := time.Now()
	// A bounded ramp avoids making TLS burst capacity the steady-state test.
	for i := 0; i < count; i++ {
		dials.Add(1)
		go func(i int) {
			defer dials.Done()
			h := make(http.Header)
			if host != "" {
				h.Set("Host", host)
			}
			dialCtx, stop := context.WithTimeout(ctx, 15*time.Second)
			c, _, err := websocket.Dial(dialCtx, "ws"+strings.TrimPrefix(base, "http")+"/subscribe", &websocket.DialOptions{HTTPClient: client, HTTPHeader: h})
			if err != nil {
				stop()
				dialFailures.Add(1)
				return
			}
			err = c.Write(dialCtx, websocket.MessageText, []byte(`{"op":"subscribe","topic":"ticks"}`))
			if err == nil {
				_, data, e := c.Read(dialCtx)
				err = e
				if e == nil && !strings.Contains(string(data), `"subscribed":true`) {
					err = fmt.Errorf("missing subscription ack")
				}
			}
			stop()
			if err != nil {
				c.CloseNow()
				dialFailures.Add(1)
				return
			}
			connections[i] = c
			active.Add(1)
			readers.Add(1)
			go func() {
				defer readers.Done()
				defer active.Add(-1)
				var last int64
				for {
					_, data, e := c.Read(ctx)
					if e != nil {
						if measuring.Load() {
							disconnects.Add(1)
						}
						return
					}
					var ev event
					if json.Unmarshal(data, &ev) != nil || ev.Topic != "ticks" || len(ev.Payload) != 256 {
						gaps.Add(1)
						continue
					}
					if measuring.Load() {
						if last != 0 && ev.Sequence != last+1 {
							gaps.Add(1)
						}
						counts[i].Add(1)
						messages.Add(1)
					}
					last = ev.Sequence
				}
			}()
		}(i)
		time.Sleep(5 * time.Millisecond)
	}
	dials.Wait()
	result := report{URL: base, Requested: count, Established: int(active.Load()), DialFailures: int(dialFailures.Load()), TargetRPS: rps, RampSeconds: time.Since(start).Seconds(), WindowSeconds: duration.Seconds()}
	measuring.Store(true)
	var scheduled, completed, httpErrors, schedulerDrops atomic.Int64
	var requests sync.WaitGroup
	var latencies []float64
	var latMu sync.Mutex
	slots := make(chan struct{}, 256)
	window := time.Now()
	// Absolute scheduling preserves offered RPS even when responses are slow.
	total := int(duration.Seconds() * float64(rps))
	for i := 0; i < total; i++ {
		deadline := window.Add(time.Duration(i) * time.Second / time.Duration(rps))
		if delay := time.Until(deadline); delay > 0 {
			time.Sleep(delay)
		}
		scheduled.Add(1)
		select {
		case slots <- struct{}{}:
		default:
			schedulerDrops.Add(1)
			continue
		}
		requests.Add(1)
		go func() {
			defer requests.Done()
			defer func() { <-slots }()
			begin := time.Now()
			r, _ := http.NewRequest("GET", base+"/api/payload", nil)
			if host != "" {
				r.Host = host
			}
			r.Header.Set("User-Agent", "Personal-PaaS-Performance/1.0")
			response, err := client.Do(r)
			if err != nil {
				httpErrors.Add(1)
				return
			}
			body, e := io.ReadAll(io.LimitReader(response.Body, 4096))
			response.Body.Close()
			if e != nil || response.StatusCode != 200 || !strings.Contains(string(body), `"ok":true`) {
				httpErrors.Add(1)
				return
			}
			completed.Add(1)
			latMu.Lock()
			latencies = append(latencies, float64(time.Since(begin).Microseconds())/1000)
			latMu.Unlock()
		}()
	}
	if delay := time.Until(window.Add(duration)); delay > 0 {
		time.Sleep(delay)
	}
	requests.Wait()
	result.Disconnects = disconnects.Load()
	result.SequenceGaps = gaps.Load()
	result.Messages = messages.Load()
	if count > 0 {
		result.MinimumMessages = counts[0].Load()
		for i := range counts {
			if n := counts[i].Load(); n < result.MinimumMessages {
				result.MinimumMessages = n
			}
		}
	}
	result.Scheduled = scheduled.Load()
	result.Completed = completed.Load()
	result.Errors = httpErrors.Load()
	result.SchedulerDrops = schedulerDrops.Load()
	result.EffectiveRPS = float64(result.Completed) / time.Since(window).Seconds()
	result.P50 = percentile(latencies, .5)
	result.P95 = percentile(latencies, .95)
	result.P99 = percentile(latencies, .99)
	result.Passed = result.Established == count && result.Disconnects == 0 && result.SequenceGaps == 0 && result.Errors == 0 && result.SchedulerDrops == 0 && result.Completed == int64(total) && result.EffectiveRPS >= float64(rps)*.99 && (count == 0 || result.MinimumMessages >= int64(duration.Seconds())-2)
	measuring.Store(false)
	cancel()
	for _, c := range connections {
		if c != nil {
			c.CloseNow()
		}
	}
	readers.Wait()
	return result
}
