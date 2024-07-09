package instance

import (
	"bytes"
	"sort"

	"github.com/pkg/errors"
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/genesis/qbft"
	"github.com/ssvlabs/ssv/protocol/genesis/types"
)

// UponCommit returns true if a quorum of commit messages was received.
// Assumes commit message is valid!
func (i *Instance) UponCommit(logger *zap.Logger, signedCommit *genesisspecqbft.SignedMessage, commitMsgContainer *genesisspecqbft.MsgContainer) (bool, []byte, *genesisspecqbft.SignedMessage, error) {
	addMsg, err := commitMsgContainer.AddFirstMsgForSignerAndRound(signedCommit)
	if err != nil {
		return false, nil, nil, errors.Wrap(err, "could not add commit msg to container")
	}
	if !addMsg {
		return false, nil, nil, nil // UponCommit was already called
	}

	logger.Debug("📬 got commit message",
		fields.Round(specqbft.Round(i.State.Round)),
		zap.Any("commit-signers", signedCommit.Signers),
		fields.Root(signedCommit.Message.Root))

	// calculate commit quorum and act upon it
	quorum, commitMsgs, err := commitQuorumForRoundRoot(i.State, commitMsgContainer, signedCommit.Message.Root, signedCommit.Message.Round)
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
			fields.Round(specqbft.Round(i.State.Round)),
			zap.Any("agg-signers", agg.Signers),
			fields.Root(signedCommit.Message.Root))

		i.metrics.EndStageCommit()

		return true, fullData, agg, nil
	}

	return false, nil, nil, nil
}

// returns true if there is a quorum for the current round for this provided value
func commitQuorumForRoundRoot(state *types.State, commitMsgContainer *genesisspecqbft.MsgContainer, root [32]byte, round genesisspecqbft.Round) (bool, []*genesisspecqbft.SignedMessage, error) {
	signers, msgs := commitMsgContainer.LongestUniqueSignersForRoundAndRoot(round, root)
	return state.CommitteeMember.HasQuorum(len(signers)), msgs, nil
}

func aggregateCommitMsgs(msgs []*genesisspecqbft.SignedMessage, fullData []byte) (*genesisspecqbft.SignedMessage, error) {
	if len(msgs) == 0 {
		return nil, errors.New("can't aggregate zero commit msgs")
	}

	var ret *genesisspecqbft.SignedMessage
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

	// TODO: REWRITE THIS!
	sort.Slice(ret.Signers, func(i, j int) bool {
		return ret.Signers[i] < ret.Signers[j]
	})

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
func CreateCommit(state *types.State, config qbft.IConfig, root [32]byte) (*genesisspecqbft.SignedMessage, error) {
	msg := &genesisspecqbft.Message{
		MsgType:    genesisspecqbft.CommitMsgType,
		Height:     state.Height,
		Round:      state.Round,
		Identifier: state.ID,

		Root: root,
	}
	sig, err := config.GetSigner().SignRoot(msg, genesisspectypes.QBFTSignatureType, state.CommitteeMember.SSVOperatorPubKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed signing commit msg")
	}

	signedMsg := &genesisspecqbft.SignedMessage{
		Signature: sig,
		Signers:   []genesisspectypes.OperatorID{config.GetOperatorID()},
		Message:   *msg,
	}
	return signedMsg, nil
}

func BaseCommitValidation(
	config qbft.IConfig,
	signedCommit *genesisspecqbft.SignedMessage,
	height genesisspecqbft.Height,
	operators []*spectypes.Operator,
) error {
	if signedCommit.Message.MsgType != genesisspecqbft.CommitMsgType {
		return errors.New("commit msg type is wrong")
	}
	if signedCommit.Message.Height != height {
		return errors.New("wrong msg height")
	}

	if err := signedCommit.Validate(); err != nil {
		return errors.Wrap(err, "signed commit invalid")
	}

	if config.VerifySignatures() {
		if err := types.VerifyByOperators(signedCommit.Signature, signedCommit, config.GetSignatureDomainType(), genesisspectypes.QBFTSignatureType, operators); err != nil {
			return errors.Wrap(err, "msg signature invalid")
		}
	}

	return nil
}

func validateCommit(
	config qbft.IConfig,
	signedCommit *genesisspecqbft.SignedMessage,
	height genesisspecqbft.Height,
	round genesisspecqbft.Round,
	proposedMsg *genesisspecqbft.SignedMessage,
	operators []*spectypes.Operator,
) error {
	if err := BaseCommitValidation(config, signedCommit, height, operators); err != nil {
		return err
	}

	if len(signedCommit.Signers) != 1 {
		return errors.New("msg allows 1 signer")
	}

	if signedCommit.Message.Round != round {
		return errors.New("wrong msg round")
	}

	if !bytes.Equal(proposedMsg.Message.Root[:], signedCommit.Message.Root[:]) {
		return errors.New("proposed data mistmatch")
	}

	return nil
}
