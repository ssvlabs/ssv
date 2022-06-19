package qbft

import (
	"github.com/bloxapp/ssv/spec/types"
	"github.com/pkg/errors"
)

func uponProposal(state *State, config IConfig, signedProposal *SignedMessage, proposeMsgContainer *MsgContainer) error {
	valCheck := config.GetValueCheck()
	if err := isValidProposal(state, config, signedProposal, valCheck, state.Share.Committee); err != nil {
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
	state.ProposalAcceptedForCurrentRound = signedProposal
	if signedProposal.Message.Round > state.Round {
		config.GetTimer().TimeoutForRound(signedProposal.Message.Round)
	}
	state.Round = newRound

	proposalData, err := signedProposal.Message.GetProposalData()
	if err != nil {
		return errors.Wrap(err, "could not get proposal data")
	}

	prepare, err := createPrepare(state, config, newRound, proposalData.Data)
	if err != nil {
		return errors.Wrap(err, "could not create prepare msg")
	}

	if err := config.GetNetwork().Broadcast(prepare); err != nil {
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
		signedProposal.Signers[0], // already verified sig so we know there is 1 signer
	); err != nil {
		return errors.Wrap(err, "proposal not justified")
	}

	if (state.ProposalAcceptedForCurrentRound == nil && signedProposal.Message.Round == state.Round) ||
		(state.ProposalAcceptedForCurrentRound != nil && signedProposal.Message.Round > state.Round) {
		return nil
	}
	return errors.New("proposal is not valid with current state")
}

// isProposalJustification returns nil if the signed proposal msg is justified
func isProposalJustification(
	state *State,
	config IConfig,
	roundChangeMsgs []*SignedMessage,
	prepareMsgs []*SignedMessage,
	height Height,
	round Round,
	value []byte,
	valCheck ProposedValueCheck,
	roundLeader types.OperatorID,
) error {
	if err := valCheck(value); err != nil {
		return errors.Wrap(err, "proposal value invalid")
	}

	if round == FirstRound {
		if proposer(state, round) != roundLeader {
			return errors.New("round leader is wrong")
		}
		return nil
	} else {
		if !state.Share.HasQuorum(len(roundChangeMsgs)) {
			return errors.New("change round has not quorum")
		}

		for _, rc := range roundChangeMsgs {
			if err := validRoundChange(state, config, rc, height, round); err != nil {
				return errors.Wrap(err, "change round msg not valid")
			}
		}

		previouslyPreparedF := func() bool {
			for _, rc := range roundChangeMsgs { // TODO - might be redundant as it's checked in validRoundChange
				if rc.Message.GetRoundChangeData().GetPreparedRound() != NoRound &&
					rc.Message.GetRoundChangeData().GetPreparedValue() != nil {
					return true
				}
			}
			return false
		}

		if !previouslyPreparedF() {
			if proposer(state, round) != roundLeader {
				return errors.New("round leader is wrong")
			}
			return nil
		} else {
			if !state.Share.HasQuorum(len(prepareMsgs)) {
				return errors.New("change round has not quorum")
			}

			rcm := highestPrepared(roundChangeMsgs)
			if rcm == nil {
				return errors.New("no highest prepared")
			}

			for _, pm := range prepareMsgs {
				if err := validSignedPrepareForHeightRoundAndValue(
					state,
					config,
					pm,
					height,
					rcm.Message.GetRoundChangeData().GetPreparedRound(),
					rcm.Message.GetRoundChangeData().GetPreparedValue(),
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
func createProposal(state *State, config IConfig, value []byte, roundChanges, prepares []*SignedMessage) (*SignedMessage, error) {
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
