package validator

import (
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	model "github.com/ssvlabs/ssv/exporter/v2"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
)

type InMemTracer struct {
	sync.Mutex
	logger *zap.Logger
	// consider having the validator pubkey before of the slot
	validatorTraces map[uint64]map[string]*model.ValidatorDutyTrace
}

func NewTracer(logger *zap.Logger) *InMemTracer {
	return &InMemTracer{
		logger:          logger,
		validatorTraces: make(map[uint64]map[string]*model.ValidatorDutyTrace),
	}
}

func (n *InMemTracer) getTrace(slot uint64, vPubKey string) *model.ValidatorDutyTrace {
	mp, ok := n.validatorTraces[slot]
	if !ok {
		mp = make(map[string]*model.ValidatorDutyTrace)
		n.validatorTraces[slot] = mp
	}

	trace, ok := mp[vPubKey]
	if !ok {
		trace = new(model.ValidatorDutyTrace)
		mp[vPubKey] = trace
	}

	return trace
}

func (n *InMemTracer) getRound(trace *model.ValidatorDutyTrace, round uint64) *model.RoundTrace {
	var count = uint64(len(trace.Rounds))
	for round+1 > count {
		var r model.RoundTrace
		trace.Rounds = append(trace.Rounds, &r)
		count = uint64(len(trace.Rounds))
	}

	return trace.Rounds[round]
}

// id -> validator or committee id
func getOperators(id string) []spectypes.OperatorID {
	return []spectypes.OperatorID{}
}

func (n *InMemTracer) toMockState(msg *specqbft.Message, operatorIDs []spectypes.OperatorID) *specqbft.State {
	var mockState = &specqbft.State{}
	mockState.Height = msg.Height
	mockState.CommitteeMember = &spectypes.CommitteeMember{
		Committee: make([]*spectypes.Operator, 0, len(operatorIDs)),
	}
	for i := 0; i < len(operatorIDs); i++ {
		mockState.CommitteeMember.Committee = append(mockState.CommitteeMember.Committee, &spectypes.Operator{
			OperatorID: operatorIDs[i],
		})
	}
	return mockState
}

func decodeJustificationWithPrepares(justifications [][]byte) []*model.MessageTrace {
	var traces = make([]*model.MessageTrace, 0, len(justifications))
	for _, rcj := range justifications {
		var signedMsg = new(spectypes.SignedSSVMessage)
		err := signedMsg.Decode(rcj)
		if err != nil {
			// n.logger.Error("failed to decode round change justification", zap.Error(err))
		}

		var qbftMsg = new(specqbft.Message)
		err = qbftMsg.Decode(signedMsg.SSVMessage.GetData())
		if err != nil {
			// n.logger.Error("failed to decode round change justification", zap.Error(err))
		}

		var justificationTrace = new(model.MessageTrace)
		justificationTrace.Round = uint64(qbftMsg.Round)
		justificationTrace.BeaconRoot = qbftMsg.Root
		justificationTrace.Signer = signedMsg.OperatorIDs[0]
		traces = append(traces, justificationTrace)
	}

	return traces
}

func decodeJustificationWithRoundChanges(justifications [][]byte) []*model.RoundChangeTrace {
	var traces = make([]*model.RoundChangeTrace, 0, len(justifications))
	for _, rcj := range justifications {

		var signedMsg = new(spectypes.SignedSSVMessage)
		err := signedMsg.Decode(rcj)
		if err != nil {
			// n.logger.Error("failed to decode round change justification", zap.Error(err))
		}

		var qbftMsg = new(specqbft.Message)
		err = qbftMsg.Decode(signedMsg.SSVMessage.GetData())
		if err != nil {
			// n.logger.Error("failed to decode round change justification", zap.Error(err))
		}

		var receivedTime time.Time // we can't know the time when the sender received the message

		var roundChangeTrace = createRoundChangeTrace(qbftMsg, signedMsg, receivedTime)
		traces = append(traces, roundChangeTrace)
	}

	return traces
}

func createRoundChangeTrace(msg *specqbft.Message, signedMsg *spectypes.SignedSSVMessage, receivedTime time.Time) *model.RoundChangeTrace {
	var roundChangeTrace = new(model.RoundChangeTrace)
	roundChangeTrace.PreparedRound = uint64(msg.DataRound)
	roundChangeTrace.Round = uint64(msg.Round)
	roundChangeTrace.BeaconRoot = msg.Root
	roundChangeTrace.Signer = signedMsg.OperatorIDs[0]
	roundChangeTrace.ReceivedTime = receivedTime

	roundChangeTrace.PrepareMessages = decodeJustificationWithPrepares(msg.RoundChangeJustification)

	return roundChangeTrace
}

func createProposalTrace(msg *specqbft.Message, signedMsg *spectypes.SignedSSVMessage) *model.ProposalTrace {
	var proposalTrace = new(model.ProposalTrace)
	proposalTrace.Round = uint64(msg.Round)
	proposalTrace.BeaconRoot = msg.Root
	proposalTrace.Signer = signedMsg.OperatorIDs[0]
	proposalTrace.ReceivedTime = time.Now() // correct

	proposalTrace.RoundChanges = decodeJustificationWithRoundChanges(msg.RoundChangeJustification)
	proposalTrace.PrepareMessages = decodeJustificationWithPrepares(msg.PrepareJustification)

	return proposalTrace
}

func (n *InMemTracer) qbft(msg *specqbft.Message, signedMsg *spectypes.SignedSSVMessage) {
	slot := uint64(msg.Height)

	// validator pubkey
	msgID := spectypes.MessageID(msg.Identifier[:]) // validator + pubkey + role + network
	validatorPubKey := msgID.GetDutyExecutorID()    // validator pubkey or committee id
	validatorID := string(validatorPubKey)

	// get or create trace
	trace := n.getTrace(slot, validatorID)

	// first round is 1
	var round = n.getRound(trace, uint64(msg.Round))

	{ // proposer
		operatorIDs := getOperators(validatorID)
		if len(operatorIDs) > 0 {
			var mockState = n.toMockState(msg, operatorIDs)
			round.Proposer = specqbft.RoundRobinProposer(mockState, msg.Round)
		}
	}

	switch msg.MsgType {
	case specqbft.ProposalMsgType:
		var data = new(spectypes.ValidatorConsensusData)
		err := data.Decode(signedMsg.FullData)
		if err != nil {
			n.logger.Error("failed to decode proposal data", zap.Error(err))
		}
		// beacon vote (for committee duty)

		trace.Validator = data.Duty.ValidatorIndex
		round.ProposalTrace = createProposalTrace(msg, signedMsg)

	case specqbft.PrepareMsgType:
		var m = new(model.MessageTrace)
		m.Round = uint64(msg.Round)
		m.BeaconRoot = msg.Root
		m.Signer = signedMsg.OperatorIDs[0]
		m.ReceivedTime = time.Now() // TODO fixme

		round.Prepares = append(round.Prepares, m)

	case specqbft.CommitMsgType:
		var m = new(model.MessageTrace)
		m.Round = uint64(msg.Round)
		m.BeaconRoot = msg.Root
		m.Signer = signedMsg.OperatorIDs[0]
		m.ReceivedTime = time.Now() // TODO fixme

		round.Commits = append(round.Commits, m)

	case specqbft.RoundChangeMsgType:
		// optional - only if round change is proposing a value

		now := time.Now()
		roundChangeTrace := createRoundChangeTrace(msg, signedMsg, now)

		round.RoundChanges = append(round.RoundChanges, roundChangeTrace)
	}

	// n.validatorTraces = append(n.validatorTraces, model.ValidatorDutyTrace{})
}

func (n *InMemTracer) signed(msg *spectypes.PartialSignatureMessages) {
	slot := uint64(msg.Slot)

	fields := []zap.Field{
		fields.Slot(phase0.Slot(slot)),
	}

	validatorID := string("TODO")

	trace := n.getTrace(slot, validatorID)

	fields = append(fields, zap.Int("messages", len(msg.Messages)))
	fields = append(fields, zap.Int("duty rounds", len(trace.Rounds)))

	round := uint64(0) // TODO
	lastRound := n.getRound(trace, round)

	// Q: how to map Message to RoundTrace?
	for _, pSigMsg := range msg.Messages {
		lastRound.Proposer = pSigMsg.Signer
		_ = pSigMsg.ValidatorIndex
	}

	// Q: usage of msg.Type?
	switch msg.Type {
	case spectypes.PostConsensusPartialSig:
		fields = append(fields, zap.String("messageType", "post consensus"))
	case spectypes.ContributionProofs:
		fields = append(fields, zap.String("messageType", "contribution proofs"))
	case spectypes.RandaoPartialSig:
		fields = append(fields, zap.String("messageType", "randao"))
	case spectypes.SelectionProofPartialSig:
		fields = append(fields, zap.String("messageType", "selection proof"))
	case spectypes.ValidatorRegistrationPartialSig:
		fields = append(fields, zap.String("messageType", "validator registration"))
	case spectypes.VoluntaryExitPartialSig:
		fields = append(fields, zap.String("messageType", "voluntary exit"))
	}

	n.logger.Info("signed", fields...)
}

func (n *InMemTracer) Trace(msg *queue.SSVMessage) {
	switch msg.MsgType {
	case spectypes.SSVConsensusMsgType:
		if subMsg, ok := msg.Body.(*specqbft.Message); ok {
			n.qbft(subMsg, msg.SignedSSVMessage)
		}
	case spectypes.SSVPartialSignatureMsgType:
		pSigMessages := new(spectypes.PartialSignatureMessages)
		signedMsg := msg.SignedSSVMessage
		ssvMsg := signedMsg.SSVMessage

		_ = signedMsg.OperatorIDs
		_ = signedMsg.Signatures

		err := pSigMessages.Decode(ssvMsg.GetData())
		if err == nil {
			n.signed(pSigMessages)
		}
	}
}
