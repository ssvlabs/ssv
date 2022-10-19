package instance

import (
	"bytes"
	qbftspec "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
)

// UponCommit returns true if a quorum of commit messages was received.
func (i *Instance) UponCommit(signedCommit *qbftspec.SignedMessage, commitMsgContainer *qbftspec.MsgContainer) (bool, []byte, *qbftspec.SignedMessage, error) {
	if i.State.ProposalAcceptedForCurrentRound == nil {
		return false, nil, nil, errors.New("did not receive proposal for this round")
	}

	if err := validateCommit(
		i.config,
		signedCommit,
		i.State.Height,
		i.State.Round,
		i.State.ProposalAcceptedForCurrentRound,
		i.State.Share.Committee,
	); err != nil {
		return false, nil, nil, errors.Wrap(err, "commit msg invalid")
	}

	addMsg, err := commitMsgContainer.AddFirstMsgForSignerAndRound(signedCommit)
	if err != nil {
		return false, nil, nil, errors.Wrap(err, "could not add commit msg to container")
	}
	if !addMsg {
		return false, nil, nil, nil // UponCommit was already called
	}

	// calculate commit quorum and act upon it
	quorum, commitMsgs, err := commitQuorumForRoundValue(i.State, commitMsgContainer, signedCommit.Message.Data, signedCommit.Message.Round)
	if err != nil {
		return false, nil, nil, errors.Wrap(err, "could not calculate commit quorum")
	}
	if quorum {
		msgCommitData, err := signedCommit.Message.GetCommitData()
		if err != nil {
			return false, nil, nil, errors.Wrap(err, "could not get msg commit data")
		}

		agg, err := aggregateCommitMsgs(commitMsgs)
		if err != nil {
			return false, nil, nil, errors.Wrap(err, "could not aggregate commit msgs")
		}
		return true, msgCommitData.Data, agg, nil
	}
	return false, nil, nil, nil
}

// returns true if there is a quorum for the current round for this provided value
func commitQuorumForRoundValue(state *qbftspec.State, commitMsgContainer *qbftspec.MsgContainer, value []byte, round qbftspec.Round) (bool, []*qbftspec.SignedMessage, error) {
	signers, msgs := commitMsgContainer.LongestUniqueSignersForRoundAndValue(round, value)
	return state.Share.HasQuorum(len(signers)), msgs, nil
}

func aggregateCommitMsgs(msgs []*qbftspec.SignedMessage) (*qbftspec.SignedMessage, error) {
	if len(msgs) == 0 {
		return nil, errors.New("can't aggregate zero commit msgs")
	}

	var ret *qbftspec.SignedMessage
	for _, m := range msgs {
		if ret == nil {
			ret = m.DeepCopy()
		} else {
			if err := ret.Aggregate(m); err != nil {
				return nil, errors.Wrap(err, "could not aggregate commit msg")
			}
		}
	}
	return ret, nil
}

// didSendCommitForHeightAndRound returns true if sent commit msg for specific Height and round
/**
!exists m :: && m in current.messagesReceived
                            && m.Commit?
                            && var uPayload := m.commitPayload.unsignedPayload;
                            && uPayload.Height == |current.blockchain|
                            && uPayload.round == current.round
                            && recoverSignedCommitAuthor(m.commitPayload) == current.id
*/
func didSendCommitForHeightAndRound(state *qbftspec.State, commitMsgContainer *qbftspec.MsgContainer) bool {
	for _, msg := range commitMsgContainer.MessagesForRound(state.Round) {
		if msg.MatchedSigners([]types.OperatorID{state.Share.OperatorID}) {
			return true
		}
	}
	return false
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
func CreateCommit(state *qbftspec.State, config qbftspec.IConfig, value []byte) (*qbftspec.SignedMessage, error) {
	commitData := &qbftspec.CommitData{
		Data: value,
	}
	dataByts, err := commitData.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "failed encoding prepare data")
	}
	msg := &qbftspec.Message{
		MsgType:    qbftspec.CommitMsgType,
		Height:     state.Height,
		Round:      state.Round,
		Identifier: state.ID,
		Data:       dataByts,
	}
	sig, err := config.GetSigner().SignRoot(msg, types.QBFTSignatureType, state.Share.SharePubKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed signing commit msg")
	}

	signedMsg := &qbftspec.SignedMessage{
		Signature: sig,
		Signers:   []types.OperatorID{state.Share.OperatorID},
		Message:   msg,
	}
	return signedMsg, nil
}

func baseCommitValidation(
	config qbftspec.IConfig,
	signedCommit *qbftspec.SignedMessage,
	height qbftspec.Height,
	operators []*types.Operator,
) error {
	if signedCommit.Message.MsgType != qbftspec.CommitMsgType {
		return errors.New("commit msg type is wrong")
	}
	if signedCommit.Message.Height != height {
		return errors.New("commit Height is wrong")
	}

	msgCommitData, err := signedCommit.Message.GetCommitData()
	if err != nil {
		return errors.Wrap(err, "could not get msg commit data")
	}
	if err := msgCommitData.Validate(); err != nil {
		return errors.Wrap(err, "msgCommitData invalid")
	}

	// verify signature
	if err := signedCommit.Signature.VerifyByOperators(signedCommit, config.GetSignatureDomainType(), types.QBFTSignatureType, operators); err != nil {
		return errors.Wrap(err, "commit msg signature invalid")
	}

	return nil
}

func validateCommit(
	config qbftspec.IConfig,
	signedCommit *qbftspec.SignedMessage,
	height qbftspec.Height,
	round qbftspec.Round,
	proposedMsg *qbftspec.SignedMessage,
	operators []*types.Operator,
) error {
	if err := baseCommitValidation(config, signedCommit, height, operators); err != nil {
		return errors.Wrap(err, "invalid commit msg")
	}

	if len(signedCommit.Signers) != 1 {
		return errors.New("commit msgs allow 1 signer")
	}

	if signedCommit.Message.Round != round {
		return errors.New("commit round is wrong")
	}

	proposedCommitData, err := proposedMsg.Message.GetCommitData()
	if err != nil {
		return errors.Wrap(err, "could not get proposed commit data")
	}

	msgCommitData, err := signedCommit.Message.GetCommitData()
	if err != nil {
		return errors.Wrap(err, "could not get msg commit data")
	}

	if !bytes.Equal(proposedCommitData.Data, msgCommitData.Data) {
		return errors.New("proposed data different than commit msg data")
	}

	return nil
}
