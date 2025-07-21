package instance

import (
	"bytes"
	"context"

	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log/fields"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// uponPrepare process prepare message
// Assumes prepare message is valid!
func (i *Instance) uponPrepare(ctx context.Context, logger *zap.Logger, msg *specqbft.ProcessingMessage) error {
	hasQuorumBefore := specqbft.HasQuorum(i.State.CommitteeMember, i.State.PrepareContainer.MessagesForRound(i.State.Round))

	addedMsg, err := i.State.PrepareContainer.AddFirstMsgForSignerAndRound(msg)
	if err != nil {
		return errors.Wrap(err, "could not add prepare msg to container")
	}
	if !addedMsg {
		return nil // uponPrepare was already called
	}

	proposedRoot := i.State.ProposalAcceptedForCurrentRound.QBFTMessage.Root
	logger.Debug("📬 got prepare message",
		fields.Round(i.State.Round),
		zap.Any("prepare_signers", msg.SignedMessage.OperatorIDs),
		fields.Root(proposedRoot))

	if hasQuorumBefore {
		return nil // already moved to commit stage
	}

	if !specqbft.HasQuorum(i.State.CommitteeMember, i.State.PrepareContainer.MessagesForRound(i.State.Round)) {
		return nil // no quorum yet
	}

	i.State.LastPreparedValue = i.State.ProposalAcceptedForCurrentRound.SignedMessage.FullData
	i.State.LastPreparedRound = i.State.Round

	i.metrics.EndStage(ctx, i.State.Round, stagePrepare)

	logger.Debug("🎯 got prepare quorum",
		fields.Round(i.State.Round),
		zap.Any("prepare_signers", allSigners(i.State.PrepareContainer.MessagesForRound(i.State.Round))))

	commitMsg, err := i.CreateCommit(proposedRoot)
	if err != nil {
		return errors.Wrap(err, "could not create commit msg")
	}

	logger.Debug("📢 broadcasting commit message",
		fields.Round(i.State.Round),
		zap.Any("commit_signers", commitMsg.OperatorIDs),
		fields.Root(proposedRoot))

	if err := i.Broadcast(commitMsg); err != nil {
		return errors.Wrap(err, "failed to broadcast commit message")
	}

	return nil
}

// getRoundChangeJustification returns the round change justification for the current round.
// The justification is a quorum of signed prepare messages that agree on state.LastPreparedValue
func (i *Instance) getRoundChangeJustification() ([]*specqbft.ProcessingMessage, error) {
	if i.State.LastPreparedValue == nil {
		return nil, nil
	}

	r, err := specqbft.HashDataRoot(i.State.LastPreparedValue)
	if err != nil {
		return nil, errors.Wrap(err, "could not hash input data")
	}

	prepareMsgs := i.State.PrepareContainer.MessagesForRound(i.State.LastPreparedRound)
	ret := make([]*specqbft.ProcessingMessage, 0)
	for _, msg := range prepareMsgs {
		if err := i.validSignedPrepareForHeightRoundAndRootIgnoreSignature(
			msg,
			i.State.LastPreparedRound,
			r,
		); err == nil {
			ret = append(ret, msg)
		}
	}

	if !specqbft.HasQuorum(i.State.CommitteeMember, ret) {
		return nil, nil
	}

	return ret, nil
}

// validSignedPrepareForHeightRoundAndRootIgnoreSignature known in dafny spec as validSignedPrepareForHeightRoundAndDigest
// https://entethalliance.github.io/client-spec/qbft_spec.html#dfn-qbftspecification
func (i *Instance) validSignedPrepareForHeightRoundAndRootIgnoreSignature(
	msg *specqbft.ProcessingMessage,
	round specqbft.Round,
	root [32]byte,
) error {
	if msg.QBFTMessage.MsgType != specqbft.PrepareMsgType {
		return errors.New("prepare msg type is wrong")
	}
	if msg.QBFTMessage.Height != i.State.Height {
		return errors.New("wrong msg height")
	}
	if msg.QBFTMessage.Round != round {
		return errors.New("wrong msg round")
	}

	if err := msg.Validate(); err != nil {
		return errors.Wrap(err, "prepareData invalid")
	}

	if !bytes.Equal(msg.QBFTMessage.Root[:], root[:]) {
		return errors.New("proposed data mismatch")
	}

	if len(msg.SignedMessage.OperatorIDs) != 1 {
		return errors.New("msg allows 1 signer")
	}

	if !msg.SignedMessage.CheckSignersInCommittee(i.State.CommitteeMember.Committee) {
		return errors.New("signer not in committee")
	}

	return nil
}

func (i *Instance) validSignedPrepareForHeightRoundAndRootVerifySignature(
	msg *specqbft.ProcessingMessage,
	round specqbft.Round,
	root [32]byte,
) error {

	if err := i.validSignedPrepareForHeightRoundAndRootIgnoreSignature(msg, round, root); err != nil {
		return err
	}

	// Verify signature
	if err := spectypes.Verify(msg.SignedMessage, i.State.CommitteeMember.Committee); err != nil {
		return errors.Wrap(err, "msg signature invalid")
	}

	return nil
}

// CreatePrepare
/**
Prepare(
                    signPrepare(
                        UnsignedPrepare(
                            |current.blockchain|,
                            newRound,
                            digest(m.proposedBlock)),
                        current.id
                        )
                );
*/
func (i *Instance) CreatePrepare(newRound specqbft.Round, root [32]byte) (*spectypes.SignedSSVMessage, error) {
	msg := &specqbft.Message{
		MsgType:    specqbft.PrepareMsgType,
		Height:     i.State.Height,
		Round:      newRound,
		Identifier: i.State.ID,

		Root: root,
	}

	return ssvtypes.Sign(msg, i.State.CommitteeMember.OperatorID, i.signer)
}
