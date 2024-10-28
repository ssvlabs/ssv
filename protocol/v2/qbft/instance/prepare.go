package instance

import (
	"bytes"
	"fmt"

	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/logging/fields"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"go.uber.org/zap"
)

// uponPrepare process prepare message
// Assumes prepare message is valid!
func (i *Instance) uponPrepare(logger *zap.Logger, msg *specqbft.ProcessingMessage, prepareMsgContainer *specqbft.MsgContainer) error {
	hasQuorumBefore := specqbft.HasQuorum(i.State.CommitteeMember, prepareMsgContainer.MessagesForRound(i.State.Round))

	addedMsg, err := prepareMsgContainer.AddFirstMsgForSignerAndRound(msg)
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

	if !specqbft.HasQuorum(i.State.CommitteeMember, prepareMsgContainer.MessagesForRound(i.State.Round)) {
		return nil // no quorum yet
	}

	i.State.LastPreparedValue = i.State.ProposalAcceptedForCurrentRound.SignedMessage.FullData
	i.State.LastPreparedRound = i.State.Round

	i.metrics.EndStagePrepare()

	logger.Debug("🎯 got prepare quorum",
		fields.Round(i.State.Round),
		zap.Any("prepare_signers", allSigners(prepareMsgContainer.MessagesForRound(i.State.Round))))

	commitMsg, err := i.CreateCommit(i.signer, proposedRoot)
	if err != nil {
		return errors.Wrap(err, "could not create commit msg")
	}

	logger.Debug("📢 broadcasting commit message",
		fields.Round(i.State.Round),
		zap.Any("commit_signers", commitMsg.OperatorIDs),
		fields.Root(proposedRoot))

	if err := i.Broadcast(logger, commitMsg); err != nil {
		return errors.Wrap(err, "failed to broadcast commit message")
	}

	return nil
}

// validSignedPrepareForHeightRoundAndRoot known in dafny spec as validSignedPrepareForHeightRoundAndDigest
// https://entethalliance.github.io/client-spec/qbft_spec.html#dfn-qbftspecification
func validSignedPrepareForHeightRoundAndRootIgnoreSignature(
	msg *specqbft.ProcessingMessage,
	height specqbft.Height,
	round specqbft.Round,
	wantRoot [32]byte,
	operators []*spectypes.Operator,
) error {
	if msg.QBFTMessage.MsgType != specqbft.PrepareMsgType {
		return errors.New("prepare msg type is wrong")
	}
	if msg.QBFTMessage.Height != height {
		return errors.New("wrong msg height")
	}
	if msg.QBFTMessage.Round != round {
		return errors.New("wrong msg round")
	}

	if err := msg.Validate(); err != nil {
		return errors.Wrap(err, "prepareData invalid")
	}

	gotRoot := msg.QBFTMessage.Root[:]
	if !bytes.Equal(gotRoot, wantRoot[:]) {
		return fmt.Errorf("proposed data root mismatch, want: %s, got: %s", wantRoot, gotRoot)
	}

	signerCnt := len(msg.SignedMessage.OperatorIDs)
	if signerCnt != 1 {
		return fmt.Errorf("msg must have exactly 1 signer, got: %d", signerCnt)
	}

	if !msg.SignedMessage.CheckSignersInCommittee(operators) {
		return errors.New("signer not in committee")
	}

	return nil
}

func validSignedPrepareForHeightRoundAndRootVerifySignature(
	msg *specqbft.ProcessingMessage,
	height specqbft.Height,
	round specqbft.Round,
	root [32]byte,
	operators []*spectypes.Operator,
) error {
	if err := validSignedPrepareForHeightRoundAndRootIgnoreSignature(msg, height, round, root, operators); err != nil {
		return err
	}

	// Verify signature
	if err := spectypes.Verify(msg.SignedMessage, operators); err != nil {
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
func (i *Instance) CreatePrepare(signer ssvtypes.OperatorSigner, newRound specqbft.Round, root [32]byte) (*spectypes.SignedSSVMessage, error) {
	msg := &specqbft.Message{
		MsgType:    specqbft.PrepareMsgType,
		Height:     i.State.Height,
		Round:      newRound,
		Identifier: i.State.ID,

		Root: root,
	}

	return ssvtypes.Sign(msg, i.State.CommitteeMember.OperatorID, signer)
}
