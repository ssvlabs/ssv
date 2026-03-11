package relayclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/attestantio/go-builder-client/api"
	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/mev/builderendpoint/relayclient"
)

func TestFactoryCachesByAddress(t *testing.T) {
	t.Parallel()

	f := relayclient.NewFactory(context.Background(), 250*time.Millisecond)

	c1, err := f.Fetch("http://example.com")
	if err != nil {
		t.Fatalf("fetch 1: %v", err)
	}
	c2, err := f.Fetch("http://example.com")
	if err != nil {
		t.Fatalf("fetch 2: %v", err)
	}
	if c1 != c2 {
		t.Fatalf("expected cached client instance")
	}

	c3, err := f.Fetch("http://example.net")
	if err != nil {
		t.Fatalf("fetch 3: %v", err)
	}
	if c1 == c3 {
		t.Fatalf("expected different instance for different address")
	}
}

func TestFactoryTimeoutIsRespected(t *testing.T) {
	t.Parallel()

	// A relay that sleeps longer than our client timeout.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		// Even a 204 is fine here; we mostly care about the client-side timeout.
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	f := relayclient.NewFactory(context.Background(), 50*time.Millisecond)

	provider, err := f.FetchBidProvider(srv.URL)
	if err != nil {
		t.Fatalf("fetch provider: %v", err)
	}

	start := time.Now()
	ctx := context.Background()
	_, err = provider.BuilderBid(ctx, &api.BuilderBidOpts{
		Slot:       1,
		ParentHash: phase0.Hash32{1},
		PubKey:     phase0.BLSPubKey{2},
	})
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("request did not time out in a reasonable duration: %s", time.Since(start))
	}
}
