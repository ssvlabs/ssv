package validator

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/jellydator/ttlcache/v3"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	gc "github.com/ssvlabs/ssv/beacon/goclient"
	model "github.com/ssvlabs/ssv/exporter/v2"
	"github.com/ssvlabs/ssv/exporter/v2/store"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

/*
TODO
- define expiration for duties per ROLE
- slot ticker instead of relying on cache eviction interval (be in sync with beacon node)
if bad perf - for performance we can use map/mutex instead of ttlcache

*/

type InMemTracer struct {
	sync.Mutex
	logger *zap.Logger

	// consider having the validator pubkey before of the slot
	validatorTraces *ttlcache.Cache[uint64, map[string]*validatorDutyTrace] // /traces/validator
	committeeTraces *ttlcache.Cache[uint64, map[string]*committeeDutyTrace] // /traces/committee

	store      *store.DutyTraceStore
	client     *gc.GoClient
	validators registrystorage.ValidatorStore
}

const (
	flushToDisk           = 2 * 12 * time.Second // two slots
	saveTTL               = 12 * time.Second
	mainnetDenebForkEpoch = 269568 // March 13, 2024, 13:55:35 UTC
)

func NewTracer(logger *zap.Logger, validators registrystorage.ValidatorStore, client *gc.GoClient, store *store.DutyTraceStore, shares registrystorage.Shares) *InMemTracer {
	tracer := &InMemTracer{
		logger: logger,
		validatorTraces: ttlcache.New(
			ttlcache.WithTTL[uint64, map[string]*validatorDutyTrace](flushToDisk),
		),
		committeeTraces: ttlcache.New(
			ttlcache.WithTTL[uint64, map[string]*committeeDutyTrace](flushToDisk),
		),
		client:     client,
		validators: validators,
		store:      store,
	}

	tracer.committeeTraces.OnEviction(func(_ context.Context, _ ttlcache.EvictionReason, item *ttlcache.Item[uint64, map[string]*committeeDutyTrace]) {
		var duties []*model.CommitteeDutyTrace
		for _, d := range item.Value() {
			duties = append(duties, &d.CommitteeDutyTrace)
		}
		err := store.SaveCommiteeDuties(phase0.Slot(item.Key()), duties)
		if err != nil {
			logger.Error("save committees duties to disk", zap.Uint64("slot", item.Key()), zap.Int("items", len(item.Value())))
			return
		}
		logger.Info("move to disk committee duties", zap.Uint64("slot", item.Key()), zap.Int("len", len(item.Value())))
	})

	tracer.committeeTraces.OnInsertion(func(ctx context.Context, i *ttlcache.Item[uint64, map[string]*committeeDutyTrace]) {
		// tracer.Lock()
		// defer tracer.Unlock()
		// for id := range i.Value() {
		// 	logger.Info("committee insertion", zap.Uint64("slot", i.Key()), zap.String("id", hex.EncodeToString([]byte(id))))
		// }
	})

	tracer.validatorTraces.OnEviction(func(_ context.Context, _ ttlcache.EvictionReason, item *ttlcache.Item[uint64, map[string]*validatorDutyTrace]) {
		logger.Info("eviction", zap.Uint64("slot", item.Key()), zap.Int("len", len(item.Value())))
	})

	tracer.validatorTraces.OnInsertion(func(ctx context.Context, i *ttlcache.Item[uint64, map[string]*validatorDutyTrace]) {
		tracer.Lock()
		defer tracer.Unlock()
		for id := range i.Value() {
			logger.Info("insertion", zap.Uint64("slot", i.Key()), zap.String("id", hex.EncodeToString([]byte(id))))
		}
	})

	// TODO:me replace with slotticker
	go func() {
		for {
			<-time.After(saveTTL)
			tracer.committeeTraces.DeleteExpired()
			tracer.validatorTraces.DeleteExpired()
		}
	}()

	return tracer
}

func (n *InMemTracer) Store() DutyTraceStore {
	return &adapter{
		tracer: n,
		logger: n.logger,
	}
}

// func (n *InMemTracer) domainData(epoch phase0.Epoch, typ phase0.DomainType) { //TODO:me ttl cache for domain data
// 	// cache by epoch

// 	var epochs = []phase0.Epoch{mainnetDenebForkEpoch} // TODO fill these up
// 	var domains = []phase0.DomainType{}

// 	// domains is formed from phase0.DomainType (which will be spectypes.DomainSyncCommittee) and an epoch
// 	// TODO get epoch to deneb and electra
// 	for _, domain := range domains {
// 		for _, epoch := range epochs {
// 			data, err := n.client.DomainData(epoch, domain)
// 			if err != nil {
// 				logger.Error("get domain data", zap.Error(err))
// 				continue
// 			}
// 			tracer.domains = append(tracer.domains, data)
// 		}
// 	}
// }

func (n *InMemTracer) getOrCreateValidatorTrace(slot uint64, vPubKey string, role spectypes.RunnerRole) *validatorDutyTrace {
	n.Lock()
	defer n.Unlock()

	mp, _ := n.validatorTraces.GetOrSet(slot, make(map[string]*validatorDutyTrace))

	trace, ok := mp.Value()[vPubKey]
	if !ok {
		trace = new(validatorDutyTrace)

		var bnRole spectypes.BeaconRole

		switch role {
		case spectypes.RoleCommittee:
			panic("unexpected committee role")
		case spectypes.RoleProposer:
			bnRole = spectypes.BNRoleProposer
		case spectypes.RoleAggregator:
			bnRole = spectypes.BNRoleAggregator
		case spectypes.RoleSyncCommitteeContribution:
			bnRole = spectypes.BNRoleSyncCommitteeContribution
		case spectypes.RoleValidatorRegistration:
			bnRole = spectypes.BNRoleValidatorRegistration
		case spectypes.RoleVoluntaryExit:
			bnRole = spectypes.BNRoleVoluntaryExit
		}

		trace.ValidatorDutyTrace.Role = bnRole
		mp.Value()[vPubKey] = trace
	}

	return trace
}

func (n *InMemTracer) getOrCreateCommitteeTrace(slot uint64, committeeID []byte) *committeeDutyTrace {
	n.Lock()
	defer n.Unlock()

	mp, _ := n.committeeTraces.GetOrSet(slot, make(map[string]*committeeDutyTrace))

	trace, ok := mp.Value()[string(committeeID)]
	if !ok {
		trace = new(committeeDutyTrace)
		trace.CommitteeID = spectypes.CommitteeID(committeeID)
		mp.Value()[string(committeeID)] = trace
	}

	return trace
}

// TODO: HOW TO? id -> validator or committee id
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

		var justificationTrace model.QBFTTrace
		justificationTrace.Round = uint64(qbftMsg.Round)
		justificationTrace.BeaconRoot = qbftMsg.Root
		justificationTrace.Signer = signedMsg.OperatorIDs[0]
		traces = append(traces, &justificationTrace)
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
	trace := n.getOrCreateValidatorTrace(slot, string(validatorPubKey), ssvMsg.MsgID.GetRoleType())
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

	trace := n.getOrCreateCommitteeTrace(slot, committeeID)
	trace.Lock()
	defer trace.Unlock()

	trace.CommitteeID = [32]byte(committeeID[:])
	trace.OperatorIDs = getOperators(committeeID) // TODO confirm is needed
	trace.Slot = msg.Slot

	for _, partialSigMsg := range msg.Messages {
		var tr model.SignerData
		tr.Signers = append(tr.Signers, partialSigMsg.Signer)
		tr.ReceivedTime = time.Now()

		if bytes.Equal(trace.syncCommitteeRoot[:], partialSigMsg.SigningRoot[:]) {
			trace.SyncCommittee = append(trace.SyncCommittee, &tr)
			continue
		}

		// TODO:me (electra) - implement a check for: attestation is correct instead of assuming
		trace.Attester = append(trace.Attester, &tr)
	}
}

// func (n *InMemTracer) fillInSyncCommitteeRoots(trace *committeeDutyTrace, in []byte, slot phase0.Slot, validator phase0.ValidatorIndex) error {
// 	var bnVote = new(spectypes.BeaconVote)
// 	_ = bnVote.Decode(in)
// 	object := bnVote.BlockRoot

// 	// TODO:me adapt acordingly, check other exporter code
// 	syncMsg := &altair.SyncCommitteeMessage{
// 		Slot:            slot,
// 		BeaconBlockRoot: beaconVote.BlockRoot,
// 		ValidatorIndex:  validatorDuty.ValidatorIndex,
// 	}

// 	// Root
// 	domain, err := cr.GetBeaconNode().DomainData(epoch, spectypes.DomainSyncCommittee)
// 	if err != nil {
// 		logger.Debug("failed to get sync committee domain", zap.Error(err))
// 		continue
// 	}
// 	// Eth root
// 	blockRoot := spectypes.SSZBytes(beaconVote.BlockRoot[:])
// 	root, err := spectypes.ComputeETHSigningRoot(blockRoot, domain)
// 	if err != nil {
// 		n.logger.Debug("failed to compute sync committee root", zap.Error(err))
// 		return
// 	}

// 	root, err := spectypes.ComputeETHSigningRoot(spectypes.SSZBytes(object[:]), phase0.Domain(spectypes.DomainSyncCommittee))
// 	if err != nil {
// 		return fmt.Errorf("compute eth signing root for domain: %w", err)
// 	}

// 	trace.syncCommitteeRoot = root
// 	return nil
// }

func (n *InMemTracer) Trace(msg *queue.SSVMessage) {
	switch msg.MsgType {
	case spectypes.SSVConsensusMsgType:
		if subMsg, ok := msg.Body.(*specqbft.Message); ok {
			slot := uint64(subMsg.Height)
			msgID := spectypes.MessageID(subMsg.Identifier[:]) // validator + pubkey + role + network
			executorID := msgID.GetDutyExecutorID()            // validator pubkey or committee id

			switch role := msgID.GetRoleType(); role {
			case spectypes.RoleCommittee:

				committeeID := executorID[16:]
				trace := n.getOrCreateCommitteeTrace(slot, committeeID) // committe id

				trace.Lock()
				defer trace.Unlock()

				// first step fill in sync committee roots
				// to be later read in 'processPartialSigCommittee'
				// err := n.fillInSyncCommitteeRoots(trace, msg.Slot, msg.SignedSSVMessage.FullData)
				// if err != nil {
				// 	n.logger.Error("fill sync committee roots", zap.Error(err))
				// }

				round := trace.getOrCreateRound(uint64(subMsg.Round))

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
				trace := n.getOrCreateValidatorTrace(slot, validatorPubKey, role)

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

				trace.Lock()
				defer trace.Unlock()

				round := trace.getOrCreateRound(uint64(subMsg.Round))

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

		for _, msg := range pSigMessages.Messages {
			sig, err := decodeSig(msg.PartialSignature)
			if err != nil {
				n.logger.Error("decode partial signature", zap.Error(err))
				continue
			}

			share, found := n.validators.ValidatorByIndex(msg.ValidatorIndex)
			if !found {
				// log
				continue
			}

			signerIndex := slices.IndexFunc(share.Committee, func(e *spectypes.ShareMember) bool {
				return e.Signer == msg.Signer
			})

			if signerIndex == -1 {
				n.logger.Warn("operator key share not found", fields.OperatorID(msg.Signer))
				continue
			}

			sharePubkey := share.Committee[signerIndex].SharePubKey

			err = types.VerifyReconstructedSignature(sig, sharePubkey, msg.SigningRoot)
			if err != nil {
				n.logger.Error("bls verification failed", zap.Error(err))
				return
			}
		}

		executorID := msg.MsgID.GetDutyExecutorID()

		if msg.MsgID.GetRoleType() == spectypes.RoleCommittee {
			committeeID := executorID[16:]
			n.processPartialSigCommittee(pSigMessages, committeeID)
			return
		}

		n.processPartialSigValidator(pSigMessages, msg, executorID)
	}
}

type validatorDutyTrace struct {
	sync.Mutex
	model.ValidatorDutyTrace
}

func (trace *validatorDutyTrace) getOrCreateRound(rnd uint64) *model.RoundTrace {
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
	syncCommitteeRoot phase0.Root
	model.CommitteeDutyTrace
}

func (trace *committeeDutyTrace) getOrCreateRound(rnd uint64) *model.RoundTrace {
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

	m := n.committeeTraces.Get(uint64(slot))

	if m == nil {
		trace, err := n.store.GetCommitteeDuty(slot, committeeID)
		if err != nil {
			return nil, fmt.Errorf("get committee duty from disk: %w", err)
		}

		if trace != nil {
			return trace, nil
		}

		return nil, errors.New("slot not found")
	}

	trace, ok := m.Value()[string(committeeID[:])]
	if !ok {
		return nil, errors.New("committe ID not found: " + hex.EncodeToString(committeeID[:]))
	}

	trace.Lock()
	defer trace.Unlock()

	return &trace.CommitteeDutyTrace, nil
}

func decodeSig(in []byte) (*bls.Sign, error) {
	sign := new(bls.Sign)
	err := sign.Deserialize(in)
	return sign, err
}
