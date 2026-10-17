package kms

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// server answers with status (and Retry-After, if set) and counts the requests it got.
type server struct {
	*httptest.Server
	hits atomic.Int32
}

func newServer(t *testing.T, status int, retryAfter string) *server {
	s := &server{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		w.WriteHeader(status)
		b, _ := io.ReadAll(r.Body)
		_, _ = w.Write(b)
	}))
	t.Cleanup(s.Close)
	return s
}

// testFailover runs on a fake clock: sleeping advances it, and the jitter is the full backoff.
func testFailover(t *testing.T, rps float64, urls ...string) (*failover, *time.Time) {
	f, err := newFailover(urls, rps)
	require.NoError(t, err)
	now := time.Unix(1_700_000_000, 0)
	f.now = func() time.Time { return now }
	f.sleep = func(_ context.Context, d time.Duration) error { now = now.Add(d); return nil }
	f.jit = func() float64 { return 1 }
	return f, &now
}

func post(t *testing.T, f *failover, body string) (int, string) {
	resp, err := (&http.Client{Transport: f}).Post("http://unused", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestFailoverRateLimitedMovesOn(t *testing.T) {
	hosted, public := newServer(t, 429, ""), newServer(t, 200, "")
	f, _ := testFailover(t, 100, hosted.URL, public.URL)
	code, body := post(t, f, `{"id":1,"method":"eth_blockNumber"}`)
	require.Equal(t, 200, code)
	require.Equal(t, `{"id":1,"method":"eth_blockNumber"}`, body)
	require.EqualValues(t, 1, hosted.hits.Load()) // not retried in a loop: cooling down
	require.EqualValues(t, 1, public.hits.Load())

	// While it cools down, the hosted endpoint gets nothing.
	post(t, f, `{"id":2,"method":"eth_call"}`)
	require.EqualValues(t, 1, hosted.hits.Load())
	require.EqualValues(t, 2, public.hits.Load())
}

func TestFailoverNoRetryOnFinalAnswers(t *testing.T) {
	for _, status := range []int{400, 401, 404, 500} {
		s, public := newServer(t, status, ""), newServer(t, 200, "")
		f, _ := testFailover(t, 100, s.URL, public.URL)
		code, _ := post(t, f, `{"method":"eth_call"}`)
		require.Equal(t, status, code)
		require.EqualValues(t, 1, s.hits.Load(), "status %d", status)
		require.EqualValues(t, 0, public.hits.Load(), "status %d", status)
	}
	// A JSON-RPC error (a revert) comes back as a 200: it's the answer.
	reverted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":3,"message":"execution reverted"}}`))
	}))
	defer reverted.Close()
	public := newServer(t, 200, "")
	f, _ := testFailover(t, 100, reverted.URL, public.URL)
	_, body := post(t, f, `{"method":"eth_call"}`)
	require.Contains(t, body, "execution reverted")
	require.EqualValues(t, 0, public.hits.Load())
}

func TestFailoverHonorsRetryAfter(t *testing.T) {
	hosted, public := newServer(t, 429, "7"), newServer(t, 200, "")
	f, now := testFailover(t, 100, hosted.URL, public.URL)
	start := *now
	post(t, f, `{"method":"eth_call"}`)
	require.Equal(t, start.Add(7*time.Second), f.eps[0].until)

	*now = start.Add(6 * time.Second)
	post(t, f, `{"method":"eth_call"}`)
	require.EqualValues(t, 1, hosted.hits.Load())

	*now = start.Add(8 * time.Second)
	post(t, f, `{"method":"eth_call"}`)
	require.EqualValues(t, 2, hosted.hits.Load())

	require.Equal(t, 30*time.Second, retryAfter("600", start)) // capped
	require.Equal(t, 5*time.Second, retryAfter(start.Add(5*time.Second).UTC().Format(http.TimeFormat), start))
}

func TestFailoverBackoffGrows(t *testing.T) {
	s := newServer(t, 503, "")
	f, now := testFailover(t, 100, s.URL)
	ep := f.eps[0]
	var cooldowns []time.Duration
	for i := 0; i < 7; i++ {
		f.failed(ep, nil)
		cooldowns = append(cooldowns, ep.until.Sub(*now))
	}
	require.Equal(t, []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second}, cooldowns)
	f.succeeded(ep)
	require.Zero(t, ep.fails)
}

func TestFailoverRetryBudget(t *testing.T) {
	a, b := newServer(t, 502, ""), newServer(t, 502, "")
	f, _ := testFailover(t, 100, a.URL, b.URL)
	code, _ := post(t, f, `{"method":"eth_call"}`)
	require.Equal(t, 502, code)
	require.EqualValues(t, 1+maxRetries, a.hits.Load()+b.hits.Load()) // at most 3 retries a request

	// The shared budget runs out: then a failure is the answer, with no retry.
	f.budget = 0.5
	before := a.hits.Load() + b.hits.Load()
	post(t, f, `{"method":"eth_call"}`)
	require.EqualValues(t, before+1, a.hits.Load()+b.hits.Load())
}

func TestFailoverRateLimitDivertsToFallback(t *testing.T) {
	hosted, public := newServer(t, 200, ""), newServer(t, 200, "")
	f, _ := testFailover(t, 1, hosted.URL, public.URL) // 1/s, burst 3
	for i := 0; i < 10; i++ {
		post(t, f, `[{"method":"eth_blockNumber"},{"method":"eth_chainId"}]`)
	}
	require.EqualValues(t, 3, hosted.hits.Load())
	require.EqualValues(t, 7, public.hits.Load())

	// Without a fallback the request waits for a token instead.
	only := newServer(t, 200, "")
	g, now := testFailover(t, 1, only.URL)
	start := *now
	for i := 0; i < 5; i++ {
		post(t, g, `{"method":"eth_call"}`)
	}
	require.EqualValues(t, 5, only.hits.Load())
	require.Equal(t, 2*time.Second, now.Sub(start))
}

func TestRPCStats(t *testing.T) {
	stats.take()
	hosted, public := newServer(t, 429, ""), newServer(t, 200, "")
	f, _ := testFailover(t, 100, hosted.URL+"/v3/secret", public.URL)
	post(t, f, `{"method":"eth_getBalance"}`)
	c := stats.take()
	for k := range c {
		require.NotContains(t, k, "secret")
	}
	require.Equal(t, 1, c[strings.TrimPrefix(hosted.URL, "http://")+" eth_getBalance 429"])
	require.Equal(t, 1, c[strings.TrimPrefix(public.URL, "http://")+" eth_getBalance ok"])
}

func TestHostedRPC(t *testing.T) {
	require.True(t, hostedRPC("https://mainnet.infura.io/v3/x"))
	require.False(t, hostedRPC("http://anvil:8545"))
	require.False(t, hostedRPC("https://localhost:8545"))
}
