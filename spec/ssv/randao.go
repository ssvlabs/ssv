package ssv

import (
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/pkg/errors"
)

func (dr *Runner) SignRandaoPreConsensus(epoch spec.Epoch, slot spec.Slot, signer types.KeyManager) (*PartialSignatureMessage, error) {
	sig, r, err := signer.SignRandaoReveal(epoch, dr.Share.SharePubKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign partial randao reveal")
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

// ProcessRandaoMessage process randao msg, returns true if it has quorum for partial signatures.
// returns true only once (first time quorum achieved)
func (dr *Runner) ProcessRandaoMessage(signedMsg *SignedPartialSignatureMessage) (bool, [][]byte, error) {
	if err := dr.canProcessRandaoMsg(signedMsg); err != nil {
		return false, nil, errors.Wrap(err, "can't process randao message")
	}

	roots := make([][]byte, 0)
	anyQuorum := false
	for _, msg := range signedMsg.Messages {
		prevQuorum := dr.State.RandaoPartialSig.HasQuorum(msg.SigningRoot)

		if err := dr.State.RandaoPartialSig.AddSignature(msg); err != nil {
			return false, nil, errors.Wrap(err, "could not add partial randao signature")
		}

		if prevQuorum {
			continue
		}

		quorum := dr.State.RandaoPartialSig.HasQuorum(msg.SigningRoot)
		if quorum {
			roots = append(roots, msg.SigningRoot)
			anyQuorum = true
		}
	}

	return anyQuorum, roots, nil
}

// canProcessRandaoMsg returns true if it can process randao message, false if not
func (dr *Runner) canProcessRandaoMsg(msg *SignedPartialSignatureMessage) error {
	return dr.validatePartialSigMsg(msg, dr.CurrentDuty.Slot)
}
