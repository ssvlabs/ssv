package runner

import (
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	genesisspectypes "github.com/bloxapp/ssv-spec-genesis/types"
	spectypes "github.com/bloxapp/ssv-spec/types"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	ssz "github.com/ferranbt/fastssz"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/protocol/v2/types"
)

func (b *BaseRunner) signBeaconObject(
	runner Runner,
	duty *spectypes.BeaconDuty,
	obj ssz.HashRoot,
	slot spec.Slot,
	domainType spec.DomainType,
) (types.PartialSignatureMessage, error) {
	epoch := runner.GetBaseRunner().BeaconNetwork.EstimatedEpochAtSlot(slot)
	domain, err := runner.GetBeaconNode().DomainData(epoch, domainType)
	if err != nil {
		return nil, errors.Wrap(err, "could not get beacon domain")
	}
	sig, r, err := runner.GetSigner().SignBeaconObject(obj, domain, runner.GetBaseRunner().Shares[duty.ValidatorIndex].SharePubKey, domainType)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign beacon object")
	}

	if true {
		return &genesisspectypes.PartialSignatureMessage{
			PartialSignature: sig,
			SigningRoot:      r,
			Signer:           runner.GetOperatorSigner().GetOperatorID(),
		}, nil

	} else {
		return &spectypes.PartialSignatureMessage{
			PartialSignature: sig,
			SigningRoot:      r,
			Signer:           runner.GetOperatorSigner().GetOperatorID(),
			ValidatorIndex:   duty.ValidatorIndex,
		}, nil
	}
}

func (b *BaseRunner) signPostConsensusMsg(runner Runner, msg *genesisspectypes.PartialSignatureMessages) (*genesisspectypes.SignedPartialSignatureMessage, error) {
	signature, err := runner.GetGenesisSigner().SignRoot(msg, spectypes.PartialSignatureType, b.Share.SharePubKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign PartialSignatureMessage for PostConsensusContainer")
	}

	return &genesisspectypes.SignedPartialSignatureMessage{
		Message:   *msg,
		Signature: signature,
		Signer:    b.Share.OperatorID,
	}, nil
}

func (b *BaseRunner) signBeaconObject(runner Runner, duty *spectypes.BeaconDuty,
	obj ssz.HashRoot, slot spec.Slot, domainType spec.DomainType) (*types.PartialSignatureMessage, error) {
	epoch := runner.GetBaseRunner().BeaconNetwork.EstimatedEpochAtSlot(slot)
	domain, err := runner.GetBeaconNode().DomainData(epoch, domainType)
	if err != nil {
		return nil, errors.Wrap(err, "could not get beacon domain")
	}

	sig, r, err := runner.GetSigner().SignBeaconObject(obj, domain,
		runner.GetBaseRunner().Shares[duty.ValidatorIndex].SharePubKey,
		domainType)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign beacon object")
	}

	return &types.PartialSignatureMessage{
		PartialSignature: sig,
		SigningRoot:      r,
		Signer:           runner.GetOperatorSigner().GetOperatorID(),
		ValidatorIndex:   duty.ValidatorIndex,
	}, nil
}

// Validate message content without verifying signatures
func (b *BaseRunner) validatePartialSigMsgForSlot(
	signedMsg types.PartialSignatureMessages,
	slot spec.Slot,
) error {
	if err := signedMsg.Validate(); err != nil {
		return errors.Wrap(err, "SignedPartialSignatureMessage invalid")
	}

	if signedMsg.GetSlot() != slot {
		return errors.New("invalid partial sig slot")
	}

	signerInCommittee := false
	for _, msg := range signedMsg.GetMessages() {
		commitee := b.getCommittee(msg.GetValidatorIndex())
		if commitee == nil {
			return errors.New("unknown signer")
		}
		for _, operator := range commitee {
			if operator.Signer == signedMsg.GetSigner() {
				signerInCommittee = true
				break
			}
		}
	}
	if !signerInCommittee {
		return errors.New("unknown signer")
	}

	return nil
}

func (b *BaseRunner) verifyBeaconPartialSignature(signer uint64, signature spectypes.Signature, root [32]byte, committee []*spectypes.ShareMember) error {
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
func (b *BaseRunner) resolveDuplicateSignature(container ssvtypes.PartialSigContainer, msg ssvtypes.PartialSignatureMessage) {
	// Check previous signature validity
	previousSignature, err := container.GetSignature(msg)

	if err == nil {
		err = b.verifyBeaconPartialSignature(msg.GetSigner(), previousSignature, msg.GetSigningRoot(), b.getCommittee(msg.GetValidatorIndex()))
		if err == nil {
			// Keep the previous sigature since it's correct
			return
		}
	}

	// Previous signature is incorrect or doesn't exist
	container.Remove(msg.GetValidatorIndex(), msg.GetSigner(), msg.GetSigningRoot())

	// Hold the new signature, if correct
	err = b.verifyBeaconPartialSignature(msg.GetSigner(), msg.GetPartialSignature(), msg.GetSigningRoot(), b.getCommittee(msg.GetValidatorIndex()))
	if err == nil {
		container.AddSignature(msg)
	}
}
