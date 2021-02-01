package beacon

import (
	"context"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
)

// GetProposalData implements Beacon interface
func (b *prysmGRPC) GetProposalData(ctx context.Context, slot uint64) (*ethpb.BeaconBlock, error) {
	// TODO: Implement
	return nil, nil
}
