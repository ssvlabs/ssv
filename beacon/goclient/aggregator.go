package goclient

import (
	"encoding/binary"
	eth2client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/config/params"
	"github.com/prysmaticlabs/prysm/crypto/hash"
	"github.com/prysmaticlabs/prysm/time"
	"github.com/prysmaticlabs/prysm/time/slots"
	time2 "time"
)

// SubmitAggregateSelectionProof returns an AggregateAndProof object
func (gc *goClient) SubmitAggregateSelectionProof(duty *spectypes.Duty, slotSig []byte) (*phase0.AggregateAndProof, error) {
	// As specified in spec, an aggregator should wait until two thirds of the way through slot
	// to broadcast the best aggregate to the global aggregate channel.
	// https://github.com/ethereum/consensus-specs/blob/v0.9.3/specs/validator/0_beacon-chain-validator.md#broadcast-aggregate
	gc.waitToSlotTwoThirds(uint64(duty.Slot))

	// Check if the validator is an aggregator
	ok, err := isAggregator(duty.CommitteeLength, slotSig)
	if err != nil {
		return nil, errors.Wrap(err, "Could not get aggregator status")
	}
	if !ok {
		return nil, errors.New("Validator is not an aggregator")
	}

	dataProvider, isProvider := gc.client.(eth2client.AttestationDataProvider)
	if !isProvider {
		return nil, errors.New("client does not support AttestationDataProvider")
	}
	data, err := dataProvider.AttestationData(gc.ctx, duty.Slot, duty.CommitteeIndex)
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
	aggregateAttestationProvider, isProvider := gc.client.(eth2client.AggregateAttestationProvider)
	if !isProvider {
		return nil, errors.New("client does not support AggregateAttestationProvider")
	}
	aggregateData, err := aggregateAttestationProvider.AggregateAttestation(gc.ctx, duty.Slot, root)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get aggregate attestation")
	}
	if aggregateData == nil {
		//a.logger.Warn("Got nil aggregate attestation", zap.Uint64("slot", uint64(data.Slot)))
		return nil, nil
	}

	var selectionProof phase0.BLSSignature
	copy(selectionProof[:], slotSig)

	return &phase0.AggregateAndProof{
		AggregatorIndex: duty.ValidatorIndex,
		Aggregate:       aggregateData,
		SelectionProof:  selectionProof,
	}, nil
}

// SubmitSignedAggregateSelectionProof broadcasts a signed aggregator msg
func (gc *goClient) SubmitSignedAggregateSelectionProof(msg *phase0.SignedAggregateAndProof) error {
	if provider, isProvider := gc.client.(eth2client.AggregateAttestationsSubmitter); isProvider {
		if err := provider.SubmitAggregateAttestations(gc.ctx, []*phase0.SignedAggregateAndProof{msg}); err != nil {
			return err
		}
		return nil
	}
	return errors.New("client does not support AggregateAttestationsSubmitter")
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
	modulo := uint64(1)
	if committeeCount/params.BeaconConfig().TargetAggregatorsPerCommittee > 1 {
		modulo = committeeCount / params.BeaconConfig().TargetAggregatorsPerCommittee
	}

	b := hash.Hash(slotSig)
	return binary.LittleEndian.Uint64(b[:8])%modulo == 0, nil
}

// waitOneThirdOrValidBlock waits until one-third of the slot has transpired (SECONDS_PER_SLOT / 3 seconds after the start of slot)
func (gc *goClient) waitToSlotTwoThirds(slot uint64) {
	oneThird := slots.DivideSlotBy(3 /* one third of slot duration */)
	twoThird := oneThird + oneThird
	delay := twoThird

	startTime := gc.slotStartTime(slot)
	finalTime := startTime.Add(delay)
	wait := time.Until(finalTime)
	if wait <= 0 {
		return
	}

	t := time2.NewTimer(wait)
	defer t.Stop()
	for range t.C {
		return
	}
}
