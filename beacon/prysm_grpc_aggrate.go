package beacon

import (
	"context"
	"encoding/binary"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/shared/hashutil"
	"github.com/prysmaticlabs/prysm/shared/params"
)

// GetAggregationData returns aggregation data
func (b *prysmGRPC) GetAggregationData(ctx context.Context, slot, committeeIndex uint64) (*ethpb.SignedAggregateAttestationAndProof, error) {
	// TODO: Implement
	return nil, nil
}

// isAggregator returns true if the given slot is aggregator
func (b *prysmGRPC) isAggregator(ctx context.Context, slot uint64, committeeLen int) (bool, error) {
	slotSig, err := b.signSlot(ctx, slot)
	if err != nil {
		return false, err
	}

	modulo := uint64(1)
	if committeeLen/int(params.BeaconConfig().TargetAggregatorsPerCommittee) > 1 {
		modulo = uint64(committeeLen) / params.BeaconConfig().TargetAggregatorsPerCommittee
	}

	hash := hashutil.Hash(slotSig)
	val := binary.LittleEndian.Uint64(hash[:8])%modulo == 0

	return val, nil
}
