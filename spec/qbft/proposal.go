package qbft

import (
	"github.com/bloxapp/ssv/spec/types"
	"github.com/pkg/errors"
)

func (i *Instance) uponProposal(signedProposal *SignedMessage, proposeMsgContainer *MsgContainer) error {
	valCheck := i.config.GetValueCheck()
	if err := isValidProposal(i.State, i.config, signedProposal, valCheck, i.State.Share.Committee); err != nil {
		return errors.Wrap(err, "proposal invalid")
	}

	addedMsg, err := proposeMsgContainer.AddIfDoesntExist(signedProposal)
	if err != nil {
		return errors.Wrap(err, "could not add proposal msg to container")
	}
	if !addedMsg {
		return nil // uponProposal was already called
	}

	newRound := signedProposal.Message.Round

	// set state to new round and proposal accepted
	i.State.ProposalAcceptedForCurrentRound = signedProposal
	// TODO - why is this here? we shouldn't timout on just a simple proposal
	if signedProposal.Message.Round > i.State.Round {
		i.config.GetTimer().TimeoutForRound(signedProposal.Message.Round)
	}
	i.State.Round = newRound

	proposalData, err := signedProposal.Message.GetProposalData()
	if err != nil {
		return errors.Wrap(err, "could not get proposal data")
	}

	prepare, err := CreatePrepare(i.State, i.config, newRound, proposalData.Data)
	if err != nil {
		return errors.Wrap(err, "could not create prepare msg")
	}

	if err := i.Broadcast(prepare); err != nil {
		return errors.Wrap(err, "failed to broadcast prepare message")
	}

	return nil
}

func isValidProposal(
	state *State,
	config IConfig,
	signedProposal *SignedMessage,
	valCheck ProposedValueCheck,
	operators []*types.Operator,
) error {
	if signedProposal.Message.MsgType != ProposalMsgType {
		return errors.New("msg type is not proposal")
	}
	if signedProposal.Message.Height != state.Height {
		return errors.New("proposal Height is wrong")
	}
	if len(signedProposal.GetSigners()) != 1 {
		return errors.New("proposal msg allows 1 signer")
	}
	if err := signedProposal.Signature.VerifyByOperators(signedProposal, config.GetSignatureDomainType(), types.QBFTSignatureType, operators); err != nil {
		return errors.Wrap(err, "proposal msg signature invalid")
	}
	if !signedProposal.MatchedSigners([]types.OperatorID{proposer(state, signedProposal.Message.Round)}) {
		return errors.New("proposal leader invalid")
	}

	proposalData, err := signedProposal.Message.GetProposalData()
	if err != nil {
		return errors.Wrap(err, "could not get proposal data")
	}
	if err := proposalData.Validate(); err != nil {
		return errors.Wrap(err, "proposalData invalid")
	}

	if err := isProposalJustification(
		state,
		config,
		proposalData.RoundChangeJustification,
		proposalData.PrepareJustification,
		state.Height,
		signedProposal.Message.Round,
		proposalData.Data,
		valCheck,
	); err != nil {
		return errors.Wrap(err, "proposal not justified")
	}

	if (state.ProposalAcceptedForCurrentRound == nil && signedProposal.Message.Round == state.Round) ||
		(state.ProposalAcceptedForCurrentRound != nil && signedProposal.Message.Round > state.Round) {
		return nil
	}
	return errors.New("proposal is not valid with current state")
}

// isProposalJustification returns nil if the proposal and round change messages are valid and justify a proposal message for the provided round, value and leader
func isProposalJustification(
	state *State,
	config IConfig,
	roundChangeMsgs []*SignedMessage,
	prepareMsgs []*SignedMessage,
	height Height,
	round Round,
	value []byte,
	valCheck ProposedValueCheck,
) error {
	if err := valCheck(value); err != nil {
		return errors.Wrap(err, "proposal value invalid")
	}

	if round == FirstRound {
		return nil
	} else {
		// check all round changes are valid for height and round
		for _, rc := range roundChangeMsgs {
			if err := validRoundChange(state, config, rc, height, round); err != nil {
				return errors.Wrap(err, "change round msg not valid")
			}
		}

		// check there is a quorum
		if !state.Share.HasQuorum(len(roundChangeMsgs)) {
			return errors.New("change round has not quorum")
		}

		// previouslyPreparedF returns true if any on the round change messages have a prepared round and value
		previouslyPrepared, err := func(rcMsgs []*SignedMessage) (bool, error) {
			for _, rc := range rcMsgs {
				rcData, err := rc.Message.GetRoundChangeData()
				if err != nil {
					return false, errors.Wrap(err, "could not get round change data")
				}
				if rcData.Prepared() {
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
			if !state.Share.HasQuorum(len(prepareMsgs)) {
				return errors.New("change round has not quorum")
			}

			// get a round change data for which there is a justification for the highest previously prepared round
			rcm, err := highestPrepared(roundChangeMsgs)
			if err != nil {
				return errors.Wrap(err, "could not get highest prepared")
			}
			if rcm == nil {
				return errors.New("no highest prepared")
			}
			rcmData, err := rcm.Message.GetRoundChangeData()
			if err != nil {
				return errors.Wrap(err, "could not get round change data")
			}

			// validate each prepare message against the highest previously prepared value and round
			for _, pm := range prepareMsgs {
				if err := validSignedPrepareForHeightRoundAndValue(
					config,
					pm,
					height,
					rcmData.PreparedRound,
					rcmData.PreparedValue,
					state.Share.Committee,
				); err != nil {
					return errors.New("signed prepare not valid")
				}
			}
			return nil
		}
	}
}

func proposer(state *State, round Round) types.OperatorID {
	// TODO - https://github.com/ConsenSys/qbft-formal-spec-and-verification/blob/29ae5a44551466453a84d4d17b9e083ecf189d97/dafny/spec/L1/node_auxiliary_functions.dfy#L304-L323
	return 1
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
func CreateProposal(state *State, config IConfig, value []byte, roundChanges, prepares []*SignedMessage) (*SignedMessage, error) {
	proposalData := &ProposalData{
		Data:                     value,
		RoundChangeJustification: roundChanges,
		PrepareJustification:     prepares,
	}
	dataByts, err := proposalData.Encode()

	msg := &Message{
		MsgType:    ProposalMsgType,
		Height:     state.Height,
		Round:      state.Round,
		Identifier: state.ID,
		Data:       dataByts,
	}
	sig, err := config.GetSigner().SignRoot(msg, types.QBFTSignatureType, state.Share.SharePubKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed signing prepare msg")
	}

	signedMsg := &SignedMessage{
		Signature: sig,
		Signers:   []types.OperatorID{state.Share.OperatorID},
		Message:   msg,
	}
	return signedMsg, nil
}
