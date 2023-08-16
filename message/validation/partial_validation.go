package validation

// partial_validation.go contains methods for validating partial signature messages

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"

	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

func (mv *MessageValidator) validatePartialSignatureMessage(share *ssvtypes.SSVShare, msg *queue.DecodedSSVMessage) error {
	signedMsg, ok := msg.Body.(*spectypes.SignedPartialSignatureMessage)
	if !ok {
		return fmt.Errorf("expected partial signature message")
	}

	if len(msg.Data) > maxPartialSignatureMsgSize {
		return fmt.Errorf("size exceeded")
	}

	role := msg.GetID().GetRoleType()
	if !mv.partialSignatureTypeMatchesRole(signedMsg.Message.Type, role) {
		return fmt.Errorf("partial signature type and role don't match")
	}

	if err := mv.validPartialSigners(share, signedMsg); err != nil {
		return err
	}

	// TODO: check running duty

	consensusID := ConsensusID{
		PubKey: phase0.BLSPubKey(msg.GetID().GetPubKey()),
		Role:   role,
	}

	consensusState := mv.consensusState(consensusID)
	if err := mv.validateSlotState(consensusState.SignerState(signedMsg.Signer), signedMsg.Message.Slot); err != nil {
		return err
	}

	// TODO: do read-only behavior checks before checking signature and then update the state if signature is correct
	if err := mv.validPartialSignatures(share, signedMsg); err != nil {
		return err
	}

	return nil
}

func (mv *MessageValidator) partialSignatureTypeMatchesRole(msgType spectypes.PartialSigMsgType, role spectypes.BeaconRole) bool {
	switch role {
	case spectypes.BNRoleAttester:
		return msgType == spectypes.PostConsensusPartialSig
	case spectypes.BNRoleAggregator:
		return msgType == spectypes.PostConsensusPartialSig || msgType == spectypes.SelectionProofPartialSig
	case spectypes.BNRoleProposer:
		return msgType == spectypes.PostConsensusPartialSig || msgType == spectypes.RandaoPartialSig
	case spectypes.BNRoleSyncCommittee:
		return msgType == spectypes.PostConsensusPartialSig
	case spectypes.BNRoleSyncCommitteeContribution:
		return msgType == spectypes.PostConsensusPartialSig || msgType == spectypes.ContributionProofs
	case spectypes.BNRoleValidatorRegistration:
		return msgType == spectypes.ValidatorRegistrationPartialSig
	default:
		panic("unknown role")
	}
}

func (mv *MessageValidator) validPartialSignatures(share *ssvtypes.SSVShare, signedMsg *spectypes.SignedPartialSignatureMessage) error {
	if err := mv.validateSignatureFormat(signedMsg.Signature); err != nil {
		return err
	}

	if err := ssvtypes.VerifyByOperators(signedMsg.Signature, signedMsg, mv.netCfg.Domain, spectypes.PartialSignatureType, share.Committee); err != nil {
		return fmt.Errorf("invalid signature: %w", err)
	}

	seen := map[[32]byte]struct{}{}
	for _, message := range signedMsg.Message.Messages {
		if _, ok := seen[message.SigningRoot]; ok {
			return fmt.Errorf("duplicated partial signature message")
		}
		seen[message.SigningRoot] = struct{}{}

		if err := mv.validateSignatureFormat(message.PartialSignature); err != nil {
			return err
		}

		if err := mv.verifyPartialSignature(message, share); err != nil {
			return err
		}
	}

	return nil
}

func (mv *MessageValidator) verifyPartialSignature(msg *spectypes.PartialSignatureMessage, share *ssvtypes.SSVShare) error {
	signer := msg.Signer
	signature := msg.PartialSignature
	root := msg.SigningRoot

	for _, n := range share.Committee {
		if n.GetID() != signer {
			continue
		}

		pk, err := ssvtypes.DeserializeBLSPublicKey(n.GetPublicKey())
		if err != nil {
			return fmt.Errorf("deserialize pk: %w", err)
		}
		sig := &bls.Sign{}
		if err := sig.Deserialize(signature); err != nil {
			return fmt.Errorf("deserialize signature: %w", err)
		}

		// verify
		if !sig.VerifyByte(&pk, root[:]) {
			return fmt.Errorf("wrong signature")
		}
		return nil
	}

	return ErrSignerNotInCommittee
}

func (mv *MessageValidator) validPartialSigners(share *ssvtypes.SSVShare, m *spectypes.SignedPartialSignatureMessage) error {
	if err := mv.commonSignerValidation(m.Signer, share); err != nil {
		return err
	}

	for _, message := range m.Message.Messages {
		if message.Signer != m.Signer {
			err := ErrUnexpectedSigner
			err.want = m.Signer
			err.got = message.Signer
			return err
		}
		if err := mv.commonSignerValidation(message.Signer, share); err != nil {
			return err
		}
	}

	return nil
}
