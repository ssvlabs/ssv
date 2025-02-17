package validator

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	// "github.com/herumi/bls-eth-go-binary/bls"
	"github.com/jellydator/ttlcache/v3"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	gc "github.com/ssvlabs/ssv/beacon/goclient"
	model "github.com/ssvlabs/ssv/exporter/v2"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"go.uber.org/zap"
)

type InMemTracer struct {
	sync.Mutex
	logger *zap.Logger
	// consider having the validator pubkey before of the slot
	validatorTraces *ttlcache.Cache[uint64, map[string]*validatorDutyTrace]
	committeeTraces *ttlcache.Cache[uint64, map[string]*committeeDutyTrace]
	domains         []phase0.Domain

	scRootsMu sync.Mutex
	// sync committee roots derived from the beacon vote that
	// belongs to the QBFT proposal message
	// TODO ever growing structure, manage space
	scRoots map[string]struct{}
}

func NewTracer(logger *zap.Logger, client *gc.GoClient) *InMemTracer {
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
		logger.Info("committee eviction", zap.Uint64("slot", item.Key()), zap.Int("len", len(item.Value())))
	})

	tracer.committeeTraces.OnInsertion(func(ctx context.Context, i *ttlcache.Item[uint64, map[string]*committeeDutyTrace]) {
		tracer.Lock()
		defer tracer.Unlock()
		for id := range i.Value() {
			logger.Info("committee insertion", zap.Uint64("slot", i.Key()), zap.String("id", hex.EncodeToString([]byte(id[16:]))))
		}
	})

	tracer.validatorTraces.OnEviction(func(_ context.Context, _ ttlcache.EvictionReason, item *ttlcache.Item[uint64, map[string]*validatorDutyTrace]) {
		logger.Info("eviction", zap.Uint64("slot", item.Key()), zap.Int("len", len(item.Value())))
	})

	tracer.validatorTraces.OnInsertion(func(ctx context.Context, i *ttlcache.Item[uint64, map[string]*validatorDutyTrace]) {
		tracer.Lock()
		defer tracer.Unlock()
		for id := range i.Value() {
			logger.Info("insertion", zap.Uint64("slot", i.Key()), zap.String("id", hex.EncodeToString([]byte(id[16:]))))
		}
	})

	go func() {
		for {
			<-time.After(time.Minute)
			tracer.committeeTraces.DeleteExpired()
			tracer.validatorTraces.DeleteExpired()
		}
	}()

	var epochs = []phase0.Epoch{} // TODO fill these up
	var domains = []phase0.DomainType{}

	// domains is formed from phase0.DomainType (which will be spectypes.DomainSyncCommittee) and an epoch
	// TODO get epoch to deneb and electra
	for _, epoch := range epochs {
		for _, domain := range domains {
			data, err := client.DomainData(epoch, domain)
			if err != nil {
				logger.Error("get domain data", zap.Error(err))
				continue
			}
			tracer.domains = append(tracer.domains, data)
		}
	}

	return tracer
}

func (n *InMemTracer) Store() DutyTraceStore {
	return &adapter{
		tracer: n,
		logger: n.logger,
	}
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

func (n *InMemTracer) decodeJustificationWithPrepares(justifications [][]byte) []*model.QBFTTrace {
	var traces = make([]*model.QBFTTrace, 0, len(justifications))
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

		var justificationTrace = new(model.QBFTTrace)
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

func (n *InMemTracer) processConsensus(msg *specqbft.Message, signedMsg *spectypes.SignedSSVMessage, round *model.RoundTrace) *model.DecidedTrace {
	switch msg.MsgType {
	case specqbft.ProposalMsgType:
		round.ProposalTrace = n.createProposalTrace(msg, signedMsg)

	case specqbft.PrepareMsgType:
		var m = new(model.QBFTTrace)
		m.Round = uint64(msg.Round)
		m.BeaconRoot = msg.Root
		m.Signer = signedMsg.OperatorIDs[0]
		m.ReceivedTime = time.Now() // TODO fixme

		round.Prepares = append(round.Prepares, m)

	case specqbft.CommitMsgType:
		if len(signedMsg.OperatorIDs) > 1 {
			return &model.DecidedTrace{
				Round:        uint64(msg.Round),
				BeaconRoot:   msg.Root,
				Signers:      signedMsg.OperatorIDs,
				ReceivedTime: time.Now(),
			}
		}

		var m = new(model.QBFTTrace)
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

	return nil
}

func (n *InMemTracer) processPartialSigValidator(msg *spectypes.PartialSignatureMessages, ssvMsg *queue.SSVMessage, validatorPubKey []byte) {
	var role spectypes.BeaconRole

	switch ssvMsg.MsgID.GetRoleType() {
	case spectypes.RoleCommittee:
		n.logger.Error("unexpected committee duty")
		return
	case spectypes.RoleProposer:
		role = spectypes.BNRoleProposer
	case spectypes.RoleAggregator:
		role = spectypes.BNRoleAggregator
	case spectypes.RoleSyncCommitteeContribution:
		role = spectypes.BNRoleSyncCommitteeContribution
	case spectypes.RoleValidatorRegistration:
		role = spectypes.BNRoleValidatorRegistration
	case spectypes.RoleVoluntaryExit:
		role = spectypes.BNRoleVoluntaryExit
	}

	slot := uint64(msg.Slot)
	trace := n.getValidatorTrace(slot, string(validatorPubKey))
	trace.Lock()
	defer trace.Unlock()

	trace.Role = role
	trace.Validator = msg.Messages[0].ValidatorIndex

	fields := []zap.Field{
		zap.String("pubkey", hex.EncodeToString(validatorPubKey)),
		zap.Uint64("slot", slot),
		zap.String("type", trace.Role.String()),
	}

	n.logger.Info("signed validator", fields...)

	var tr model.PartialSigTrace
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

	n.scRootsMu.Lock()
	defer n.scRootsMu.Unlock()

	for _, partialSigMsg := range msg.Messages {
		tr := new(model.SignerData)
		tr.Signers = append(tr.Signers, partialSigMsg.Signer) // aggregate messages so that it ends up a collection
		tr.ReceivedTime = time.Now()

		if _, found := n.scRoots[string(partialSigMsg.SigningRoot[:])]; found {
			trace.SyncCommittee = append(trace.SyncCommittee, tr)
			continue
		}

		trace.Attester = append(trace.Attester, tr)
	}
}

func (n *InMemTracer) fillInSyncCommitteeRoots(in []byte) error {
	n.scRootsMu.Lock()
	defer n.scRootsMu.Unlock()

	var bnVote = new(spectypes.BeaconVote)
	_ = bnVote.Decode(in)
	object := bnVote.BlockRoot

	for _, domain := range n.domains {
		root, err := spectypes.ComputeETHSigningRoot(spectypes.SSZBytes(object[:]), domain)
		if err != nil {
			return fmt.Errorf("compute eth signing root for domain: %w", err)
		}
		n.scRoots[string(root[:])] = struct{}{}
	}

	return nil
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

				oldC := len(trace.Rounds)
				round := trace.getRound(uint64(subMsg.Round))

				if round == nil {
					n.logger.Info("nil round at consensus committee",
						zap.Int("new rounds", len(trace.Rounds)),
						zap.Int("old rounds", oldC),
						zap.Uint64("subMsg.Round", uint64(subMsg.Round)))
					return
				}

				// in this step will fill in sync committee roots
				// to be later read in
				err := n.fillInSyncCommitteeRoots(msg.SignedSSVMessage.FullData)
				if err != nil {
					n.logger.Error("fill sync committee roots", zap.Error(err))
				}

				// TODO is this needed?
				operatorIDs := getOperators(executorID)
				if len(operatorIDs) > 0 {
					mockState := n.toMockState(subMsg, operatorIDs)
					round.Proposer = specqbft.RoundRobinProposer(mockState, subMsg.Round)
				}

				decided := n.processConsensus(subMsg, msg.SignedSSVMessage, round)
				if decided != nil {
					trace.Decideds = append(trace.Decideds, decided)
				}
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

				oldC := len(trace.Rounds)
				round := trace.getRound(uint64(subMsg.Round))

				if round == nil {
					n.logger.Info("nil round at consensus committee",
						zap.Int("new rounds", len(trace.Rounds)),
						zap.Int("old rounds", oldC),
						zap.Uint64("subMsg.Round", uint64(subMsg.Round)))
					return
				}

				// populate proposer
				operatorIDs := getOperators(executorID) // validator pubkey
				if len(operatorIDs) > 0 {
					var mockState = n.toMockState(subMsg, operatorIDs)
					round.Proposer = specqbft.RoundRobinProposer(mockState, subMsg.Round)
				}

				decided := n.processConsensus(subMsg, msg.SignedSSVMessage, round)
				if decided != nil {
					trace.Decideds = append(trace.Decideds, decided)
				}
			}
		}
	case spectypes.SSVPartialSignatureMsgType:
		pSigMessages := new(spectypes.PartialSignatureMessages)
		err := pSigMessages.Decode(msg.SignedSSVMessage.SSVMessage.GetData())
		if err != nil {
			n.logger.Error("failed to decode partial signature messages", zap.Error(err))
			return
		}

		// for _, msg := range pSigMessages.Messages {
		// 	sig, err := decodeSig(msg.PartialSignature)
		// 	if err != nil {
		// 		n.logger.Error("decode partial signature", zap.Error(err))
		// 		return
		// 	}

		// 	// TODO read from event syncer state (or something) talk to Moshe/Matus
		// 	var operatorKeySharePubkey []byte
		// 	err = types.VerifyReconstructedSignature(sig, operatorKeySharePubkey, msg.SigningRoot)
		// 	if err != nil {
		// 		n.logger.Error("bls verification failed", zap.Error(err))
		// 		 return
		// 	}
		// }

		executorID := msg.MsgID.GetDutyExecutorID()

		if msg.MsgID.GetRoleType() == spectypes.RoleCommittee {
			n.processPartialSigCommittee(pSigMessages, executorID)
			return
		}

		n.processPartialSigValidator(pSigMessages, msg, executorID)
	}
}

type validatorDutyTrace struct {
	sync.Mutex
	model.ValidatorDutyTrace
}

func (trace *validatorDutyTrace) getRound(rnd uint64) *model.RoundTrace {
	trace.Lock()
	defer trace.Unlock()

	var count = len(trace.Rounds)
	for rnd > uint64(count) { //nolint:gosec
		trace.Rounds = append(trace.Rounds, &model.RoundTrace{})
		count = len(trace.Rounds)
	}

	return trace.Rounds[rnd-1]
}

// committee

type committeeDutyTrace struct {
	sync.Mutex
	model.CommitteeDutyTrace
}

func (trace *committeeDutyTrace) getRound(rnd uint64) *model.RoundTrace {
	trace.Lock()
	defer trace.Unlock()

	var count = len(trace.Rounds)
	for rnd > uint64(count) { //nolint:gosec
		trace.Rounds = append(trace.Rounds, &model.RoundTrace{})
		count = len(trace.Rounds)
	}

	return trace.Rounds[rnd-1]
}

func (n *InMemTracer) getValidatorDuty(role spectypes.BeaconRole, slot phase0.Slot, pubkey spectypes.ValidatorPK) (*model.ValidatorDutyTrace, error) {
	n.Lock()
	defer n.Unlock()

	if !n.validatorTraces.Has(uint64(slot)) {
		return nil, errors.New("slot not found")
	}

	m := n.validatorTraces.Get(uint64(slot))

	trace, ok := m.Value()[string(pubkey[:])]
	if !ok {
		return nil, errors.New("validator not found")
	}

	trace.Lock()
	defer trace.Unlock()

	return &trace.ValidatorDutyTrace, nil
}

func (n *InMemTracer) getCommitteeDuty(slot phase0.Slot, committeeID spectypes.CommitteeID) (*model.CommitteeDutyTrace, error) {
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

// func decodeSig(in []byte) (*bls.Sign, error) {
// 	sign := new(bls.Sign)
// 	err := sign.Deserialize(in)
// 	return sign, err
// }
