package validator

import (
	"context"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	model "github.com/ssvlabs/ssv/exporter/v2"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
)

type InMemTracer struct {
	sync.Mutex
	logger *zap.Logger
	// consider having the validator pubkey before of the slot
	validatorTraces *ttlcache.Cache[uint64, map[string]*validatorDutyTrace]
	committeeTraces *ttlcache.Cache[uint64, map[string]*committeeDutyTrace]
}

func NewTracer(logger *zap.Logger) *InMemTracer {
	tracer := &InMemTracer{
		logger: logger,
		validatorTraces: ttlcache.New(
			ttlcache.WithTTL[uint64, map[string]*validatorDutyTrace](3 * time.Minute),
		),
		committeeTraces: ttlcache.New(
			ttlcache.WithTTL[uint64, map[string]*committeeDutyTrace](3 * time.Minute),
		),
	}

	tracer.committeeTraces.OnEviction(func(_ context.Context, _ ttlcache.EvictionReason, item *ttlcache.Item[uint64, map[string]*committeeDutyTrace]) {
		logger.Info("eviction", zap.Uint64("slot", item.Key()), zap.Int("len", len(item.Value())))
	})

	go func() {
		for {
			<-time.After(time.Minute)
			tracer.committeeTraces.DeleteExpired()
		}
	}()

	return tracer
}

func (n *InMemTracer) getValidatorTrace(slot uint64, vPubKey string) *validatorDutyTrace {
	n.Lock()
	defer n.Unlock()

	mp, _ := n.validatorTraces.GetOrSet(slot, make(map[string]*validatorDutyTrace))

	trace, ok := mp.Value()[vPubKey]
	if !ok {
		trace = new(validatorDutyTrace)
		mp.Value()[vPubKey] = trace
	}

	return trace
}

func (n *InMemTracer) getCommitteeTrace(slot uint64, committeeID []byte) *committeeDutyTrace {
	n.Lock()
	defer n.Unlock()

	mp, _ := n.committeeTraces.GetOrSet(slot, make(map[string]*committeeDutyTrace))

	trace, ok := mp.Value()[string(committeeID)]
	if !ok {
		trace = new(committeeDutyTrace)
		mp.Value()[string(committeeID)] = trace
	}

	return trace
}

// HOW TO? id -> validator or committee id
func getOperators([]byte) []spectypes.OperatorID {
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

func (n *InMemTracer) decodeJustificationWithPrepares(justifications [][]byte) []*model.MessageTrace {
	var traces = make([]*model.MessageTrace, 0, len(justifications))
	for _, rcj := range justifications {
		var signedMsg = new(spectypes.SignedSSVMessage)
		err := signedMsg.Decode(rcj)
		if err != nil {
			n.logger.Error("failed to decode round change justification", zap.Error(err))
			continue
		}

		var qbftMsg = new(specqbft.Message)
		err = qbftMsg.Decode(signedMsg.SSVMessage.GetData())
		if err != nil {
			n.logger.Error("failed to decode round change justification", zap.Error(err))
			continue
		}

		var justificationTrace = new(model.MessageTrace)
		justificationTrace.Round = uint64(qbftMsg.Round)
		justificationTrace.BeaconRoot = qbftMsg.Root
		justificationTrace.Signer = signedMsg.OperatorIDs[0]
		traces = append(traces, justificationTrace)
	}

	return traces
}

func (n *InMemTracer) decodeJustificationWithRoundChanges(justifications [][]byte) []*model.RoundChangeTrace {
	var traces = make([]*model.RoundChangeTrace, 0, len(justifications))
	for _, rcj := range justifications {
		var signedMsg = new(spectypes.SignedSSVMessage)
		err := signedMsg.Decode(rcj)
		if err != nil {
			n.logger.Error("failed to decode round change justification", zap.Error(err))
			continue
		}

		var qbftMsg = new(specqbft.Message)
		err = qbftMsg.Decode(signedMsg.SSVMessage.GetData())
		if err != nil {
			n.logger.Error("failed to decode round change justification", zap.Error(err))
			continue
		}

		var receivedTime time.Time // zero value because we can't know the time when the sender received the message

		var roundChangeTrace = n.createRoundChangeTrace(qbftMsg, signedMsg, receivedTime)
		traces = append(traces, roundChangeTrace)
	}

	return traces
}

func (n *InMemTracer) createRoundChangeTrace(msg *specqbft.Message, signedMsg *spectypes.SignedSSVMessage, receivedTime time.Time) *model.RoundChangeTrace {
	var roundChangeTrace = new(model.RoundChangeTrace)
	roundChangeTrace.PreparedRound = uint64(msg.DataRound)
	roundChangeTrace.Round = uint64(msg.Round)
	roundChangeTrace.BeaconRoot = msg.Root
	roundChangeTrace.Signer = signedMsg.OperatorIDs[0]
	roundChangeTrace.ReceivedTime = receivedTime

	roundChangeTrace.PrepareMessages = n.decodeJustificationWithPrepares(msg.RoundChangeJustification)

	return roundChangeTrace
}

func (n *InMemTracer) createProposalTrace(msg *specqbft.Message, signedMsg *spectypes.SignedSSVMessage) *model.ProposalTrace {
	var proposalTrace = new(model.ProposalTrace)
	proposalTrace.Round = uint64(msg.Round)
	proposalTrace.BeaconRoot = msg.Root
	proposalTrace.Signer = signedMsg.OperatorIDs[0]
	proposalTrace.ReceivedTime = time.Now() // correct

	proposalTrace.RoundChanges = n.decodeJustificationWithRoundChanges(msg.RoundChangeJustification)
	proposalTrace.PrepareMessages = n.decodeJustificationWithPrepares(msg.PrepareJustification)

	return proposalTrace
}

func (n *InMemTracer) processConsensus(msg *specqbft.Message, signedMsg *spectypes.SignedSSVMessage, round *round) {
	round.Lock()
	defer round.Unlock()

	switch msg.MsgType {
	case specqbft.ProposalMsgType:
		round.ProposalTrace = n.createProposalTrace(msg, signedMsg)

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
		now := time.Now()
		roundChangeTrace := n.createRoundChangeTrace(msg, signedMsg, now)

		round.RoundChanges = append(round.RoundChanges, roundChangeTrace)
	}
}

func (n *InMemTracer) processPartialSigValidator(msg *spectypes.PartialSignatureMessages, ssvMsg *queue.SSVMessage, validatorPubKey []byte) {
	slot := uint64(msg.Slot)

	trace := n.getValidatorTrace(slot, string(validatorPubKey))
	trace.Lock()
	defer trace.Unlock()

	if trace.Validator == 0 {
		trace.Validator = msg.Messages[0].ValidatorIndex
	}

	switch ssvMsg.MsgID.GetRoleType() {
	case spectypes.RoleCommittee:
		n.logger.Error("unexpected committee duty")
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
		zap.String("validator", hex.EncodeToString(validatorPubKey[len(validatorPubKey)-4:])),
		zap.Uint64("slot", slot),
		zap.String("type", trace.Role.String()),
	}

	n.logger.Info("signed validator", fields...)

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

func (n *InMemTracer) processPartialSigCommittee(msg *spectypes.PartialSignatureMessages, committeeID []byte) {
	slot := uint64(msg.Slot)

	trace := n.getCommitteeTrace(slot, committeeID)
	trace.Lock()
	defer trace.Unlock()

	trace.CommitteeID = [32]byte(committeeID[:])
	trace.OperatorIDs = getOperators(committeeID)
	trace.Slot = msg.Slot

	var cTrace = new(model.CommitteePartialSigMessageTrace)
	cTrace.Type = msg.Type
	cTrace.Signer = msg.Messages[0].Signer
	cTrace.ReceivedTime = time.Now()

	for _, partialSigMsg := range msg.Messages {
		tr := new(model.PartialSigMessage)
		tr.BeaconRoot = partialSigMsg.SigningRoot
		tr.Signer = partialSigMsg.Signer
		tr.ValidatorIndex = partialSigMsg.ValidatorIndex
		cTrace.Messages = append(cTrace.Messages, tr)
	}

	trace.Post = append(trace.Post, cTrace)

	trace.Decideds = append(trace.Decideds, &model.DecidedTrace{
		MessageTrace: model.MessageTrace{
			Round:        0,
			BeaconRoot:   msg.Messages[0].SigningRoot, // Matheus:TODO: check if this is correct
			Signer:       msg.Messages[0].Signer,
			ReceivedTime: time.Now(),
		},
		Signers: []spectypes.OperatorID{msg.Messages[0].Signer}, // Matheus: WIP
	})
}

func (n *InMemTracer) Trace(msg *queue.SSVMessage) {
	switch msg.MsgType {
	case spectypes.SSVConsensusMsgType:
		if subMsg, ok := msg.Body.(*specqbft.Message); ok {
			slot := uint64(subMsg.Height)
			msgID := spectypes.MessageID(subMsg.Identifier[:]) // validator + pubkey + role + network
			executorID := msgID.GetDutyExecutorID()            // validator pubkey or committee id

			switch msgID.GetRoleType() {
			case spectypes.RoleCommittee:
				trace := n.getCommitteeTrace(slot, executorID) // committe id
				round := trace.getRound(uint64(subMsg.Round))

				func() { // populate proposer
					operatorIDs := getOperators(executorID)
					if len(operatorIDs) > 0 {
						mockState := n.toMockState(subMsg, operatorIDs)
						round.Lock()
						defer round.Unlock()
						round.Proposer = specqbft.RoundRobinProposer(mockState, subMsg.Round)
					}
				}()

				n.processConsensus(subMsg, msg.SignedSSVMessage, round)
			default:
				validatorPubKey := string(executorID)
				trace := n.getValidatorTrace(slot, validatorPubKey)

				if msg.MsgType == spectypes.SSVConsensusMsgType {
					var qbftMsg specqbft.Message
					err := qbftMsg.Decode(msg.Data)
					if err != nil {
						n.logger.Error("failed to decode validator consensus data", zap.Error(err))
						return
					}

					if qbftMsg.MsgType == specqbft.ProposalMsgType {
						stop := func() bool {
							trace.Lock()
							defer trace.Unlock()

							var data = new(spectypes.ValidatorConsensusData)
							err := data.Decode(msg.SignedSSVMessage.FullData)
							if err != nil {
								n.logger.Error("failed to decode validator proposal data", zap.Error(err))
								return true
							}

							trace.Validator = data.Duty.ValidatorIndex
							return false
						}()

						if stop {
							return
						}
					}
				}

				round := trace.getRound(uint64(subMsg.Round))
				// populate proposer
				operatorIDs := getOperators(executorID) // validator pubkey
				if len(operatorIDs) > 0 {
					var mockState = n.toMockState(subMsg, operatorIDs)
					round.Lock()
					defer round.Unlock()
					round.Proposer = specqbft.RoundRobinProposer(mockState, subMsg.Round)
				}

				n.processConsensus(subMsg, msg.SignedSSVMessage, round)
			}
		}
	case spectypes.SSVPartialSignatureMsgType:
		pSigMessages := new(spectypes.PartialSignatureMessages)
		err := pSigMessages.Decode(msg.SignedSSVMessage.SSVMessage.GetData())
		if err != nil {
			n.logger.Error("failed to decode partial signature messages", zap.Error(err))
			return
		}

		executorID := msg.MsgID.GetDutyExecutorID()

		if msg.MsgID.GetRoleType() == spectypes.RoleCommittee {
			n.processPartialSigCommittee(pSigMessages, executorID)
			return
		}

		n.processPartialSigValidator(pSigMessages, msg, executorID)
	}
}

type round struct {
	sync.Mutex
	*model.RoundTrace
}

type validatorDutyTrace struct {
	sync.Mutex
	model.ValidatorDutyTrace
}

func (trace *validatorDutyTrace) getRound(rnd uint64) *round {
	trace.Lock()
	defer trace.Unlock()

	var count = len(trace.Rounds)
	for rnd > uint64(count) { //nolint:gosec
		trace.Rounds = append(trace.Rounds, &model.RoundTrace{})
		count = len(trace.Rounds)
	}

	r := trace.Rounds[rnd-1]
	return &round{
		RoundTrace: r,
	}
}

// committee

type committeeDutyTrace struct {
	sync.Mutex
	model.CommitteeDutyTrace
}

func (trace *committeeDutyTrace) getRound(rnd uint64) *round {
	trace.Lock()
	defer trace.Unlock()

	var count = len(trace.Rounds)
	for rnd > uint64(count) { //nolint:gosec
		trace.Rounds = append(trace.Rounds, &model.RoundTrace{})
		count = len(trace.Rounds)
	}

	return &round{
		RoundTrace: trace.Rounds[rnd-1],
	}
}

// tmp hack

func (n *InMemTracer) GetCommitteeDuty(slot phase0.Slot, committeeID spectypes.CommitteeID) (*model.CommitteeDutyTrace, error) {
	n.Lock()
	defer n.Unlock()

	if !n.committeeTraces.Has(uint64(slot)) {
		return nil, errors.New("slot not found")
	}

	m := n.committeeTraces.Get(uint64(slot))

	var mapID [48]byte
	copy(mapID[16:], committeeID[:])

	trace, ok := m.Value()[string(mapID[:])]
	if !ok {
		return nil, errors.New("committe ID not found: " + hex.EncodeToString(mapID[:]))
	}

	trace.Lock()
	defer trace.Unlock()

	return &trace.CommitteeDutyTrace, nil
}
