package instance

import (
	"bytes"
	"context"

	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// uponProposal process proposal message
// Assumes proposal message is valid!
func (i *Instance) uponProposal(ctx context.Context, logger *zap.Logger, msg *specqbft.ProcessingMessage) error {
	addedMsg, err := i.State.ProposeContainer.AddFirstMsgForSignerAndRound(msg)
	if err != nil {
		return errors.Wrap(err, "could not add proposal msg to container")
	}
	if !addedMsg {
		return nil // uponProposal was already called
	}

	logger.Debug("📬 got proposal message",
		fields.Round(i.State.Round),
		zap.Any("proposal_signers", msg.SignedMessage.OperatorIDs))

	newRound := msg.QBFTMessage.Round
	i.State.ProposalAcceptedForCurrentRound = msg

	// A future justified proposal should bump us into future round and reset timer
	if msg.QBFTMessage.Round > i.State.Round {
		i.config.GetTimer().TimeoutForRound(msg.QBFTMessage.Height, msg.QBFTMessage.Round)
	}
	i.bumpToRound(newRound)

	i.metrics.EndStage(ctx, newRound, stageProposal)

	// value root
	r, err := specqbft.HashDataRoot(msg.SignedMessage.FullData)
	if err != nil {
		return errors.Wrap(err, "could not hash input data")
	}

	prepare, err := CreatePrepare(i.State, i.signer, newRound, r)
	if err != nil {
		return errors.Wrap(err, "could not create prepare msg")
	}

	logger.Debug("📢 got proposal, broadcasting prepare message",
		fields.Round(i.State.Round),
		zap.Any("proposal_signers", msg.SignedMessage.OperatorIDs),
		zap.Any("prepare_signers", prepare.OperatorIDs))

	if err := i.Broadcast(prepare); err != nil {
		return errors.Wrap(err, "failed to broadcast prepare message")
	}
	return nil
}

func (i *Instance) isValidProposal(msg *specqbft.ProcessingMessage) error {
	if msg.QBFTMessage.MsgType != specqbft.ProposalMsgType {
		return errors.New("msg type is not proposal")
	}
	if msg.QBFTMessage.Height != i.State.Height {
		return errors.New("wrong msg height")
	}
	if len(msg.SignedMessage.OperatorIDs) != 1 {
		return errors.New("msg allows 1 signer")
	}

	if !msg.SignedMessage.CheckSignersInCommittee(i.State.CommitteeMember.Committee) {
		return errors.New("signer not in committee")
	}

	if !msg.SignedMessage.MatchedSigners([]spectypes.OperatorID{i.proposer(msg.QBFTMessage.Round)}) {
		return errors.New("proposal leader invalid")
	}

	if err := msg.Validate(); err != nil {
		return errors.Wrap(err, "proposal invalid")
	}

	// verify full data integrity
	r, err := specqbft.HashDataRoot(msg.SignedMessage.FullData)
	if err != nil {
		return errors.Wrap(err, "could not hash input data")
	}
	if !bytes.Equal(msg.QBFTMessage.Root[:], r[:]) {
		return errors.New("H(data) != root")
	}

	// get justifications
	roundChangeJustificationSignedMessages, _ := msg.QBFTMessage.GetRoundChangeJustifications() // no need to check error, checked on msg.SignedMessage.Validate()
	prepareJustificationSignedMessages, _ := msg.QBFTMessage.GetPrepareJustifications()         // no need to check error, checked on msg.SignedMessage.Validate()

	roundChangeJustification := make([]*specqbft.ProcessingMessage, 0)
	for _, rcSignedMessage := range roundChangeJustificationSignedMessages {
		rc, err := specqbft.NewProcessingMessage(rcSignedMessage)
		if err != nil {
			return errors.Wrap(err, "could not create ProcessingMessage from round change justification")
		}
		roundChangeJustification = append(roundChangeJustification, rc)
	}
	prepareJustification := make([]*specqbft.ProcessingMessage, 0)
	for _, prepareSignedMessage := range prepareJustificationSignedMessages {
		procMsg, err := specqbft.NewProcessingMessage(prepareSignedMessage)
		if err != nil {
			return errors.Wrap(err, "could not create ProcessingMessage from prepare justification")
		}
		prepareJustification = append(prepareJustification, procMsg)
	}

	if err := i.isProposalJustification(
		roundChangeJustification,
		prepareJustification,
		msg.QBFTMessage.Round,
		msg.SignedMessage.FullData,
	); err != nil {
		return errors.Wrap(err, "proposal not justified")
	}

	if (i.State.ProposalAcceptedForCurrentRound == nil && msg.QBFTMessage.Round == i.State.Round) ||
		msg.QBFTMessage.Round > i.State.Round {
		return nil
	}
	return errors.New("proposal is not valid with current state")
}

// isProposalJustification returns nil if the proposal and round change messages are valid and justify a proposal message for the provided round, value and leader
func (i *Instance) isProposalJustification(
	roundChangeMsgs []*specqbft.ProcessingMessage,
	prepareMsgs []*specqbft.ProcessingMessage,
	round specqbft.Round,
	fullData []byte,
) error {
	if err := i.config.GetValueCheckF()(fullData); err != nil {
		return errors.Wrap(err, "proposal fullData invalid")
	}

	if round == specqbft.FirstRound {
		return nil
	} else {
		// check all round changes are valid for height and round
		// no quorum, duplicate signers,  invalid still has quorum, invalid no quorum
		// prepared
		for _, rc := range roundChangeMsgs {
			if err := i.validRoundChangeForDataVerifySignature(rc, round, fullData); err != nil {
				return errors.Wrap(err, "change round msg not valid")
			}
		}

		// check there is a quorum
		if !specqbft.HasQuorum(i.State.CommitteeMember, roundChangeMsgs) {
			return errors.New("change round has no quorum")
		}

		// previouslyPreparedF returns true if any on the round change messages have a prepared round and fullData
		previouslyPrepared, err := func(rcMsgs []*specqbft.ProcessingMessage) (bool, error) {
			for _, rc := range rcMsgs {
				if rc.QBFTMessage.RoundChangePrepared() {
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
			if !specqbft.HasQuorum(i.State.CommitteeMember, prepareMsgs) {
				return errors.New("prepares has no quorum")
			}

			// get a round change data for which there is a justification for the highest previously prepared round
			rcMsg, err := highestPrepared(roundChangeMsgs)
			if err != nil {
				return errors.Wrap(err, "could not get highest prepared")
			}
			if rcMsg == nil {
				return errors.New("no highest prepared")
			}

			// proposed fullData must equal highest prepared fullData
			r, err := specqbft.HashDataRoot(fullData)
			if err != nil {
				return errors.Wrap(err, "could not hash input data")
			}
			if !bytes.Equal(r[:], rcMsg.QBFTMessage.Root[:]) {
				return errors.New("proposed data doesn't match highest prepared")
			}

			// validate each prepare message against the highest previously prepared fullData and round
			for _, pm := range prepareMsgs {
				if err := i.validSignedPrepareForHeightRoundAndRootVerifySignature(
					pm,
					rcMsg.QBFTMessage.DataRound,
					rcMsg.QBFTMessage.Root,
				); err != nil {
					return errors.New("signed prepare not valid")
				}
			}
			return nil
		}
	}
}

func (i *Instance) proposer(round specqbft.Round) spectypes.OperatorID {
	// TODO - https://github.com/ConsenSys/qbft-formal-spec-and-verification/blob/29ae5a44551466453a84d4d17b9e083ecf189d97/dafny/spec/L1/node_auxiliary_functions.dfy#L304-L323
	return i.config.GetProposerF()(i.State, round)
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
func CreateProposal(state *specqbft.State, signer ssvtypes.OperatorSigner, fullData []byte, roundChanges, prepares []*specqbft.ProcessingMessage) (*spectypes.SignedSSVMessage, error) {
	r, err := specqbft.HashDataRoot(fullData)
	if err != nil {
		return nil, errors.Wrap(err, "could not hash input data")
	}

	roundChangeSignedMessages := make([]*spectypes.SignedSSVMessage, 0)
	for _, msg := range roundChanges {
		roundChangeSignedMessages = append(roundChangeSignedMessages, msg.SignedMessage)
	}
	prepareSignedMessages := make([]*spectypes.SignedSSVMessage, 0)
	for _, msg := range prepares {
		prepareSignedMessages = append(prepareSignedMessages, msg.SignedMessage)
	}

	roundChangesData, err := specqbft.MarshalJustifications(roundChangeSignedMessages)
	if err != nil {
		return nil, errors.Wrap(err, "could not marshal justifications")
	}
	preparesData, err := specqbft.MarshalJustifications(prepareSignedMessages)
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

	signedMsg, err := ssvtypes.Sign(msg, state.CommitteeMember.OperatorID, signer)
	if err != nil {
		return nil, errors.Wrap(err, "could not wrap proposal message")
	}
	signedMsg.FullData = fullData
	return signedMsg, nil
}
