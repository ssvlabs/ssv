package instance

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// uponProposal process proposal message
// Assumes proposal message is valid!
func (i *Instance) uponProposal(ctx context.Context, logger *zap.Logger, msg *specqbft.ProcessingMessage) error {
	logger = logger.With(zap.Any("proposal_signers", msg.SignedMessage.OperatorIDs))

	addedMsg, err := i.State.ProposeContainer.AddFirstMsgForSignerAndRound(msg)
	if err != nil {
		return fmt.Errorf("could not add proposal msg to container: %w", err)
	}
	if !addedMsg {
		return nil // uponProposal was already called
	}

	logger.Debug("📬 got proposal message")

	currentRound := i.State.Round
	msgRound := msg.QBFTMessage.Round

	i.metrics.EndStage(ctx, currentRound)
	i.metrics.StartStage(stagePrepare)

	// A future justified proposal should move us into the future round, hence we try to bump the round here.
	// Always move on to the message-round. The round-change message broadcast is a best-effort thing, the QBFT
	// cluster as a whole can progress further even if our round-change message cannot be created/broadcast
	// for whatever reason.
	i.bumpToRound(msgRound)

	i.State.ProposalAcceptedForCurrentRound = msg

	r := qbft.HashDataRoot(msg.SignedMessage.FullData)
	prepare, err := i.CreatePrepare(msgRound, r)
	if err != nil {
		return fmt.Errorf("could not create prepare msg: %w", err)
	}

	logger.Debug("📢 got proposal, broadcasting prepare message", zap.Any("prepare_signers", prepare.OperatorIDs))

	if err := i.Broadcast(prepare); err != nil {
		return fmt.Errorf("failed to broadcast prepare message: %w", err)
	}
	return nil
}

func (i *Instance) isValidProposal(msg *specqbft.ProcessingMessage) error {
	if msg.QBFTMessage.MsgType != specqbft.ProposalMsgType {
		return errors.New("msg type is not proposal")
	}
	if msg.QBFTMessage.Height != i.State.Height {
		return spectypes.WrapError(spectypes.WrongMessageHeightErrorCode, ErrWrongMsgHeight)
	}
	if len(msg.SignedMessage.OperatorIDs) != 1 {
		return spectypes.NewError(spectypes.MessageAllowsOneSignerOnlyErrorCode, "msg allows 1 signer")
	}

	if !msg.SignedMessage.CheckSignersInCommittee(i.State.CommitteeMember.Committee) {
		return spectypes.NewError(spectypes.SignerIsNotInCommitteeErrorCode, "signer not in committee")
	}

	if !msg.SignedMessage.MatchedSigners([]spectypes.OperatorID{i.ProposerForRound(msg.QBFTMessage.Round)}) {
		return spectypes.NewError(spectypes.ProposalLeaderInvalidErrorCode, "proposal leader invalid")
	}

	if err := msg.Validate(); err != nil {
		return spectypes.WrapError(spectypes.ProposalInvalidErrorCode, fmt.Errorf("proposal invalid: %w", err))
	}

	// verify full data integrity
	r := qbft.HashDataRoot(msg.SignedMessage.FullData)
	if !bytes.Equal(msg.QBFTMessage.Root[:], r[:]) {
		return spectypes.NewError(spectypes.RootHashInvalidErrorCode, "H(data) != root")
	}

	// get justifications
	roundChangeJustificationSignedMessages, _ := msg.QBFTMessage.GetRoundChangeJustifications() // no need to check error, checked on msg.SignedMessage.Validate()
	prepareJustificationSignedMessages, _ := msg.QBFTMessage.GetPrepareJustifications()         // no need to check error, checked on msg.SignedMessage.Validate()

	roundChangeJustification := make([]*specqbft.ProcessingMessage, 0)
	for _, rcSignedMessage := range roundChangeJustificationSignedMessages {
		rc, err := specqbft.NewProcessingMessage(rcSignedMessage)
		if err != nil {
			return fmt.Errorf("could not create ProcessingMessage from round change justification: %w", err)
		}
		roundChangeJustification = append(roundChangeJustification, rc)
	}
	prepareJustification := make([]*specqbft.ProcessingMessage, 0)
	for _, prepareSignedMessage := range prepareJustificationSignedMessages {
		procMsg, err := specqbft.NewProcessingMessage(prepareSignedMessage)
		if err != nil {
			return fmt.Errorf("could not create ProcessingMessage from prepare justification: %w", err)
		}
		prepareJustification = append(prepareJustification, procMsg)
	}

	if err := i.isProposalJustification(
		roundChangeJustification,
		prepareJustification,
		msg.QBFTMessage.Round,
		msg.SignedMessage.FullData,
	); err != nil {
		return fmt.Errorf("proposal not justified: %w", err)
	}

	if (i.State.ProposalAcceptedForCurrentRound == nil && msg.QBFTMessage.Round == i.State.Round) ||
		msg.QBFTMessage.Round > i.State.Round {
		return nil
	}
	return spectypes.NewError(spectypes.ProposalInvalidErrorCode, "proposal is not valid with current state")
}

// isProposalJustification returns nil if the proposal and round change messages are valid and justify a proposal message for the provided round, value and leader
func (i *Instance) isProposalJustification(
	roundChangeMsgs []*specqbft.ProcessingMessage,
	prepareMsgs []*specqbft.ProcessingMessage,
	round specqbft.Round,
	fullData []byte,
) error {
	if err := i.ValueChecker.CheckValue(fullData); err != nil {
		return fmt.Errorf("proposal fullData invalid: %w", err)
	}

	if round == specqbft.FirstRound {
		return nil
	}

	// check all round changes are valid for height and round
	// no quorum, duplicate signers,  invalid still has quorum, invalid no quorum
	// prepared
	for _, rc := range roundChangeMsgs {
		if err := i.validRoundChangeForDataVerifySignature(rc, round, fullData); err != nil {
			return fmt.Errorf("change round msg not valid: %w", err)
		}
	}

	// check there is a quorum
	if !specqbft.HasQuorum(i.State.CommitteeMember, roundChangeMsgs) {
		return spectypes.NewError(spectypes.RoundChangeNoQuorumErrorCode, "change round has no quorum")
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
		return fmt.Errorf("could not calculate if previously prepared: %w", err)
	}

	if !previouslyPrepared {
		return nil
	}

	// check prepare quorum
	if !specqbft.HasQuorum(i.State.CommitteeMember, prepareMsgs) {
		return errors.New("prepares has no quorum")
	}

	// get a round change data for which there is a justification for the highest previously prepared round
	rcMsg, err := highestPrepared(roundChangeMsgs)
	if err != nil {
		return fmt.Errorf("could not get highest prepared: %w", err)
	}
	if rcMsg == nil {
		return errors.New("no highest prepared")
	}

	// proposed fullData must equal highest prepared fullData
	r := qbft.HashDataRoot(fullData)
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
			return spectypes.NewError(spectypes.PrepareMessageInvalidErrorCode, "signed prepare not valid")
		}
	}
	return nil
}

func (i *Instance) Proposer() spectypes.OperatorID {
	return i.ProposerForRound(i.State.Round)
}

func (i *Instance) ProposerForRound(round specqbft.Round) spectypes.OperatorID {
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
func (i *Instance) CreateProposal(fullData []byte, roundChanges, prepares []*specqbft.ProcessingMessage) (*spectypes.SignedSSVMessage, error) {
	r := qbft.HashDataRoot(fullData)

	roundChangeSignedMessages := make([]*spectypes.SignedSSVMessage, 0, len(roundChanges))
	for _, msg := range roundChanges {
		roundChangeSignedMessages = append(roundChangeSignedMessages, msg.SignedMessage)
	}
	prepareSignedMessages := make([]*spectypes.SignedSSVMessage, 0, len(prepares))
	for _, msg := range prepares {
		prepareSignedMessages = append(prepareSignedMessages, msg.SignedMessage)
	}

	roundChangesData, err := specqbft.MarshalJustifications(roundChangeSignedMessages)
	if err != nil {
		return nil, fmt.Errorf("could not marshal justifications: %w", err)
	}
	preparesData, err := specqbft.MarshalJustifications(prepareSignedMessages)
	if err != nil {
		return nil, fmt.Errorf("could not marshal justifications: %w", err)
	}

	msg := &specqbft.Message{
		MsgType:    specqbft.ProposalMsgType,
		Height:     i.State.Height,
		Round:      i.State.Round,
		Identifier: i.State.ID,

		Root:                     r,
		RoundChangeJustification: roundChangesData,
		PrepareJustification:     preparesData,
	}

	signedMsg, err := ssvtypes.Sign(msg, i.State.CommitteeMember.OperatorID, i.signer)
	if err != nil {
		return nil, fmt.Errorf("could not wrap proposal message: %w", err)
	}
	signedMsg.FullData = fullData
	return signedMsg, nil
}
