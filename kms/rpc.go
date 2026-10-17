package kms

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"go.uber.org/zap"
)

// publicRPCs back a hosted provider (an https URL with a key, which rate-limits in bursts) on the
// chains friends mode serves. A local or self-run node is used alone.
var publicRPCs = map[uint64][]string{
	1:     {"https://ethereum-rpc.publicnode.com", "https://eth.drpc.org"},
	42161: {"https://arbitrum-one-rpc.publicnode.com", "https://arb1.arbitrum.io/rpc"},
	8453:  {"https://base-rpc.publicnode.com", "https://mainnet.base.org"},
}

var localRPC = regexp.MustCompile(`localhost|127\.0\.0\.1|anvil`)

// HostedRPC says whether url is a hosted provider (https, not a local node).
func HostedRPC(url string) bool { return hostedRPC(url) }

func hostedRPC(u string) bool {
	return len(u) > 8 && u[:8] == "https://" && !localRPC.MatchString(u)
}

const (
	maxRetries      = 3                      // per request
	backoffBase     = 250 * time.Millisecond // first cooldown after a failure, doubled per consecutive failure
	backoffCap      = 8 * time.Second
	retryAfterCap   = 30 * time.Second
	retryTokens     = 10.0 // shared retry budget (gRPC-style throttling)
	retryTokenRefil = 0.1  // tokens back per successful request
	defaultRPS      = 1.0  // client-side limit on the hosted endpoint (burst 3×)
)

// dialRPC dials an EVM JSON-RPC endpoint. A hosted URL is rate-limited on our side and backed by
// the chain's public endpoints (see failover); a local node is dialed as is.
func dialRPC(ctx context.Context, rawURL string, chainID uint64) (*ethclient.Client, error) {
	if !hostedRPC(rawURL) {
		return ethclient.DialContext(ctx, rawURL)
	}
	rps := defaultRPS
	if v, err := strconv.ParseFloat(os.Getenv("DKMS_RPC_RPS"), 64); err == nil && v > 0 {
		rps = v
	}
	f, err := newFailover(append([]string{rawURL}, publicRPCs[chainID]...), rps)
	if err != nil {
		return nil, err
	}
	c, err := rpc.DialOptions(ctx, rawURL, rpc.WithHTTPClient(&http.Client{Timeout: 20 * time.Second, Transport: f}))
	if err != nil {
		return nil, err
	}
	return ethclient.NewClient(c), nil
}

type endpoint struct {
	url     *url.URL
	fails   int       // consecutive retryable failures
	until   time.Time // cooling down until
	limiter *bucket   // the hosted endpoint only
}

// failover sends a request to the first endpoint that isn't cooling down (the hosted one first, while
// it's under its rate limit). A retryable failure (network, 408, 429, 502-504) cools that endpoint
// down (Retry-After, or exponential backoff with full jitter) and the request moves on, within a
// per-request and a shared retry budget. Anything else, including JSON-RPC errors such as a revert,
// is the answer and is never retried.
type failover struct {
	eps  []*endpoint
	next http.RoundTripper

	mu     sync.Mutex
	budget float64

	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
	jit   func() float64
}

func newFailover(urls []string, rps float64) (*failover, error) {
	f := &failover{next: http.DefaultTransport, budget: retryTokens, now: time.Now, sleep: sleepCtx, jit: rand.Float64}
	for i, raw := range urls {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, err
		}
		ep := &endpoint{url: u}
		if i == 0 {
			burst := max(1, 3*rps)
			ep.limiter = &bucket{rate: rps, burst: burst, tokens: burst}
		}
		f.eps = append(f.eps, ep)
	}
	return f, nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// pick returns the endpoint to use now, or how long until one is free.
func (f *failover) pick() (*endpoint, time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.now()
	wait := time.Duration(math.MaxInt64)
	for _, ep := range f.eps {
		if d := ep.until.Sub(now); d > 0 {
			wait = min(wait, d)
			continue
		}
		if ep.limiter != nil {
			if d := ep.limiter.take(now); d > 0 {
				wait = min(wait, d)
				continue
			}
		}
		return ep, 0
	}
	return nil, wait
}

func (f *failover) failed(ep *endpoint, resp *http.Response) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ep.fails++
	d := time.Duration(0)
	if resp != nil && (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable) {
		d = retryAfter(resp.Header.Get("Retry-After"), f.now())
	}
	if d == 0 {
		ceil := min(backoffCap, backoffBase<<min(ep.fails-1, 10))
		d = time.Duration(f.jit() * float64(ceil))
	}
	ep.until = f.now().Add(d)
}

func (f *failover) succeeded(ep *endpoint) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ep.fails = 0
	f.budget = min(retryTokens, f.budget+retryTokenRefil)
}

// retry spends one token of the shared budget, if there is one.
func (f *failover) retry() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.budget < 1 {
		return false
	}
	f.budget--
	return true
}

func retryAfter(h string, now time.Time) time.Duration {
	if h == "" {
		return 0
	}
	var d time.Duration
	if s, err := strconv.Atoi(h); err == nil {
		d = time.Duration(s) * time.Second
	} else if t, err := http.ParseTime(h); err == nil {
		d = t.Sub(now)
	}
	return max(0, min(d, retryAfterCap))
}

func retryable(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}
	switch resp.StatusCode {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

func (f *failover) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, err
		}
		body = b
	}
	methods := rpcMethods(body)
	ctx := req.Context()
	for attempt := 0; ; attempt++ {
		ep, wait := f.pick()
		for ep == nil {
			if err := f.sleep(ctx, wait); err != nil {
				return nil, err
			}
			ep, wait = f.pick()
		}
		r := req.Clone(ctx)
		r.URL, r.Host = ep.url, ep.url.Host
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		resp, err := f.next.RoundTrip(r)
		stats.add(ep.url.Host, methods, resp, err)
		if !retryable(resp, err) {
			if resp.StatusCode < 300 {
				f.succeeded(ep)
			}
			return resp, nil
		}
		f.failed(ep, resp)
		if ctx.Err() != nil || attempt >= maxRetries || !f.retry() {
			return resp, err
		}
		if resp != nil {
			resp.Body.Close()
		}
	}
}

// bucket is a token bucket: rate tokens a second, up to burst.
type bucket struct {
	rate, burst, tokens float64
	last                time.Time
}

// take takes a token, or says how long until one is there.
func (b *bucket) take(now time.Time) time.Duration {
	if !b.last.IsZero() {
		b.tokens = min(b.burst, b.tokens+now.Sub(b.last).Seconds()*b.rate)
	}
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return 0
	}
	return time.Duration((1 - b.tokens) / b.rate * float64(time.Second))
}

func rpcMethods(body []byte) []string {
	var one struct {
		Method string `json:"method"`
	}
	if json.Unmarshal(body, &one) == nil && one.Method != "" {
		return []string{one.Method}
	}
	var batch []struct {
		Method string `json:"method"`
	}
	if json.Unmarshal(body, &batch) == nil && len(batch) > 0 {
		out := make([]string, len(batch))
		for i, m := range batch {
			out[i] = m.Method
		}
		return out
	}
	return []string{"unknown"}
}

// ---------- stats ----------

type rpcStats struct {
	mu     sync.Mutex
	counts map[string]int // host method outcome
}

var stats = &rpcStats{counts: map[string]int{}}

func outcome(resp *http.Response, err error) string {
	switch {
	case err != nil:
		return "neterr"
	case resp.StatusCode < 300:
		return "ok"
	case resp.StatusCode == http.StatusTooManyRequests:
		return "429"
	case resp.StatusCode >= 500:
		return "5xx"
	}
	return strconv.Itoa(resp.StatusCode)
}

func (s *rpcStats) add(host string, methods []string, resp *http.Response, err error) {
	o := outcome(resp, err)
	s.mu.Lock()
	for _, m := range methods {
		s.counts[host+" "+m+" "+o]++
	}
	s.mu.Unlock()
}

func (s *rpcStats) take() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.counts
	s.counts = map[string]int{}
	return out
}

// StartRPCStats logs, once a minute, how many requests went to each RPC host (never the URL path,
// which carries the provider key) by method and outcome.
func StartRPCStats(ctx context.Context, logger *zap.Logger) {
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			c := stats.take()
			if len(c) == 0 {
				continue
			}
			keys := make([]string, 0, len(c))
			for k := range c {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			type row struct {
				Call  string `json:"call"`
				Count int    `json:"n"`
			}
			rows := make([]row, len(keys))
			for i, k := range keys {
				rows[i] = row{k, c[k]}
			}
			logger.Info("rpc stats", zap.String("event", "rpc_stats"), zap.Any("calls", rows))
		}
	}()
}
