package ssv

import (
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/pkg/errors"
)

func (dr *Runner) SignSlotWithSelectionProofPreConsensus(slot spec.Slot, signer types.KeyManager) (*PartialSignatureMessage, error) {
	sig, r, err := signer.SignSlotWithSelectionProof(slot, dr.Share.SharePubKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign partial selection proof")
	}

	// generate partial sig for randao
	msg := &PartialSignatureMessage{
		Slot:             slot,
		PartialSignature: sig,
		SigningRoot:      ensureRoot(r),
		Signers:          []types.OperatorID{dr.Share.OperatorID},
	}

	return msg, nil
}

// ProcessSelectionProofMessage process selection proof msg, returns true if it has quorum for partial signatures.
// returns true only once (first time quorum achieved)
func (dr *Runner) ProcessSelectionProofMessage(signedMsg *SignedPartialSignatureMessage) (bool, [][]byte, error) {
	if err := dr.canProcessSelectionProofMsg(signedMsg); err != nil {
		return false, nil, errors.Wrap(err, "can't process selection proof message")
	}

	roots := make([][]byte, 0)
	anyQuorum := false
	for _, msg := range signedMsg.Messages {
		prevQuorum := dr.State.SelectionProofPartialSig.HasQuorum(msg.SigningRoot)

		if err := dr.State.SelectionProofPartialSig.AddSignature(msg); err != nil {
			return false, nil, errors.Wrap(err, "could not add partial selection proof signature")
		}

		if prevQuorum {
			continue
		}

		quorum := dr.State.SelectionProofPartialSig.HasQuorum(msg.SigningRoot)
		if quorum {
			roots = append(roots, msg.SigningRoot)
			anyQuorum = true
		}
	}

	return anyQuorum, roots, nil
}

// canProcessRandaoMsg returns true if it can process selection proof message, false if not
func (dr *Runner) canProcessSelectionProofMsg(msg *SignedPartialSignatureMessage) error {
	return dr.validatePartialSigMsg(msg, dr.CurrentDuty.Slot)
}
