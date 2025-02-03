package validator

import (
	"encoding/hex"
	"sync"
	"time"

	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	model "github.com/ssvlabs/ssv/exporter/v2"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
)

type InMemTracer struct {
	sync.Mutex
	logger *zap.Logger
	// consider having the validator pubkey before of the slot
	validatorTraces map[uint64]map[string]*validatorDutyTrace
}

func NewTracer(logger *zap.Logger) *InMemTracer {
	return &InMemTracer{
		logger:          logger,
		validatorTraces: make(map[uint64]map[string]*validatorDutyTrace),
	}
}

func (n *InMemTracer) getTrace(slot uint64, vPubKey string) *validatorDutyTrace {
	n.Lock()
	defer n.Unlock()

	mp, ok := n.validatorTraces[slot]
	if !ok {
		mp = make(map[string]*validatorDutyTrace)
		n.validatorTraces[slot] = mp
	}

	trace, ok := mp[vPubKey]
	if !ok {
		trace = new(validatorDutyTrace)
		mp[vPubKey] = trace
	}

	return trace
}

func getRound(trace *validatorDutyTrace, round uint64) *round {
	trace.Lock()
	defer trace.Unlock()

	var count = len(trace.Rounds)
	for round+1 > uint64(count) { //nolint:gosec
		count = trace.addRound()
	}

	return trace.getRound(round)
}

// HOW TO? id -> validator or committee id
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
	var round = getRound(trace, uint64(msg.Round))
	round.Lock()
	defer round.Unlock()

	// proposer
	operatorIDs := getOperators(validatorID)
	if len(operatorIDs) > 0 {
		var mockState = n.toMockState(msg, operatorIDs)
		round.Proposer = specqbft.RoundRobinProposer(mockState, msg.Round)
	}

	switch msg.MsgType {
	case specqbft.ProposalMsgType:
		msgID := signedMsg.SSVMessage.GetID()
		switch msgID.GetRoleType() {
		case spectypes.RoleCommittee:
			n.logger.Info("qbft proposal for committee duty: to be implemented")
		default:
			var data = new(spectypes.ValidatorConsensusData)
			err := data.Decode(signedMsg.FullData)
			if err != nil {
				n.logger.Error("failed to decode proposal data", zap.Error(err))
			}
			// beacon vote (for committee duty)
			trace.Lock()
			trace.Validator = data.Duty.ValidatorIndex
			trace.Unlock()

			round.ProposalTrace = createProposalTrace(msg, signedMsg)
		}

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

func (n *InMemTracer) signed(msg *spectypes.PartialSignatureMessages, ssvMsg *queue.SSVMessage, validatorPubKey string) {
	slot := uint64(msg.Slot)

	trace := n.getTrace(slot, validatorPubKey)
	trace.Lock()
	defer trace.Unlock()

	if trace.Validator == 0 {
		trace.Validator = msg.Messages[0].ValidatorIndex
	}

	switch ssvMsg.MsgID.GetRoleType() {
	case spectypes.RoleCommittee:
		n.logger.Warn("unexpected committee duty") // we get this every slot
	case spectypes.RoleProposer:
		trace.Role = spectypes.BNRoleProposer
	case spectypes.RoleAggregator:
		trace.Role = spectypes.BNRoleAggregator
	case spectypes.RoleSyncCommitteeContribution:
		trace.Role = spectypes.BNRoleSyncCommitteeContribution
	case spectypes.RoleValidatorRegistration:
		trace.Role = spectypes.BNRoleValidatorRegistration
	case spectypes.RoleVoluntaryExit:
		trace.Role = spectypes.BNRoleVoluntaryExit
	}

	fields := []zap.Field{
		zap.Uint64("slot", slot),
		zap.String("validator", hex.EncodeToString([]byte(validatorPubKey[len(validatorPubKey)-4:]))),
		zap.String("type", trace.Role.String()),
	}

	n.logger.Info("signed", fields...)

	var tr model.PartialSigMessageTrace
	tr.Type = msg.Type
	tr.BeaconRoot = msg.Messages[0].SigningRoot
	tr.Signer = msg.Messages[0].Signer
	tr.ReceivedTime = time.Now()

	if msg.Type == spectypes.PostConsensusPartialSig {
		trace.Post = append(trace.Post, &tr)
		return
	}

	trace.Pre = append(trace.Pre, &tr)
}

func (n *InMemTracer) Trace(msg *queue.SSVMessage) {
	switch msg.MsgType {
	case spectypes.SSVConsensusMsgType:
		if subMsg, ok := msg.Body.(*specqbft.Message); ok {
			n.qbft(subMsg, msg.SignedSSVMessage)
		}
	case spectypes.SSVPartialSignatureMsgType:
		pSigMessages := new(spectypes.PartialSignatureMessages)
		err := pSigMessages.Decode(msg.SignedSSVMessage.SSVMessage.GetData())
		if err != nil {
			n.logger.Error("failed to decode partial signature messages", zap.Error(err))
			return
		}
		if msg.MsgID.GetRoleType() != spectypes.RoleCommittee {
			validatorPubKey := msg.MsgID.GetDutyExecutorID()
			n.signed(pSigMessages, msg, string(validatorPubKey))
		} else { // to be refined
			committeeID := msg.MsgID.GetDutyExecutorID()
			n.signed(pSigMessages, msg, string(committeeID))
		}
	}
}

type validatorDutyTrace struct {
	sync.Mutex
	model.ValidatorDutyTrace
}

func (t *validatorDutyTrace) addRound() int {
	t.Rounds = append(t.Rounds, &model.RoundTrace{})
	return len(t.Rounds)
}

func (t *validatorDutyTrace) getRound(rnd uint64) *round {
	r := t.Rounds[rnd]
	return &round{
		RoundTrace: *r,
	}
}

type round struct {
	sync.Mutex
	model.RoundTrace
}
