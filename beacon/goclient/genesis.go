package goclient

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/api"
	v1 "github.com/attestantio/go-eth2-client/api/v1"
)

// Genesis must be called if GoClient is initialized (gc.beaconConfig is set)
func (gc *GoClient) Genesis() *v1.Genesis {
	if gc.genesis == nil {
		gc.log.Fatal("Genesis must be called after GoClient is initialized (gc.beaconConfig is set)")
	}
	return gc.genesis
}

// fetchGenesis must be called once on GoClient's initialization
func (gc *GoClient) fetchGenesis() (*v1.Genesis, error) {
	genesisResponse, err := gc.client.Genesis(gc.ctx, &api.GenesisOpts{})
	if err != nil {
		return nil, fmt.Errorf("failed to obtain genesis response: %w", err)
	}
	if genesisResponse == nil {
		return nil, fmt.Errorf("genesis response is nil")
	}
	if genesisResponse.Data == nil {
		return nil, fmt.Errorf("genesis response data is nil")
	}

	return genesisResponse.Data, nil
}
