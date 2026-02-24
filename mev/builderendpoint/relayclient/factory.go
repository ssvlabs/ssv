package relayclient

import (
	"context"
	"fmt"
	"sync"
	"time"

	builderclient "github.com/attestantio/go-builder-client"
	httpclient "github.com/attestantio/go-builder-client/http"
	"github.com/pkg/errors"
)

// Factory creates and caches builder clients per relay address.
//
// It intentionally hides the concrete HTTP client implementation behind the go-builder-client interfaces.
// This keeps relay I/O in the infrastructure layer and out of higher-level strategy code.
type Factory struct {
	timeout      time.Duration
	extraHeaders map[string]string
	enforceJSON  bool

	mu      sync.Mutex
	clients map[string]builderclient.Service
}

type Option func(*Factory)

func WithExtraHeaders(headers map[string]string) Option {
	return func(f *Factory) {
		f.extraHeaders = headers
	}
}

func WithEnforceJSON(enforce bool) Option {
	return func(f *Factory) {
		f.enforceJSON = enforce
	}
}

func NewFactory(timeout time.Duration, opts ...Option) *Factory {
	f := &Factory{
		timeout:      timeout,
		extraHeaders: map[string]string{},
		enforceJSON:  true,
		clients:      make(map[string]builderclient.Service),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(f)
		}
	}
	return f
}

func (f *Factory) Fetch(ctx context.Context, address string) (builderclient.Service, error) {
	if address == "" {
		return nil, errors.New("address is empty")
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if c, ok := f.clients[address]; ok {
		return c, nil
	}

	c, err := httpclient.New(ctx,
		httpclient.WithAddress(address),
		httpclient.WithTimeout(f.timeout),
		httpclient.WithExtraHeaders(f.extraHeaders),
		httpclient.WithEnforceJSON(f.enforceJSON),
	)
	if err != nil {
		return nil, errors.Wrap(err, "create relay client")
	}

	f.clients[address] = c
	return c, nil
}

func (f *Factory) FetchBidProvider(ctx context.Context, address string) (builderclient.BuilderBidProvider, error) {
	c, err := f.Fetch(ctx, address)
	if err != nil {
		return nil, err
	}
	p, ok := c.(builderclient.BuilderBidProvider)
	if !ok {
		return nil, fmt.Errorf("relay client for %q does not implement BuilderBidProvider", address)
	}
	return p, nil
}
