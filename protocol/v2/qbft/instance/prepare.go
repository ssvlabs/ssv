package instance

import (
	"bytes"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/protocol/v2/qbft"
)

// uponPrepare process prepare message
// Assumes prepare message is valid!
func (i *Instance) uponPrepare(logger *zap.Logger, signedPrepare *spectypes.SignedSSVMessage, prepareMsgContainer *specqbft.MsgContainer) error {
	hasQuorumBefore := specqbft.HasQuorum(i.State.Share, prepareMsgContainer.MessagesForRound(i.State.Round))

	addedMsg, err := prepareMsgContainer.AddFirstMsgForSignerAndRound(signedPrepare)
	if err != nil {
		return errors.Wrap(err, "could not add prepare msg to container")
	}
	if !addedMsg {
		return nil // uponPrepare was already called
	}

	logger.Debug("📬 got prepare message",
		fields.Round(i.State.Round),
		zap.Any("prepare-signers", signedPrepare.GetOperatorIDs()),
	)

	if hasQuorumBefore {
		return nil // already moved to commit stage
	}

	if !specqbft.HasQuorum(i.State.Share, prepareMsgContainer.MessagesForRound(i.State.Round)) {
		return nil // no quorum yet
	}

	proposalMsgAccepted, err := specqbft.DecodeMessage(i.State.ProposalAcceptedForCurrentRound.SSVMessage.Data)
	if err != nil {
		return err
	}

	proposedRoot := proposalMsgAccepted.Root

	i.State.LastPreparedValue = i.State.ProposalAcceptedForCurrentRound.FullData
	i.State.LastPreparedRound = i.State.Round

	i.metrics.EndStagePrepare()

	logger.Debug("🎯 got prepare quorum",
		fields.Round(i.State.Round),
		zap.Any("prepare-signers", allSigners(prepareMsgContainer.MessagesForRound(i.State.Round))),
		fields.Root(proposedRoot))

	commit, err := CreateCommit(i.State, i.config, proposedRoot)
	if err != nil {
		return errors.Wrap(err, "could not create commit msg")
	}

	commitMsg, err := specqbft.DecodeMessage(commit.SSVMessage.Data)
	if err != nil {
		return err
	}

	logger.Debug("📢 broadcasting commit message",
		fields.Round(i.State.Round),
		zap.Any("commit-singers", commit.GetOperatorIDs()),
		fields.Root(commitMsg.Root),
	)

	if err := i.Broadcast(commit); err != nil {
		return errors.Wrap(err, "failed to broadcast commit message")
	}

	return nil
}

// getRoundChangeJustification returns the round change justification for the current round.
// The justification is a quorum of signed prepare messages that agree on state.LastPreparedValue
func getRoundChangeJustification(state *specqbft.State, config qbft.IConfig, prepareMsgContainer *specqbft.MsgContainer) ([]*spectypes.SignedSSVMessage, error) {
	if state.LastPreparedValue == nil {
		return nil, nil
	}

	r, err := specqbft.HashDataRoot(state.LastPreparedValue)
	if err != nil {
		return nil, errors.Wrap(err, "could not hash input data")
	}

	prepareMsgs := prepareMsgContainer.MessagesForRound(state.LastPreparedRound)
	ret := make([]*spectypes.SignedSSVMessage, 0)
	for _, msg := range prepareMsgs {
		if err := validSignedPrepareForHeightRoundAndRoot(
			config,
			msg,
			state.Height,
			state.LastPreparedRound,
			r,
			state.Share.Committee,
		); err == nil {
			ret = append(ret, msg)
		}
	}

	if !specqbft.HasQuorum(state.Share, ret) {
		return nil, nil
	}

	return ret, nil
}

// validPreparesForHeightRoundAndValue returns an aggregated prepare msg for a specific Height and round
// func validPreparesForHeightRoundAndValue(
//	config IConfig,
//	prepareMessages []*SignedMessage,
//	height Height,
//	round Round,
//	value []byte,
//	operators []*types.Operator) *SignedMessage {
//	var aggregatedPrepareMsg *SignedMessage
//	for _, signedMsg := range prepareMessages {
//		if err := validSignedPrepareForHeightRoundAndValue(config, signedMsg, height, round, value, operators); err == nil {
//			if aggregatedPrepareMsg == nil {
//				aggregatedPrepareMsg = signedMsg
//			} else {
//				// TODO: check error
//				// nolint
//				aggregatedPrepareMsg.Aggregate(signedMsg)
//			}
//		}
//	}
//	return aggregatedPrepareMsg
// }

// validSignedPrepareForHeightRoundAndValue known in dafny spec as validSignedPrepareForHeightRoundAndDigest
// https://entethalliance.github.io/client-spec/qbft_spec.html#dfn-qbftspecification
func validSignedPrepareForHeightRoundAndRoot(
	config qbft.IConfig,
	signedPrepare *spectypes.SignedSSVMessage,
	height specqbft.Height,
	round specqbft.Round,
	root [32]byte,
	operators []*spectypes.CommitteeMember) error {
	msg, err := specqbft.DecodeMessage(signedPrepare.SSVMessage.Data)
	if err != nil {
		return err
	}

	if msg.MsgType != specqbft.PrepareMsgType {
		return errors.New("prepare msg type is wrong")
	}
	if msg.Height != height {
		return errors.New("wrong msg height")
	}
	if msg.Round != round {
		return errors.New("wrong msg round")
	}

	if err := signedPrepare.Validate(); err != nil {
		return errors.Wrap(err, "prepareData invalid")
	}

	if !bytes.Equal(msg.Root[:], root[:]) {
		return errors.New("proposed data mistmatch")
	}

	if len(signedPrepare.GetOperatorIDs()) != 1 {
		return errors.New("msg allows 1 signer")
	}

	if config.VerifySignatures() {
		if err := config.GetSignatureVerifier().Verify(signedPrepare, operators); err != nil {
			return errors.Wrap(err, "msg signature invalid")
		}
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
func CreatePrepare(state *specqbft.State, config qbft.IConfig, newRound specqbft.Round, root [32]byte) (*spectypes.SignedSSVMessage, error) {
	msg := &specqbft.Message{
		MsgType:    specqbft.PrepareMsgType,
		Height:     state.Height,
		Round:      newRound,
		Identifier: state.ID,

		Root: root,
	}

	return specqbft.MessageToSignedSSVMessage(msg, state.Share.OperatorID, config.GetOperatorSigner())
}
