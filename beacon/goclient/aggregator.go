package goclient

import (
	"encoding/binary"
	"hash"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/minio/sha256-simd"
	"github.com/pkg/errors"
)

// SubmitAggregateSelectionProof returns an AggregateAndProof object
func (gc *goClient) SubmitAggregateSelectionProof(slot phase0.Slot, committeeIndex phase0.CommitteeIndex, committeeLength uint64, validatorIndex phase0.ValidatorIndex, slotSig []byte) (*phase0.AggregateAndProof, error) {
	// As specified in spec, an aggregator should wait until two thirds of the way through slot
	// to broadcast the best aggregate to the global aggregate channel.
	// https://github.com/ethereum/consensus-specs/blob/v0.9.3/specs/validator/0_beacon-chain-validator.md#broadcast-aggregate
	gc.waitToSlotTwoThirds(slot)

	// differ from spec because we need to subscribe to subnet
	isAggregator, err := isAggregator(committeeLength, slotSig)
	if err != nil {
		return nil, errors.Wrap(err, "could not get aggregator status")
	}
	if !isAggregator {
		return nil, errors.New("validator is not an aggregator")
	}

	data, err := gc.client.AttestationData(gc.ctx, slot, committeeIndex)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, errors.New("attestation data is nil")
	}

	// Get aggregate attestation data.
	root, err := data.HashTreeRoot()
	if err != nil {
		return nil, errors.Wrap(err, "AttestationData.HashTreeRoot")
	}
	aggregateData, err := gc.client.AggregateAttestation(gc.ctx, slot, root)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get aggregate attestation")
	}
	if aggregateData == nil {
		return nil, errors.New("aggregation data is nil")
	}

	var selectionProof phase0.BLSSignature
	copy(selectionProof[:], slotSig)

	return &phase0.AggregateAndProof{
		AggregatorIndex: validatorIndex,
		Aggregate:       aggregateData,
		SelectionProof:  selectionProof,
	}, nil
}

// SubmitSignedAggregateSelectionProof broadcasts a signed aggregator msg
func (gc *goClient) SubmitSignedAggregateSelectionProof(msg *phase0.SignedAggregateAndProof) error {
	return gc.client.SubmitAggregateAttestations(gc.ctx, []*phase0.SignedAggregateAndProof{msg})
}

// IsAggregator returns true if the signature is from the input validator. The committee
// count is provided as an argument rather than imported implementation from spec. Having
// committee count as an argument allows cheaper computation at run time.
//
// Spec pseudocode definition:
//
//	def is_aggregator(state: BeaconState, slot: Slot, index: CommitteeIndex, slot_signature: BLSSignature) -> bool:
//	 committee = get_beacon_committee(state, slot, index)
//	 modulo = max(1, len(committee) // TARGET_AGGREGATORS_PER_COMMITTEE)
//	 return bytes_to_uint64(hash(slot_signature)[0:8]) % modulo == 0
func isAggregator(committeeCount uint64, slotSig []byte) (bool, error) {
	//modulo := uint64(1)
	// TODO(oleg) prysm params TargetAggregatorsPerCommittee:  16
	//if committeeCount/params.BeaconConfig().TargetAggregatorsPerCommittee > 1 {
	//	modulo = committeeCount / params.BeaconConfig().TargetAggregatorsPerCommittee
	//}

	modulo := committeeCount / TargetAggregatorsPerCommittee

	// TODO(oleg) prysm
	//b := hash.Hash(slotSig)
	b := Hash(slotSig)
	return binary.LittleEndian.Uint64(b[:8])%modulo == 0, nil
}

var sha256Pool = sync.Pool{New: func() interface{} {
	return sha256.New()
}}

// Hash defines a function that returns the sha256 checksum of the data passed in.
// https://github.com/ethereum/consensus-specs/blob/v0.9.3/specs/core/0_beacon-chain.md#hash
func Hash(data []byte) [32]byte {
	h, ok := sha256Pool.Get().(hash.Hash)
	if !ok {
		h = sha256.New()
	}
	defer sha256Pool.Put(h)
	h.Reset()

	var b [32]byte

	// The hash interface never returns an error, for that reason
	// we are not handling the error below. For reference, it is
	// stated here https://golang.org/pkg/hash/#Hash

	// #nosec G104
	h.Write(data)
	h.Sum(b[:0])

	return b
}

// waitOneThirdOrValidBlock waits until one-third of the slot has transpired (SECONDS_PER_SLOT / 3 seconds after the start of slot)
func (gc *goClient) waitToSlotTwoThirds(slot phase0.Slot) {
	oneThird := gc.network.DivideSlotBy(3 /* one third of slot duration */)
	twoThird := oneThird + oneThird
	delay := twoThird

	startTime := gc.slotStartTime(slot)
	finalTime := startTime.Add(delay)
	//TODO(oleg) changed from prysmtime
	wait := time.Until(finalTime)
	if wait <= 0 {
		return
	}

	t := time.NewTimer(wait)
	defer t.Stop()
	for range t.C {
		return
	}
}
