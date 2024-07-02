package instance

import (
	"bytes"

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

// uponPrepare process prepare message
// Assumes prepare message is valid!
func (i *Instance) uponPrepare(logger *zap.Logger, signedPrepare *genesisspecqbft.SignedMessage, prepareMsgContainer *genesisspecqbft.MsgContainer) error {
	hasQuorumBefore := HasQuorum(i.State.Share, prepareMsgContainer.MessagesForRound(i.State.Round))

	addedMsg, err := prepareMsgContainer.AddFirstMsgForSignerAndRound(signedPrepare)
	if err != nil {
		return errors.Wrap(err, "could not add prepare msg to container")
	}
	if !addedMsg {
		return nil // uponPrepare was already called
	}

	logger.Debug("📬 got prepare message",
		fields.Round(specqbft.Round(i.State.Round)),
		zap.Any("prepare-signers", signedPrepare.Signers),
		fields.Root(signedPrepare.Message.Root))

	if hasQuorumBefore {
		return nil // already moved to commit stage
	}

	if !HasQuorum(i.State.Share, prepareMsgContainer.MessagesForRound(i.State.Round)) {
		return nil // no quorum yet
	}

	proposedRoot := i.State.ProposalAcceptedForCurrentRound.Message.Root

	i.State.LastPreparedValue = i.State.ProposalAcceptedForCurrentRound.FullData
	i.State.LastPreparedRound = i.State.Round

	i.metrics.EndStagePrepare()

	logger.Debug("🎯 got prepare quorum",
		fields.Round(specqbft.Round(i.State.Round)),
		zap.Any("prepare-signers", allSigners(prepareMsgContainer.MessagesForRound(i.State.Round))),
		fields.Root(proposedRoot))

	commitMsg, err := CreateCommit(i.State, i.config, proposedRoot)
	if err != nil {
		return errors.Wrap(err, "could not create commit msg")
	}

	logger.Debug("📢 broadcasting commit message",
		fields.Round(specqbft.Round(i.State.Round)),
		zap.Any("commit-singers", commitMsg.Signers),
		fields.Root(commitMsg.Message.Root))

	if err := i.Broadcast(logger, commitMsg); err != nil {
		return errors.Wrap(err, "failed to broadcast commit message")
	}

	return nil
}

// getRoundChangeJustification returns the round change justification for the current round.
// The justification is a quorum of signed prepare messages that agree on state.LastPreparedValue
func getRoundChangeJustification(state *types.State, config qbft.IConfig, prepareMsgContainer *genesisspecqbft.MsgContainer) ([]*genesisspecqbft.SignedMessage, error) {
	if state.LastPreparedValue == nil {
		return nil, nil
	}

	r, err := genesisspecqbft.HashDataRoot(state.LastPreparedValue)
	if err != nil {
		return nil, errors.Wrap(err, "could not hash input data")
	}

	prepareMsgs := prepareMsgContainer.MessagesForRound(state.LastPreparedRound)
	ret := make([]*genesisspecqbft.SignedMessage, 0)
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

	if !HasQuorum(state.Share, ret) {
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
	signedPrepare *genesisspecqbft.SignedMessage,
	height genesisspecqbft.Height,
	round genesisspecqbft.Round,
	root [32]byte,
	operators []*spectypes.ShareMember) error {
	if signedPrepare.Message.MsgType != genesisspecqbft.PrepareMsgType {
		return errors.New("prepare msg type is wrong")
	}
	if signedPrepare.Message.Height != height {
		return errors.New("wrong msg height")
	}
	if signedPrepare.Message.Round != round {
		return errors.New("wrong msg round")
	}

	if err := signedPrepare.Validate(); err != nil {
		return errors.Wrap(err, "prepareData invalid")
	}

	if !bytes.Equal(signedPrepare.Message.Root[:], root[:]) {
		return errors.New("proposed data mistmatch")
	}

	if len(signedPrepare.GetSigners()) != 1 {
		return errors.New("msg allows 1 signer")
	}

	if config.VerifySignatures() {
		if err := types.VerifyByOperators(signedPrepare.Signature, signedPrepare, config.GetSignatureDomainType(), genesisspectypes.QBFTSignatureType, operators); err != nil {
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
func CreatePrepare(state *types.State, config qbft.IConfig, newRound genesisspecqbft.Round, root [32]byte) (*genesisspecqbft.SignedMessage, error) {
	msg := &genesisspecqbft.Message{
		MsgType:    genesisspecqbft.PrepareMsgType,
		Height:     state.Height,
		Round:      newRound,
		Identifier: state.ID,

		Root: root,
	}
	sig, err := config.GetSigner().SignRoot(msg, genesisspectypes.QBFTSignatureType, state.Share.SharePubKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed signing prepare msg")
	}

	signedMsg := &genesisspecqbft.SignedMessage{
		Signature: sig,
		Signers:   []genesisspectypes.OperatorID{state.Share.Committee[0].Signer},
		Message:   *msg,
	}
	return signedMsg, nil
}
