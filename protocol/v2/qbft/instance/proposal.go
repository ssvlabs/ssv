package instance

import (
	"bytes"

	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv-spec/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/logging/fields"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/v2/qbft"
)

// uponProposal process proposal message
// Assumes proposal message is valid!
func (i *Instance) uponProposal(logger *zap.Logger, signedProposal *spectypes.SignedSSVMessage, proposeMsgContainer *specqbft.MsgContainer) error {
	addedMsg, err := proposeMsgContainer.AddFirstMsgForSignerAndRound(signedProposal)
	if err != nil {
		return errors.Wrap(err, "could not add proposal msg to container")
	}
	if !addedMsg {
		return nil // uponProposal was already called
	}

	msg, err := specqbft.DecodeMessage(signedProposal.SSVMessage.Data)
	if err != nil {
		return err
	}

	logger.Debug("📬 got proposal message",
		fields.Round(i.State.Round),
		zap.Any("proposal-signers", signedProposal.OperatorIDs))

	newRound := msg.Round
	i.State.ProposalAcceptedForCurrentRound = signedProposal

	// A future justified proposal should bump us into future round and reset timer
	if msg.Round > i.State.Round {
		i.config.GetTimer().TimeoutForRound(msg.Height, msg.Round)
	}
	i.bumpToRound(newRound)

	i.metrics.EndStageProposal()

	// value root
	r, err := specqbft.HashDataRoot(signedProposal.FullData)
	if err != nil {
		return errors.Wrap(err, "could not hash input data")
	}

	prepare, err := CreatePrepare(i.State, i.config, newRound, r)
	if err != nil {
		return errors.Wrap(err, "could not create prepare msg")
	}

	logger.Debug("📢 got proposal, broadcasting prepare message",
		fields.Round(i.State.Round),
		zap.Any("proposal-signers", signedProposal.OperatorIDs),
		zap.Any("prepare-signers", prepare.OperatorIDs))

	if err := i.Broadcast(logger, prepare); err != nil {
		return errors.Wrap(err, "failed to broadcast prepare message")
	}
	return nil
}

func isValidProposal(
	state *specqbft.State,
	config qbft.IConfig,
	signedProposal *spectypes.SignedSSVMessage,
	valCheck specqbft.ProposedValueCheckF,
) error {
	msg, err := specqbft.DecodeMessage(signedProposal.SSVMessage.Data)
	if err != nil {
		return err
	}

	if msg.MsgType != specqbft.ProposalMsgType {
		return errors.New("msg type is not proposal")
	}
	if msg.Height != state.Height {
		return errors.New("wrong msg height")
	}
	if len(signedProposal.GetOperatorIDs()) != 1 {
		return errors.New("msg allows 1 signer")
	}

	if !signedProposal.CheckSignersInCommittee(state.Share.Committee) {
		return errors.New("signer not in committee")
	}

	if !signedProposal.MatchedSigners([]types.OperatorID{proposer(state, config, msg.Round)}) {
		return errors.New("proposal leader invalid")
	}

	if err := signedProposal.Validate(); err != nil {
		return errors.Wrap(err, "proposal invalid")
	}

	// verify full data integrity
	r, err := specqbft.HashDataRoot(signedProposal.FullData)
	if err != nil {
		return errors.Wrap(err, "could not hash input data")
	}
	if !bytes.Equal(msg.Root[:], r[:]) {
		return errors.New("H(data) != root")
	}

	// get justifications
	roundChangeJustification, _ := msg.GetRoundChangeJustifications() // no need to check error, checked on signedProposal.Validate()
	prepareJustification, _ := msg.GetPrepareJustifications()         // no need to check error, checked on signedProposal.Validate()

	if err := isProposalJustification(
		state,
		config,
		roundChangeJustification,
		prepareJustification,
		state.Height,
		msg.Round,
		signedProposal.FullData,
		valCheck,
	); err != nil {
		return errors.Wrap(err, "proposal not justified")
	}

	if (state.ProposalAcceptedForCurrentRound == nil && msg.Round == state.Round) ||
		msg.Round > state.Round {
		return nil
	}
	return errors.New("proposal is not valid with current state")
}

func IsProposalJustification(
	config qbft.IConfig,
	share *spectypes.Operator,
	roundChangeMsgs []*spectypes.SignedSSVMessage,
	prepareMsgs []*spectypes.SignedSSVMessage,
	height specqbft.Height,
	round specqbft.Round,
	fullData []byte,
) error {
	return isProposalJustification(
		&specqbft.State{
			Share:  share,
			Height: height,
		},
		config,
		roundChangeMsgs,
		prepareMsgs,
		height,
		round,
		fullData,
		func(data []byte) error { return nil },
	)
}

// isProposalJustification returns nil if the proposal and round change messages are valid and justify a proposal message for the provided round, value and leader
func isProposalJustification(
	state *specqbft.State,
	config qbft.IConfig,
	roundChangeMsgs []*spectypes.SignedSSVMessage,
	prepareMsgs []*spectypes.SignedSSVMessage,
	height specqbft.Height,
	round specqbft.Round,
	fullData []byte,
	valCheck specqbft.ProposedValueCheckF,
) error {
	if err := valCheck(fullData); err != nil {
		return errors.Wrap(err, "proposal fullData invalid")
	}

	if round == specqbft.FirstRound {
		return nil
	} else {
		// check all round changes are valid for height and round
		// no quorum, duplicate signers,  invalid still has quorum, invalid no quorum
		// prepared
		for _, rc := range roundChangeMsgs {
			if err := validRoundChangeForDataVerifySignature(state, config, rc, height, round, fullData); err != nil {
				return errors.Wrap(err, "change round msg not valid")
			}
		}

		// check there is a quorum
		if !specqbft.HasQuorum(state.Share, roundChangeMsgs) {
			return errors.New("change round has no quorum")
		}

		// previouslyPreparedF returns true if any on the round change messages have a prepared round and fullData
		previouslyPrepared, err := func(rcMsgs []*spectypes.SignedSSVMessage) (bool, error) {
			for _, rc := range rcMsgs {

				msg, err := specqbft.DecodeMessage(rc.SSVMessage.Data)
				if err != nil {
					continue
				}

				if msg.RoundChangePrepared() {
					return true, nil
				}
			}
			return false, nil
		}(roundChangeMsgs)
		if err != nil {
			return errors.Wrap(err, "could not calculate if previously prepared")
		}

		if !previouslyPrepared {
			return nil
		} else {

			// check prepare quorum
			if !specqbft.HasQuorum(state.Share, prepareMsgs) {
				return errors.New("prepares has no quorum")
			}

			// get a round change data for which there is a justification for the highest previously prepared round
			rcSignedMsg, err := highestPrepared(roundChangeMsgs)
			if err != nil {
				return errors.Wrap(err, "could not get highest prepared")
			}
			if rcSignedMsg == nil {
				return errors.New("no highest prepared")
			}

			rcMsg, err := specqbft.DecodeMessage(rcSignedMsg.SSVMessage.Data)
			if err != nil {
				return errors.Wrap(err, "highest prepared can't be decoded to Message")
			}

			// proposed fullData must equal highest prepared fullData
			r, err := specqbft.HashDataRoot(fullData)
			if err != nil {
				return errors.Wrap(err, "could not hash input data")
			}
			if !bytes.Equal(r[:], rcMsg.Root[:]) {
				return errors.New("proposed data doesn't match highest prepared")
			}

			// validate each prepare message against the highest previously prepared fullData and round
			for _, pm := range prepareMsgs {
				if err := validSignedPrepareForHeightRoundAndRootVerifySignature(
					config,
					pm,
					height,
					rcMsg.DataRound,
					rcMsg.Root,
					state.Share.Committee,
				); err != nil {
					return errors.New("signed prepare not valid")
				}
			}
			return nil
		}
	}
}

func proposer(state *specqbft.State, config qbft.IConfig, round specqbft.Round) spectypes.OperatorID {
	// TODO - https://github.com/ConsenSys/qbft-formal-spec-and-verification/blob/29ae5a44551466453a84d4d17b9e083ecf189d97/dafny/spec/L1/node_auxiliary_functions.dfy#L304-L323
	return config.GetProposerF()(state, round)
}

// CreateProposal
/**
  	Proposal(
                        signProposal(
                            UnsignedProposal(
                                |current.blockchain|,
                                newRound,
                                digest(block)),
                            current.id),
                        block,
                        extractSignedRoundChanges(roundChanges),
                        extractSignedPrepares(prepares));
*/
func CreateProposal(state *specqbft.State, config qbft.IConfig, fullData []byte, roundChanges, prepares []*spectypes.SignedSSVMessage) (*spectypes.SignedSSVMessage, error) {
	r, err := specqbft.HashDataRoot(fullData)
	if err != nil {
		return nil, errors.Wrap(err, "could not hash input data")
	}

	roundChangesData, err := specqbft.MarshalJustifications(roundChanges)
	if err != nil {
		return nil, errors.Wrap(err, "could not marshal justifications")
	}
	preparesData, err := specqbft.MarshalJustifications(prepares)
	if err != nil {
		return nil, errors.Wrap(err, "could not marshal justifications")
	}

	msg := &specqbft.Message{
		MsgType:    specqbft.ProposalMsgType,
		Height:     state.Height,
		Round:      state.Round,
		Identifier: state.ID,

		Root:                     r,
		RoundChangeJustification: roundChangesData,
		PrepareJustification:     preparesData,
	}
	return specqbft.MessageToSignedSSVMessageWithFullData(msg, state.Share.OperatorID, config.GetOperatorSigner(), fullData)
}
