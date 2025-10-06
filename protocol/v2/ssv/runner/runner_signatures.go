package runner

import (
	"context"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

func signBeaconObject(
	ctx context.Context,
	runner Runner,
	duty *spectypes.ValidatorDuty,
	obj ssz.HashRoot,
	slot phase0.Slot,
	signatureDomain phase0.DomainType,
) (*spectypes.PartialSignatureMessage, error) {
	epoch := runner.GetNetworkConfig().EstimatedEpochAtSlot(slot)
	domain, err := runner.GetBeaconNode().DomainData(ctx, epoch, signatureDomain)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch beacon domain: %w", err)
	}
	return signAsValidator(ctx, runner, duty.ValidatorIndex, obj, slot, signatureDomain, domain)
}

func signAsValidator(
	ctx context.Context,
	runner Runner,
	validatorIndex phase0.ValidatorIndex,
	obj ssz.HashRoot,
	slot phase0.Slot,
	signatureDomain phase0.DomainType,
	domain phase0.Domain,
) (*spectypes.PartialSignatureMessage, error) {
	share, ok := runner.GetShares()[validatorIndex]
	if !ok {
		return nil, fmt.Errorf("unknown validator index %d", validatorIndex)
	}
	sig, r, err := runner.GetSigner().SignBeaconObject(
		ctx,
		obj,
		domain,
		phase0.BLSPubKey(share.SharePubKey),
		slot,
		signatureDomain,
	)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign beacon object")
	}

	return &spectypes.PartialSignatureMessage{
		PartialSignature: sig,
		SigningRoot:      r,
		Signer:           runner.GetOperatorSigner().GetOperatorID(),
		ValidatorIndex:   validatorIndex,
	}, nil
}

// Validate message content without verifying signatures and slot.
func (b *BaseRunner) validatePartialSigMsg(
	psigMsgs *spectypes.PartialSignatureMessages,
	expectedSlot phase0.Slot,
) error {
	if err := psigMsgs.Validate(); err != nil {
		return errors.Wrap(err, "PartialSignatureMessages invalid")
	}

	if psigMsgs.Slot < expectedSlot {
		// This message is targeting a slot that's already passed - our runner cannot process it anymore
		return fmt.Errorf("invalid partial sig slot: %d, expected slot: %d", psigMsgs.Slot, expectedSlot)
	}
	if psigMsgs.Slot > expectedSlot {
		return NewRetryableError(fmt.Errorf(
			"%w, message slot: %d, expected slot: %d",
			ErrFuturePartialSigMsg,
			psigMsgs.Slot,
			expectedSlot,
		))
	}

	// Get signer, it is the same in all psigMsgs.Messages and len(psigMsgs.Messages) > 0 (guaranteed by psigMsgs.Validate()).
	msgSigner := psigMsgs.Messages[0].Signer

	// Get committee (unique for runner)
	var shareSample *spectypes.Share
	for _, share := range b.Share {
		shareSample = share
		break
	}
	if shareSample == nil {
		return errors.New("can not get committee because there is not a single Share in runner")
	}
	committee := shareSample.Committee

	// Check if signer is in committee
	signerInCommittee := false
	for _, operator := range committee {
		if operator.Signer == msgSigner {
			signerInCommittee = true
			break
		}
	}
	if !signerInCommittee {
		return errors.New("unknown signer")
	}

	return nil
}

// Validate if runner has a share for each ValidatorIndex in the PartialSignatureMessages.Messages
func (b *BaseRunner) validateValidatorIndexInPartialSigMsg(
	psigMsgs *spectypes.PartialSignatureMessages,
) error {
	for _, msg := range psigMsgs.Messages {
		// Check if it has the validator index share
		_, ok := b.Share[msg.ValidatorIndex]
		if !ok {
			return errors.New("unknown validator index")
		}
	}
	return nil
}

func (b *BaseRunner) verifyBeaconPartialSignature(signer spectypes.OperatorID, signature spectypes.Signature, root [32]byte,
	committee []*spectypes.ShareMember) error {
	for _, n := range committee {
		if n.Signer == signer {
			pk, err := types.DeserializeBLSPublicKey(n.SharePubKey)
			if err != nil {
				return errors.Wrap(err, "could not deserialized pk")
			}
			sig := &bls.Sign{}
			if err := sig.Deserialize(signature); err != nil {
				return errors.Wrap(err, "could not deserialized Signature")
			}

			// verify
			if !sig.VerifyByte(&pk, root[:]) {
				return errors.New("wrong signature")
			}
			return nil
		}
	}
	return errors.New("unknown signer")
}

// Stores the container's existing signature or the new one, depending on their validity. If both are invalid, remove the existing one
func (b *BaseRunner) resolveDuplicateSignature(container *ssv.PartialSigContainer, msg *spectypes.PartialSignatureMessage) {
	// Check previous signature validity
	previousSignature, err := container.GetSignature(msg.ValidatorIndex, msg.Signer, msg.SigningRoot)
	if err == nil {
		err = b.verifyBeaconPartialSignature(
			msg.Signer,
			previousSignature,
			msg.SigningRoot,
			b.Share[msg.ValidatorIndex].Committee,
		)
		if err == nil {
			// Keep the previous signature since it's correct
			return
		}
	}

	// Previous signature is incorrect or doesn't exist
	container.Remove(msg.ValidatorIndex, msg.Signer, msg.SigningRoot)

	// Hold the new signature, if correct
	err = b.verifyBeaconPartialSignature(
		msg.Signer,
		msg.PartialSignature,
		msg.SigningRoot,
		b.Share[msg.ValidatorIndex].Committee,
	)
	if err == nil {
		container.AddSignature(msg)
	}
}
