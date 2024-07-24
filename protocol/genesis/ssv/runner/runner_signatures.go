package runner

import (
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	genesisspecssv "github.com/ssvlabs/ssv-spec-pre-cc/ssv"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"

	"github.com/ssvlabs/ssv/protocol/genesis/types"
)

func (b *BaseRunner) signBeaconObject(
	runner Runner,
	obj ssz.HashRoot,
	slot spec.Slot,
	domainType spec.DomainType,
) (*genesisspectypes.PartialSignatureMessage, error) {
	epoch := runner.GetBaseRunner().BeaconNetwork.EstimatedEpochAtSlot(slot)
	domain, err := runner.GetBeaconNode().DomainData(epoch, domainType)
	if err != nil {
		return nil, errors.Wrap(err, "could not get beacon domain")
	}
	sig, r, err := runner.GetSigner().SignBeaconObject(obj, domain, runner.GetBaseRunner().Share.SharePubKey, domainType)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign beacon object")
	}

	return &genesisspectypes.PartialSignatureMessage{
		PartialSignature: sig,
		SigningRoot:      r,
		Signer:           runner.GetBaseRunner().Share.OperatorID,
	}, nil
}

func (b *BaseRunner) signPostConsensusMsg(runner Runner, msg *genesisspectypes.PartialSignatureMessages) (*genesisspectypes.SignedPartialSignatureMessage, error) {
	signature, err := runner.GetSigner().SignRoot(msg, genesisspectypes.PartialSignatureType, b.Share.SharePubKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign PartialSignatureMessage for PostConsensusContainer")
	}

	return &genesisspectypes.SignedPartialSignatureMessage{
		Message:   *msg,
		Signature: signature,
		Signer:    b.Share.OperatorID,
	}, nil
}

// Validate message content without verifying signatures
func (b *BaseRunner) validatePartialSigMsgForSlot(
	signedMsg *genesisspectypes.SignedPartialSignatureMessage,
	slot spec.Slot,
) error {
	if err := signedMsg.Validate(); err != nil {
		return errors.Wrap(err, "SignedPartialSignatureMessage invalid")
	}

	if signedMsg.Message.Slot != slot {
		return errors.New("invalid partial sig slot")
	}

	// Check if signer is in committee
	signerInCommittee := false
	for _, operator := range b.Share.Committee {
		if operator.OperatorID == signedMsg.Signer {
			signerInCommittee = true
			break
		}
	}
	if !signerInCommittee {
		return errors.New("unknown signer")
	}

	return nil
}

func (b *BaseRunner) verifyBeaconPartialSignature(signer uint64, signature genesisspectypes.Signature, root [32]byte) error {
	types.MetricsSignaturesVerifications.WithLabelValues().Inc()

	for _, n := range b.Share.Committee {
		if n.GetID() == signer {
			pk, err := types.DeserializeBLSPublicKey(n.GetPublicKey())
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
func (b *BaseRunner) resolveDuplicateSignature(container *genesisspecssv.PartialSigContainer, msg *genesisspectypes.PartialSignatureMessage) {

	// Check previous signature validity
	previousSignature, err := container.GetSignature(msg.Signer, msg.SigningRoot)
	if err == nil {
		err = b.verifyBeaconPartialSignature(msg.Signer, previousSignature, msg.SigningRoot)
		if err == nil {
			// Keep the previous sigature since it's correct
			return
		}
	}

	// Previous signature is incorrect or doesn't exist
	container.Remove(msg.Signer, msg.SigningRoot)

	// Hold the new signature, if correct
	err = b.verifyBeaconPartialSignature(msg.Signer, msg.PartialSignature, msg.SigningRoot)
	if err == nil {
		container.AddSignature(msg)
	}
}
