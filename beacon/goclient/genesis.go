package goclient

import (
	"context"
	"fmt"
	"net/http"
	"time"

	client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/api"
	apiv1 "github.com/attestantio/go-eth2-client/api/v1"
)

// genesisForClient makes a Genesis call, the provider of Genesis call is expected to cache the result (and update it
// every once in a while) so that calling this method frequently shouldn't affect the overall performance much.
func (gc *GoClient) genesisForClient(ctx context.Context, provider client.Service) (*apiv1.Genesis, error) {
	start := time.Now()
	genesisResp, err := provider.(client.GenesisProvider).Genesis(ctx, &api.GenesisOpts{})
	recordSingleClientRequest(ctx, gc.log, "Genesis", provider.Address(), http.MethodGet, time.Since(start), err)
	if err != nil {
		return nil, errSingleClient(fmt.Errorf("fetch genesis: %w", err), provider.Address(), "Genesis")
	}
	if genesisResp == nil {
		return nil, errSingleClient(fmt.Errorf("genesis response is nil"), provider.Address(), "Genesis")
	}
	if genesisResp.Data == nil {
		return nil, errSingleClient(fmt.Errorf("genesis response data is nil"), provider.Address(), "Genesis")
	}

	return genesisResp.Data, err
}
