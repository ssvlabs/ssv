package instance

import (
	"bytes"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/protocol/v2/qbft"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

// uponProposal process proposal message
// Assumes proposal message is valid!
func (i *Instance) uponProposal(logger *zap.Logger, signedProposal *specqbft.SignedMessage, proposeMsgContainer *specqbft.MsgContainer) error {
	addedMsg, err := proposeMsgContainer.AddFirstMsgForSignerAndRound(signedProposal)
	if err != nil {
		return errors.Wrap(err, "could not add proposal msg to container")
	}
	if !addedMsg {
		return nil // uponProposal was already called
	}

	logger.Debug("📬 got proposal message",
		fields.Round(i.State.Round),
		zap.Any("proposal-signers", signedProposal.Signers))

	newRound := signedProposal.Message.Round
	i.State.ProposalAcceptedForCurrentRound = signedProposal

	// A future justified proposal should bump us into future round and reset timer
	if signedProposal.Message.Round > i.State.Round {
		i.config.GetTimer().TimeoutForRound(signedProposal.Message.Height, signedProposal.Message.Round)
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
		zap.Any("proposal-signers", signedProposal.Signers),
		zap.Any("prepare-signers", prepare.Signers))

	if err := i.Broadcast(logger, prepare); err != nil {
		return errors.Wrap(err, "failed to broadcast prepare message")
	}
	return nil
}

func isValidProposal(
	state *specqbft.State,
	config qbft.IConfig,
	signedProposal *specqbft.SignedMessage,
	valCheck specqbft.ProposedValueCheckF,
	operators []*spectypes.Operator,
) error {
	if signedProposal.Message.MsgType != specqbft.ProposalMsgType {
		return errors.New("msg type is not proposal")
	}
	if signedProposal.Message.Height != state.Height {
		return errors.New("wrong msg height")
	}
	if len(signedProposal.GetSigners()) != 1 {
		return errors.New("msg allows 1 signer")
	}
	if config.VerifySignatures() {
		if err := ssvtypes.VerifyByOperators(signedProposal.Signature, signedProposal, config.GetSignatureDomainType(), spectypes.QBFTSignatureType, operators); err != nil {
			return errors.Wrap(err, "msg signature invalid")
		}
	}
	if !signedProposal.MatchedSigners([]spectypes.OperatorID{proposer(state, config, signedProposal.Message.Round)}) {
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
	if !bytes.Equal(signedProposal.Message.Root[:], r[:]) {
		return errors.New("H(data) != root")
	}

	// get justifications
	roundChangeJustification, _ := signedProposal.Message.GetRoundChangeJustifications() // no need to check error, checked on signedProposal.Validate()
	prepareJustification, _ := signedProposal.Message.GetPrepareJustifications()         // no need to check error, checked on signedProposal.Validate()

	if err := isProposalJustification(
		state,
		config,
		roundChangeJustification,
		prepareJustification,
		state.Height,
		signedProposal.Message.Round,
		signedProposal.FullData,
		valCheck,
	); err != nil {
		return errors.Wrap(err, "proposal not justified")
	}

	if (state.ProposalAcceptedForCurrentRound == nil && signedProposal.Message.Round == state.Round) ||
		signedProposal.Message.Round > state.Round {
		return nil
	}
	return errors.New("proposal is not valid with current state")
}

func IsProposalJustification(
	config qbft.IConfig,
	share *ssvtypes.SSVShare,
	roundChangeMsgs []*specqbft.SignedMessage,
	prepareMsgs []*specqbft.SignedMessage,
	height specqbft.Height,
	round specqbft.Round,
	fullData []byte,
) error {
	return isProposalJustification(
		&specqbft.State{
			Share:  &share.Share,
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
	roundChangeMsgs []*specqbft.SignedMessage,
	prepareMsgs []*specqbft.SignedMessage,
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
			if err := validRoundChangeForData(state, config, rc, height, round, fullData); err != nil {
				return errors.Wrap(err, "change round msg not valid")
			}
		}

		// check there is a quorum
		if !specqbft.HasQuorum(state.Share, roundChangeMsgs) {
			return errors.New("change round has no quorum")
		}

		// previouslyPreparedF returns true if any on the round change messages have a prepared round and fullData
		previouslyPrepared, err := func(rcMsgs []*specqbft.SignedMessage) (bool, error) {
			for _, rc := range rcMsgs {
				if rc.Message.RoundChangePrepared() {
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
			rcm, err := highestPrepared(roundChangeMsgs)
			if err != nil {
				return errors.Wrap(err, "could not get highest prepared")
			}
			if rcm == nil {
				return errors.New("no highest prepared")
			}

			// proposed fullData must equal highest prepared fullData
			r, err := specqbft.HashDataRoot(fullData)
			if err != nil {
				return errors.Wrap(err, "could not hash input data")
			}
			if !bytes.Equal(r[:], rcm.Message.Root[:]) {
				return errors.New("proposed data doesn't match highest prepared")
			}

			// validate each prepare message against the highest previously prepared fullData and round
			for _, pm := range prepareMsgs {
				if err := validSignedPrepareForHeightRoundAndRoot(
					config,
					pm,
					height,
					rcm.Message.DataRound,
					rcm.Message.Root,
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
func CreateProposal(state *specqbft.State, config qbft.IConfig, fullData []byte, roundChanges, prepares []*specqbft.SignedMessage) (*specqbft.SignedMessage, error) {
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
	sig, err := config.GetSigner().SignRoot(msg, spectypes.QBFTSignatureType, state.Share.SharePubKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed signing proposal msg")
	}

	signedMsg := &specqbft.SignedMessage{
		Signature: sig,
		Signers:   []spectypes.OperatorID{state.Share.OperatorID},
		Message:   *msg,

		FullData: fullData,
	}
	return signedMsg, nil
}
