package kms

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// publicRPCs back a hosted provider (an https URL with a key, which rate-limits in bursts) on the
// chains friends mode serves. A local or self-run node is used alone.
var publicRPCs = map[uint64][]string{
	1:     {"https://ethereum-rpc.publicnode.com", "https://eth.drpc.org"},
	42161: {"https://arbitrum-one-rpc.publicnode.com", "https://arb1.arbitrum.io/rpc"},
	8453:  {"https://base-rpc.publicnode.com", "https://mainnet.base.org"},
}

var localRPC = regexp.MustCompile(`localhost|127\.0\.0\.1|anvil`)

func hostedRPC(u string) bool {
	return len(u) > 8 && u[:8] == "https://" && !localRPC.MatchString(u)
}

// dialRPC dials an EVM JSON-RPC endpoint. For a hosted URL, a request the provider rate-limits
// (429) or fails (5xx, network) is retried after a short wait, then sent to the public endpoints.
func dialRPC(ctx context.Context, rawURL string, chainID uint64) (*ethclient.Client, error) {
	if !hostedRPC(rawURL) {
		return ethclient.DialContext(ctx, rawURL)
	}
	urls := []string{rawURL}
	urls = append(urls, publicRPCs[chainID]...)
	client := &http.Client{Timeout: 20 * time.Second, Transport: &failover{urls: urls, next: http.DefaultTransport}}
	c, err := rpc.DialOptions(ctx, rawURL, rpc.WithHTTPClient(client))
	if err != nil {
		return nil, err
	}
	return ethclient.NewClient(c), nil
}

// failover sends each request to the endpoints in order: the first one twice (a rate limit is
// usually over within a second), the others once each.
type failover struct {
	urls []string
	next http.RoundTripper
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
	type try struct {
		url  string
		wait time.Duration
	}
	tries := []try{{f.urls[0], 0}, {f.urls[0], 400 * time.Millisecond}}
	for _, u := range f.urls[1:] {
		tries = append(tries, try{u, 0})
	}
	var resp *http.Response
	var err error
	for i, t := range tries {
		if t.wait > 0 {
			select {
			case <-time.After(t.wait):
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		}
		r := req.Clone(req.Context())
		if t.url != f.urls[0] {
			target, perr := url.Parse(t.url)
			if perr != nil {
				continue
			}
			r.URL, r.Host = target, target.Host
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		resp, err = f.next.RoundTrip(r)
		if err == nil && resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
			return resp, nil
		}
		if req.Context().Err() != nil {
			break
		}
		if i < len(tries)-1 && resp != nil {
			resp.Body.Close()
		}
	}
	return resp, err
}
