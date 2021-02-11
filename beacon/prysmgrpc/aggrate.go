package prysmgrpc

import (
	"context"
	"encoding/binary"
	"time"

	"github.com/prysmaticlabs/prysm/shared/slotutil"
	"github.com/prysmaticlabs/prysm/shared/timeutils"

	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"

	"github.com/pkg/errors"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/shared/hashutil"
	"github.com/prysmaticlabs/prysm/shared/params"
)

// GetAggregationData returns aggregation data
func (b *prysmGRPC) GetAggregationData(ctx context.Context, slot, committeeIndex uint64) (*ethpb.AggregateAttestationAndProof, error) {
	b.waitToSlotTwoThirds(ctx, slot)

	slotSig, err := b.signSlot(ctx, slot)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign slot")
	}

	res, err := b.validatorClient.SubmitAggregateSelectionProof(ctx, &ethpb.AggregateSelectionRequest{
		Slot:           slot,
		CommitteeIndex: committeeIndex,
		PublicKey:      b.validatorPublicKey,
		SlotSignature:  slotSig,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to submit aggregation proof")
	}

	return res.GetAggregateAndProof(), nil
}

// SignAggregation signs the given aggregation data
func (b *prysmGRPC) SignAggregation(ctx context.Context, data *ethpb.AggregateAttestationAndProof) (*ethpb.SignedAggregateAttestationAndProof, error) {
	sig, err := b.aggregateAndProofSig(ctx, data)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign aggregate and proof")
	}

	return &ethpb.SignedAggregateAttestationAndProof{
		Message:   data,
		Signature: sig,
	}, nil
}

// SubmitAggregation submits the given signed aggregation data
func (b *prysmGRPC) SubmitAggregation(ctx context.Context, data *ethpb.SignedAggregateAttestationAndProof) error {
	_, err := b.validatorClient.SubmitSignedAggregateSelectionProof(ctx, &ethpb.SignedAggregateSubmitRequest{
		SignedAggregateAndProof: data,
	})
	if err != nil {
		return errors.Wrap(err, "failed to submit signed aggregation data")
	}

	return nil
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

// aggregateAndProofSig returns the signature of validator signing over aggregate and proof object.
func (b *prysmGRPC) aggregateAndProofSig(ctx context.Context, agg *ethpb.AggregateAttestationAndProof) ([]byte, error) {
	domain, err := b.domainData(ctx, agg.Aggregate.Data.Slot, params.BeaconConfig().DomainAggregateAndProof[:])
	if err != nil {
		return nil, err
	}

	root, err := helpers.ComputeSigningRoot(agg, domain.SignatureDomain)
	if err != nil {
		return nil, err
	}

	return b.privateKey.SignByte(root[:]).Serialize(), nil
}

// waitToSlotTwoThirds waits until two third through the current slot period
// such that any attestations from this slot have time to reach the beacon node
// before creating the aggregated attestation.
func (b *prysmGRPC) waitToSlotTwoThirds(ctx context.Context, slot uint64) {
	oneThird := slotutil.DivideSlotBy(3 /* one third of slot duration */)
	twoThird := oneThird + oneThird
	delay := twoThird

	startTime := slotutil.SlotStartTime(b.network.MinGenesisTime(), slot)
	finalTime := startTime.Add(delay)
	wait := timeutils.Until(finalTime)
	if wait <= 0 {
		return
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return
	case <-t.C:
		return
	}
}
