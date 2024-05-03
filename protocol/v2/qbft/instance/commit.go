package instance

import (
	"bytes"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/exp/slices"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/protocol/v2/qbft"
)

// UponCommit returns true if a quorum of commit messages was received.
// Assumes commit message is valid!
func (i *Instance) UponCommit(logger *zap.Logger, signedCommit *spectypes.SignedSSVMessage, commitMsgContainer *specqbft.MsgContainer) (bool, []byte, *spectypes.SignedSSVMessage, error) {
	// Decode qbft message
	msg, err := specqbft.DecodeMessage(signedCommit.SSVMessage.Data)
	if err != nil {
		return false, nil, nil, err
	}

	addMsg, err := commitMsgContainer.AddFirstMsgForSignerAndRound(signedCommit)
	if err != nil {
		return false, nil, nil, errors.Wrap(err, "could not add commit msg to container")
	}
	if !addMsg {
		return false, nil, nil, nil // UponCommit was already called
	}

	logger.Debug("📬 got commit message",
		fields.Round(i.State.Round),
		zap.Any("commit-signers", signedCommit.GetOperatorIDs()),
		fields.Root(msg.Root))

	// calculate commit quorum and act upon it
	quorum, commitMsgs, err := commitQuorumForRoundRoot(i.State, commitMsgContainer, msg.Root, msg.Round)
	if err != nil {
		return false, nil, nil, errors.Wrap(err, "could not calculate commit quorum")
	}

	if quorum {
		fullData := i.State.ProposalAcceptedForCurrentRound.FullData /* must have value there, checked on validateCommit */

		agg, err := aggregateCommitMsgs(commitMsgs, fullData)
		if err != nil {
			return false, nil, nil, errors.Wrap(err, "could not aggregate commit msgs")
		}

		logger.Debug("🎯 got commit quorum",
			fields.Round(i.State.Round),
			zap.Any("agg-signers", signedCommit.GetOperatorIDs()),
			fields.Root(msg.Root))

		i.metrics.EndStageCommit()

		return true, fullData, agg, nil
	}

	return false, nil, nil, nil
}

// returns true if there is a quorum for the current round for this provided value
func commitQuorumForRoundRoot(state *specqbft.State, commitMsgContainer *specqbft.MsgContainer, root [32]byte, round specqbft.Round) (bool, []*spectypes.SignedSSVMessage, error) {
	signers, msgs := commitMsgContainer.LongestUniqueSignersForRoundAndRoot(round, root)
	return state.Share.HasQuorum(len(signers)), msgs, nil
}

func aggregateCommitMsgs(msgs []*spectypes.SignedSSVMessage, fullData []byte) (*spectypes.SignedSSVMessage, error) {
	if len(msgs) == 0 {
		return nil, errors.New("can't aggregate zero commit msgs")
	}

	var ret *spectypes.SignedSSVMessage
	for _, m := range msgs {
		if ret == nil {
			ret = m.DeepCopy()
		} else {
			if err := ret.Aggregate(m); err != nil {
				return nil, errors.Wrap(err, "could not aggregate commit msg")
			}
		}
	}
	ret.FullData = fullData

	if ret != nil {
		slices.Sort(ret.OperatorIDs)
	}

	return ret, nil
}

// CreateCommit
/**
Commit(
                    signCommit(
                        UnsignedCommit(
                            |current.blockchain|,
                            current.round,
                            signHash(hashBlockForCommitSeal(proposedBlock), current.id),
                            digest(proposedBlock)),
                            current.id
                        )
                    );
*/
func CreateCommit(state *specqbft.State, config qbft.IConfig, root [32]byte) (*spectypes.SignedSSVMessage, error) {
	msg := &specqbft.Message{
		MsgType:    specqbft.CommitMsgType,
		Height:     state.Height,
		Round:      state.Round,
		Identifier: state.ID,

		Root: root,
	}
	return specqbft.MessageToSignedSSVMessage(msg, state.Share.OperatorID, config.GetOperatorSigner())
}

func BaseCommitValidation(
	config qbft.IConfig,
	signedCommit *spectypes.SignedSSVMessage,
	height specqbft.Height,
	operators []*spectypes.CommitteeMember,
) error {
	if err := signedCommit.Validate(); err != nil {
		return errors.Wrap(err, "signed commit invalid")
	}

	msg, err := specqbft.DecodeMessage(signedCommit.SSVMessage.Data)
	if err != nil {
		return err
	}

	if msg.MsgType != specqbft.CommitMsgType {
		return errors.New("commit msg type is wrong")
	}
	if msg.Height != height {
		return errors.New("wrong msg height")
	}

	if !signedCommit.CheckSignersInCommittee(operators) {
		return errors.New("signer not in committee")
	}

	if config.VerifySignatures() {
		if err := config.GetSignatureVerifier().Verify(signedCommit, operators); err != nil {
			return errors.Wrap(err, "msg signature invalid")
		}
	}

	return nil
}

func validateCommit(
	config qbft.IConfig,
	signedCommit *spectypes.SignedSSVMessage,
	height specqbft.Height,
	round specqbft.Round,
	proposedSignedMsg *spectypes.SignedSSVMessage,
	operators []*spectypes.CommitteeMember,
) error {
	if err := BaseCommitValidation(config, signedCommit, height, operators); err != nil {
		return err
	}

	if len(signedCommit.GetOperatorIDs()) != 1 {
		return errors.New("msg allows 1 signer")
	}

	msg, err := specqbft.DecodeMessage(signedCommit.SSVMessage.Data)
	if err != nil {
		return err
	}

	if msg.Round != round {
		return errors.New("wrong msg round")
	}

	proposedMsg, err := specqbft.DecodeMessage(proposedSignedMsg.SSVMessage.Data)
	if err != nil {
		return err
	}

	if !bytes.Equal(proposedMsg.Root[:], msg.Root[:]) {
		return errors.New("proposed data mismatch")
	}

	return nil
}
