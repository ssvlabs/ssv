package runner

import (
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	specssv "github.com/bloxapp/ssv-spec/ssv"
	"github.com/bloxapp/ssv-spec/types"
	ssz "github.com/ferranbt/fastssz"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
)

func (b *BaseRunner) signBeaconObject(
	runner Runner,
	obj ssz.HashRoot,
	slot spec.Slot,
	domainType spec.DomainType,
) (*specssv.PartialSignatureMessage, error) {
	epoch := runner.GetBaseRunner().BeaconNetwork.EstimatedEpochAtSlot(slot)
	domain, err := runner.GetBeaconNode().DomainData(epoch, domainType)
	if err != nil {
		return nil, errors.Wrap(err, "could not get beacon domain")
	}

	sig, r, err := runner.GetSigner().SignBeaconObject(obj, domain, runner.GetBaseRunner().Share.SharePubKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign beacon object")
	}

	return &specssv.PartialSignatureMessage{
		PartialSignature: sig,
		SigningRoot:      r,
		Signer:           runner.GetBaseRunner().Share.OperatorID,
	}, nil
}

func (b *BaseRunner) signPostConsensusMsg(runner Runner, msg *specssv.PartialSignatureMessages) (*specssv.SignedPartialSignatureMessage, error) {
	signature, err := runner.GetSigner().SignRoot(msg, types.PartialSignatureType, b.Share.SharePubKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign PartialSignatureMessage for PostConsensusContainer")
	}

	return &specssv.SignedPartialSignatureMessage{
		Message:   *msg,
		Signature: signature,
		Signer:    b.Share.OperatorID,
	}, nil
}

func (b *BaseRunner) validatePartialSigMsg(
	signedMsg *specssv.SignedPartialSignatureMessage,
	slot spec.Slot,
) error {
	if err := signedMsg.Validate(); err != nil {
		return errors.Wrap(err, "SignedPartialSignatureMessage invalid")
	}

	if err := signedMsg.GetSignature().VerifyByOperators(signedMsg, b.Share.DomainType, types.PartialSignatureType, b.Share.Committee); err != nil {
		return errors.Wrap(err, "failed to verify PartialSignature")
	}

	for _, msg := range signedMsg.Message.Messages {
		if err := b.verifyBeaconPartialSignature(msg); err != nil {
			return errors.Wrap(err, "could not verify Beacon partial Signature")
		}
	}

	return nil
}

func (b *BaseRunner) verifyBeaconPartialSignature(msg *specssv.PartialSignatureMessage) error {
	signer := msg.Signer
	signature := msg.PartialSignature
	root := msg.SigningRoot

	for _, n := range b.Share.Committee {
		if n.GetID() == signer {
			pk := &bls.PublicKey{}
			if err := pk.Deserialize(n.GetPublicKey()); err != nil {
				return errors.Wrap(err, "could not deserialized pk")
			}
			sig := &bls.Sign{}
			if err := sig.Deserialize(signature); err != nil {
				return errors.Wrap(err, "could not deserialized Signature")
			}

			// verify
			if !sig.VerifyByte(pk, root) {
				return errors.New("wrong signature")
			}
			return nil
		}
	}
	return errors.New("unknown signer")
}
