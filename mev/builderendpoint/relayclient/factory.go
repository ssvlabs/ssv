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
	ctx context.Context

	timeout time.Duration

	mu      sync.Mutex
	clients map[string]builderclient.Service
}

func NewFactory(ctx context.Context, timeout time.Duration) *Factory {
	if ctx == nil {
		ctx = context.Background()
	}
	return &Factory{
		ctx:     ctx,
		timeout: timeout,
		clients: make(map[string]builderclient.Service),
	}
}

func (f *Factory) Fetch(address string) (builderclient.Service, error) {
	if address == "" {
		return nil, errors.New("address is empty")
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if c, ok := f.clients[address]; ok {
		return c, nil
	}

	c, err := httpclient.New(f.ctx,
		httpclient.WithAddress(address),
		httpclient.WithTimeout(f.timeout),
		httpclient.WithEnforceJSON(true),
	)
	if err != nil {
		return nil, errors.Wrap(err, "create relay client")
	}

	f.clients[address] = c
	return c, nil
}

func (f *Factory) FetchBidProvider(address string) (builderclient.BuilderBidProvider, error) {
	c, err := f.Fetch(address)
	if err != nil {
		return nil, err
	}
	p, ok := c.(builderclient.BuilderBidProvider)
	if !ok {
		return nil, fmt.Errorf("relay client for %q does not implement BuilderBidProvider", address)
	}
	return p, nil
}

func (f *Factory) FetchUnblindProvider(address string) (builderclient.UnblindedProposalProvider, error) {
	c, err := f.Fetch(address)
	if err != nil {
		return nil, err
	}
	p, ok := c.(builderclient.UnblindedProposalProvider)
	if !ok {
		return nil, fmt.Errorf("relay client for %q does not implement UnblindedProposalProvider", address)
	}
	return p, nil
}

func (f *Factory) FetchSubmitter(address string) (builderclient.ValidatorRegistrationsSubmitter, error) {
	c, err := f.Fetch(address)
	if err != nil {
		return nil, err
	}
	p, ok := c.(builderclient.ValidatorRegistrationsSubmitter)
	if !ok {
		return nil, fmt.Errorf("relay client for %q does not implement ValidatorRegistrationsSubmitter", address)
	}
	return p, nil
}
