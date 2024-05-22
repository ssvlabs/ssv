package instance

import (
	"bytes"
	"sort"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
)

// UponCommit returns true if a quorum of commit messages was received.
// Assumes commit message is valid!
func (i *Instance) UponCommit(logger *zap.Logger, signedCommit *spectypes.SignedSSVMessage, commitMsgContainer *specqbft.MsgContainer) (bool, []byte, *spectypes.SignedSSVMessage, error) {
	addMsg, err := commitMsgContainer.AddFirstMsgForSignerAndRound(signedCommit)
	if err != nil {
		return false, nil, nil, errors.Wrap(err, "could not add commit msg to container")
	}
	if !addMsg {
		return false, nil, nil, nil // UponCommit was already called
	}

	msg, err := specqbft.DecodeMessage(signedCommit.SSVMessage.Data)
	if err != nil {
		return false, nil, nil, err
	}

	logger.Debug("📬 got commit message",
		fields.Round(i.State.Round),
		zap.Any("commit-signers", signedCommit.OperatorIDs),
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
			zap.Any("agg-signers", agg.OperatorIDs),
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

	// Sort the OperatorIDs and Signatures in the SignedSSVMessage

	pairs := make([]struct {
		OpID spectypes.OperatorID
		Sig  spectypes.Signature
	}, len(ret.OperatorIDs))

	for i, id := range ret.OperatorIDs {
		pairs[i] = struct {
			OpID spectypes.OperatorID
			Sig  spectypes.Signature
		}{OpID: id, Sig: ret.Signatures[i]}
	}

	// Sort the slice of pairs
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].OpID < pairs[j].OpID
	})

	// Extract the sorted IDs and Signatures back into separate slices
	for i, pair := range pairs {
		ret.OperatorIDs[i] = pair.OpID
		ret.Signatures[i] = pair.Sig
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

func baseCommitValidationIgnoreSignature(
	signedCommit *spectypes.SignedSSVMessage,
	height specqbft.Height,
	operators []*spectypes.CommitteeMember,
) error {

	msg, err := specqbft.DecodeMessage(signedCommit.SSVMessage.Data)
	if err != nil {
		return errors.Wrap(err, "can't decode inner SSVMessage")
	}

	if msg.MsgType != specqbft.CommitMsgType {
		return errors.New("commit msg type is wrong")
	}
	if msg.Height != height {
		return errors.New("wrong msg height")
	}

	if err := signedCommit.Validate(); err != nil {
		return errors.Wrap(err, "signed commit invalid")
	}

	if !signedCommit.CheckSignersInCommittee(operators) {
		return errors.New("signer not in committee")
	}

	return nil
}

func BaseCommitValidationVerifySignature(
	config qbft.IConfig,
	signedCommit *spectypes.SignedSSVMessage,
	height specqbft.Height,
	operators []*spectypes.CommitteeMember,
) error {

	if err := baseCommitValidationIgnoreSignature(signedCommit, height, operators); err != nil {
		return err
	}

	if config.VerifySignatures() {

		// verify signature
		if err := config.GetSignatureVerifier().Verify(signedCommit, operators); err != nil {
			return errors.Wrap(err, "msg signature invalid")
		}

	}

	return nil
}

func validateCommit(
	signedCommit *spectypes.SignedSSVMessage,
	height specqbft.Height,
	round specqbft.Round,
	proposedMsg *spectypes.SignedSSVMessage,
	operators []*spectypes.CommitteeMember,
) error {
	if err := baseCommitValidationIgnoreSignature(signedCommit, height, operators); err != nil {
		return err
	}

	if len(signedCommit.Signatures) != 1 {
		return errors.New("msg allows 1 signer")
	}

	msg, err := specqbft.DecodeMessage(signedCommit.SSVMessage.Data)
	if err != nil {
		return errors.Wrap(err, "could not decode inner SSVMessage")
	}

	if msg.Round != round {
		return errors.New("wrong msg round")
	}

	if !bytes.Equal(msg.Root[:], msg.Root[:]) {
		return errors.New("proposed data mismatch")
	}

	return nil
}
