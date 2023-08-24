package validation

// partial_validation.go contains methods for validating partial signature messages

import (
	"encoding/hex"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
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

	if !mv.validPartialSigMsgType(signedMsg.Message.Type) {
		e := ErrUnknownMessageType
		e.got = signedMsg.Message.Type
		return e
	}

	role := msg.GetID().GetRoleType()
	if !mv.partialSignatureTypeMatchesRole(signedMsg.Message.Type, role) {
		return ErrPartialSignatureTypeRoleMismatch
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
	signerState := consensusState.SignerState(signedMsg.Signer)

	if signedMsg.Message.Slot < signerState.Slot {
		// Signers aren't allowed to decrease their slot.
		// If they've sent a future message due to clock error,
		// this should be caught by the earlyMessage check.
		err := ErrSlotAlreadyAdvanced
		err.want = signerState.Slot
		err.got = signedMsg.Message.Slot
		return err
	}

	if err := mv.validPartialSignatures(share, signedMsg); err != nil {
		return err
	}

	// Advance slot & round, if needed.
	if signedMsg.Message.Slot > signerState.Slot {
		signerState.Reset(signedMsg.Message.Slot, specqbft.FirstRound)
	}

	return nil
}

func (mv *MessageValidator) validPartialSigMsgType(msgType spectypes.PartialSigMsgType) bool {
	switch msgType {
	case spectypes.PostConsensusPartialSig,
		spectypes.RandaoPartialSig,
		spectypes.SelectionProofPartialSig,
		spectypes.ContributionProofs,
		spectypes.ValidatorRegistrationPartialSig:
		return true
	default:
		return false
	}
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
		panic("invalid role") // role validity should be checked before
	}
}

func (mv *MessageValidator) validPartialSignatures(share *ssvtypes.SSVShare, signedMsg *spectypes.SignedPartialSignatureMessage) error {
	if err := mv.validateSignatureFormat(signedMsg.Signature); err != nil {
		return err
	}

	if err := ssvtypes.VerifyByOperators(signedMsg.Signature, signedMsg, mv.netCfg.Domain, spectypes.PartialSignatureType, share.Committee); err != nil {
		signErr := ErrInvalidSignature
		signErr.innerErr = err
		signErr.got = fmt.Sprintf("domain %v from %v", hex.EncodeToString(mv.netCfg.Domain[:]), hex.EncodeToString(share.ValidatorPubKey))
		return signErr
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
