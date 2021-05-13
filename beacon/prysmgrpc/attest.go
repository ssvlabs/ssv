package prysmgrpc

import (
	"context"
	"fmt"
	"github.com/herumi/bls-eth-go-binary/bls"
	"time"

	"github.com/prysmaticlabs/prysm/shared/featureconfig"
	"github.com/prysmaticlabs/prysm/shared/slotutil"
	"github.com/prysmaticlabs/prysm/shared/timeutils"

	"github.com/pkg/errors"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/go-bitfield"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/shared/params"
)

// GetAttestationData implements Beacon interface
func (b *prysmGRPC) GetAttestationData(ctx context.Context, slot, committeeIndex uint64) (*ethpb.AttestationData, error) {
	b.waitOneThirdOrValidBlock(ctx, slot)

	data, err := b.validatorClient.GetAttestationData(ctx, &ethpb.AttestationDataRequest{
		Slot:           slot,
		CommitteeIndex: committeeIndex,
	})
	if err != nil {
		return nil, errors.Wrap(err, "could not request attestation to sign at slot")
	}

	return data, nil
}

// SignAttestation implements Beacon interface
func (b *prysmGRPC) SignAttestation(ctx context.Context, data *ethpb.AttestationData, duty *ethpb.DutiesResponse_Duty, shareKey *bls.SecretKey) (*ethpb.Attestation, []byte, error) {
	sig, root, err := b.signAtt(ctx, data, shareKey)
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not sign attestation")
	}

	var indexInCommittee uint64
	var found bool
	for i, vID := range duty.GetCommittee() {
		if vID == duty.GetValidatorIndex() {
			indexInCommittee = uint64(i)
			found = true
			break
		}
	}

	if !found {
		return nil, nil, fmt.Errorf("validator ID %d not found in committee of %v", duty.GetValidatorIndex(), duty.GetCommittee())
	}

	aggregationBitfield := bitfield.NewBitlist(uint64(len(duty.GetCommittee())))
	aggregationBitfield.SetBitAt(indexInCommittee, true)

	return &ethpb.Attestation{
		Data:            data,
		AggregationBits: aggregationBitfield,
		Signature:       sig,
	}, root, nil
}

// SubmitAttestation implements Beacon interface
func (b *prysmGRPC) SubmitAttestation(ctx context.Context, attestation *ethpb.Attestation, validatorIndex uint64, publicKey *bls.PublicKey) error {
	indexedAtt := &ethpb.IndexedAttestation{
		AttestingIndices: []uint64{validatorIndex},
		Data:             attestation.GetData(),
		Signature:        attestation.GetSignature(),
	}

	signingRoot, err := b.getSigningRoot(ctx, attestation.GetData())
	if err != nil {
		return errors.Wrap(err, "failed to get signing root")
	}

	if err := b.slashableAttestationCheck(ctx, indexedAtt, publicKey.Serialize(), signingRoot); err != nil {
		return errors.Wrap(err, "failed attestation slashing protection check")
	}

	if _, err := b.validatorClient.ProposeAttestation(ctx, attestation); err != nil {
		return errors.Wrap(err, "could not submit attestation to beacon node")
	}

	return nil
}

// signAtt returns the signature of an attestation data and its signing root.
func (b *prysmGRPC) signAtt(ctx context.Context, data *ethpb.AttestationData, shareKey *bls.SecretKey) ([]byte, []byte, error) {
	root, err := b.getSigningRoot(ctx, data)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to get signing root")
	}

	return shareKey.SignByte(root[:]).Serialize(), root[:], nil
}

// getSigningRoot returns signing root
func (b *prysmGRPC) getSigningRoot(ctx context.Context, data *ethpb.AttestationData) ([32]byte, error) {
	domain, err := b.domainData(ctx, data.GetSlot(), params.BeaconConfig().DomainBeaconAttester[:])
	if err != nil {
		return [32]byte{}, err
	}

	root, err := helpers.ComputeSigningRoot(data, domain.SignatureDomain)
	if err != nil {
		return [32]byte{}, err
	}

	return root, nil
}

// waitOneThirdOrValidBlock waits until (a) or (b) whichever comes first:
//   (a) the validator has received a valid block that is the same slot as input slot
//   (b) one-third of the slot has transpired (SECONDS_PER_SLOT / 3 seconds after the start of slot)
func (b *prysmGRPC) waitOneThirdOrValidBlock(ctx context.Context, slot uint64) {
	// Don't need to wait if requested slot is the same as highest valid slot.
	if slot <= b.highestValidSlot {
		return
	}

	delay := slotutil.DivideSlotBy(3 /* a third of the slot duration */)
	startTime := slotutil.SlotStartTime(b.network.MinGenesisTime(), slot)
	finalTime := startTime.Add(delay)
	wait := timeutils.Until(finalTime)
	if wait <= 0 {
		return
	}

	t := time.NewTimer(wait)
	defer t.Stop()

	bChannel := make(chan *ethpb.SignedBeaconBlock, 1)
	sub := b.blockFeed.Subscribe(bChannel)
	defer sub.Unsubscribe()

	for {
		select {
		case b := <-bChannel:
			if featureconfig.Get().AttestTimely {
				if slot <= b.Block.Slot {
					return
				}
			}
		case <-ctx.Done():
			return
		case <-t.C:
			return
		}
	}
}
