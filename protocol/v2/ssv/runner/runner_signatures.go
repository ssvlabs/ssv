package runner

import (
	"fmt"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/types"
)

func (b *BaseRunner) signBeaconObject(
	runner Runner,
	duty *spectypes.BeaconDuty,
	obj ssz.HashRoot,
	slot spec.Slot,
	domainType spec.DomainType,
) (*spectypes.PartialSignatureMessage, error) {
	epoch := runner.GetBaseRunner().BeaconNetwork.EstimatedEpochAtSlot(slot)
	domain, err := runner.GetBeaconNode().DomainData(epoch, domainType)
	if err != nil {
		return nil, errors.Wrap(err, "could not get beacon domain")
	}
	if _, ok := runner.GetBaseRunner().Share[duty.ValidatorIndex]; !ok {
		return nil, fmt.Errorf("unknown validator index %d", duty.ValidatorIndex)
	}
	sig, r, err := runner.GetSigner().SignBeaconObject(obj, domain, runner.GetBaseRunner().Share[duty.ValidatorIndex].SharePubKey, domainType)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign beacon object")
	}

	return &spectypes.PartialSignatureMessage{
		PartialSignature: sig,
		SigningRoot:      r,
		Signer:           runner.GetOperatorSigner().GetOperatorID(),
		ValidatorIndex:   duty.ValidatorIndex,
	}, nil
}

//func (b *BaseRunner) signPostConsensusMsg(runner Runner, msg *spectypes.PartialSignatureMessages) (*spectypes.SignedPartialSignatureMessage, error) {
//	signature, err := runner.GetSigner().SignBeaconObject(msg, spectypes.PartialSignatureType, b.Share.SharePubKey)
//	if err != nil {
//		return nil, errors.Wrap(err, "could not sign PartialSignatureMessage for PostConsensusContainer")
//	}
//
//	return &spectypes.SignedPartialSignatureMessage{
//		Message:   *msg,
//		Signature: signature,
//		Signer:    b.Share.OperatorID,
//	}, nil
//}

// Validate message content without verifying signatures
func (b *BaseRunner) validatePartialSigMsgForSlot(
	signedMsg *spectypes.PartialSignatureMessages,
	slot spec.Slot,
) error {
	if err := signedMsg.Validate(); err != nil {
		return errors.Wrap(err, "SignedPartialSignatureMessage invalid")
	}

	if signedMsg.Slot != slot {
		return errors.New("invalid partial sig slot")
	}

	for _, msg := range signedMsg.Messages {

		// Check if knows it has the validator index share
		validatorShare, ok := b.Share[msg.ValidatorIndex]
		if !ok {
			return errors.New("unknown validator index")
		}

		// Check if signer is in committee
		signerInCommittee := false
		for _, operator := range validatorShare.Committee {
			if operator.Signer == msg.Signer {
				signerInCommittee = true
				break
			}
		}
		if !signerInCommittee {
			return errors.New("unknown signer")
		}
	}
	return nil
}

func (b *BaseRunner) verifyBeaconPartialSignature(signer spectypes.OperatorID, signature spectypes.Signature, root [32]byte,
	committee []*spectypes.ShareMember) error {
	types.MetricsSignaturesVerifications.WithLabelValues().Inc()

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
func (b *BaseRunner) resolveDuplicateSignature(container *types.PartialSigContainer, msg *spectypes.PartialSignatureMessage) {

	// Check previous signature validity
	previousSignature, err := container.GetSignature(msg.ValidatorIndex, msg.Signer, msg.SigningRoot)
	if err == nil {
		err = b.verifyBeaconPartialSignature(msg.Signer, previousSignature, msg.SigningRoot,
			b.Share[msg.ValidatorIndex].Committee)
		if err == nil {
			// Keep the previous sigature since it's correct
			return
		}
	}

	// Previous signature is incorrect or doesn't exist
	container.Remove(msg.ValidatorIndex, msg.Signer, msg.SigningRoot)

	// Hold the new signature, if correct
	err = b.verifyBeaconPartialSignature(msg.Signer, msg.PartialSignature, msg.SigningRoot,
		b.Share[msg.ValidatorIndex].Committee)
	if err == nil {
		container.AddSignature(msg)
	}
}
