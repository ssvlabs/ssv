package validator

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
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
	"github.com/ssvlabs/ssv/operator/slotticker"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

/*
TODO
ask
where to get vald index for migration
how to do root sync comm
double check do we really need bls verification


- define expiration for duties per ROLE
if bad perf - for performance we can use map/mutex instead of ttlcache
*/

type InMemTracer struct {
	logger *zap.Logger

	validatorCommitteeMappingMu sync.Mutex
	validatorCommitteeMapping   *ttlcache.Cache[phase0.Slot, map[phase0.ValidatorIndex]spectypes.CommitteeID]

	validatorTracesMu sync.Mutex
	validatorTraces   *ttlcache.Cache[uint64, map[spectypes.ValidatorPK][]*validatorDutyTrace] // /traces/validator

	committeeTracesMu sync.Mutex
	committeeTraces   *ttlcache.Cache[uint64, map[spectypes.CommitteeID]*committeeDutyTrace] // /traces/committee

	store      *store.DutyTraceStore
	client     *gc.GoClient
	validators registrystorage.ValidatorStore
}

const flushToDisk = 2 * 12 * time.Second // two slots

func NewTracer(ctx context.Context, logger *zap.Logger, ticker slotticker.SlotTicker, validators registrystorage.ValidatorStore, client *gc.GoClient, store *store.DutyTraceStore) *InMemTracer {
	tracer := &InMemTracer{
		logger: logger,
		validatorTraces: ttlcache.New(
			ttlcache.WithTTL[uint64, map[spectypes.ValidatorPK][]*validatorDutyTrace](flushToDisk),
		),
		committeeTraces: ttlcache.New(
			ttlcache.WithTTL[uint64, map[spectypes.CommitteeID]*committeeDutyTrace](flushToDisk),
		),
		validatorCommitteeMapping: ttlcache.New(
			ttlcache.WithTTL[phase0.Slot, map[phase0.ValidatorIndex]spectypes.CommitteeID](flushToDisk),
		),
		store:      store,
		client:     client,
		validators: validators,
	}

	// every slot check which duties are older than 2 slots
	// and move them from cache to disk
	go func() {
		logger.Info("start duty tracer cache to disk evictor")
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.Next():
				tracer.validatorCommitteeMapping.DeleteExpired()
				tracer.validatorTraces.DeleteExpired()
				tracer.committeeTraces.DeleteExpired()
			}
		}
	}()

	tracer.validatorCommitteeMapping.OnEviction(func(_ context.Context, _ ttlcache.EvictionReason, item *ttlcache.Item[phase0.Slot, map[phase0.ValidatorIndex]spectypes.CommitteeID]) {
		tracer.validatorCommitteeMappingMu.Lock()
		defer tracer.validatorCommitteeMappingMu.Unlock()

		err := store.SaveCommitteeDutyLinks(item.Key(), item.Value())
		if err != nil {
			logger.Error("save committee duty links to disk", zap.Error(err))
			return
		}

		logger.Info("save committee duty links to disk", fields.Slot(item.Key()), zap.Int("items", len(item.Value())))
	})

	tracer.committeeTraces.OnEviction(func(_ context.Context, _ ttlcache.EvictionReason, item *ttlcache.Item[uint64, map[spectypes.CommitteeID]*committeeDutyTrace]) {
		tracer.committeeTracesMu.Lock()
		defer tracer.committeeTracesMu.Unlock()

		var duties []*model.CommitteeDutyTrace
		for _, d := range item.Value() {
			duties = append(duties, &d.CommitteeDutyTrace)
		}
		err := store.SaveCommitteeDuties(phase0.Slot(item.Key()), duties)
		if err != nil {
			logger.Error("save committee duties to disk", zap.Uint64("slot", item.Key()), zap.Int("items", len(duties)))
			return
		}
		logger.Info("save committee duties to disk", zap.Uint64("slot", item.Key()), zap.Int("len", len(item.Value())))
	})

	tracer.committeeTraces.OnInsertion(func(ctx context.Context, i *ttlcache.Item[uint64, map[spectypes.CommitteeID]*committeeDutyTrace]) {
		// tracer.Lock()
		// defer tracer.Unlock()
		// for id := range i.Value() {
		// 	logger.Info("committee insertion", zap.Uint64("slot", i.Key()), zap.String("id", hex.EncodeToString([]byte(id))))
		// }
	})

	tracer.validatorTraces.OnEviction(func(_ context.Context, _ ttlcache.EvictionReason, item *ttlcache.Item[uint64, map[spectypes.ValidatorPK][]*validatorDutyTrace]) {
		tracer.validatorTracesMu.Lock()
		defer tracer.validatorTracesMu.Unlock()

		var duties []*model.ValidatorDutyTrace
		for pk, traces := range item.Value() {
			for _, trace := range traces {
				// TODO: confirm it makes sense
				// in case some duties do not have the validator index set
				if trace.Validator == 0 {
					index, found := tracer.validators.ValidatorIndex(pk)
					if !found {
						logger.Error("no validator index", fields.Validator(pk[:]))
						continue
					}
					trace.Validator = index
				}
				logger.Info("validator", zap.Uint64("slot", item.Key()), zap.Uint64("index", uint64(trace.Validator)), fields.BeaconRole(trace.Role), fields.Validator(pk[:]))
				duties = append(duties, &trace.ValidatorDutyTrace)
			}
		}
		if err := store.SaveValidatorDuties(duties); err != nil {
			logger.Error("save validator duties to disk", zap.Uint64("slot", item.Key()), zap.Int("items", len(duties)))
			return
		}
		logger.Info("save validator duties to disk", zap.Uint64("slot", item.Key()), zap.Int("len", len(item.Value())))
	})

	tracer.validatorTraces.OnInsertion(func(ctx context.Context, i *ttlcache.Item[uint64, map[spectypes.ValidatorPK][]*validatorDutyTrace]) {
		// tracer.Lock()
		// defer tracer.Unlock()
		// for id := range i.Value() {
		// 	logger.Info("insertion", zap.Uint64("slot", i.Key()), zap.String("id", hex.EncodeToString([]byte(id))))
		// }
	})

	return tracer
}

func (n *InMemTracer) Store() DutyTraceStore {
	return &adapter{
		tracer: n,
	}
}

func (n *InMemTracer) getOrCreateValidatorTrace(slot uint64, role spectypes.BeaconRole, vPubKey spectypes.ValidatorPK) *validatorDutyTrace {
	mp, _ := n.validatorTraces.GetOrSet(slot, make(map[spectypes.ValidatorPK][]*validatorDutyTrace))

	n.validatorTracesMu.Lock()
	defer n.validatorTracesMu.Unlock()

	traces, found := mp.Value()[vPubKey]

	if !found {
		trace := &validatorDutyTrace{
			ValidatorDutyTrace: model.ValidatorDutyTrace{
				Slot: phase0.Slot(slot),
				Role: role,
			},
		}
		mp.Value()[vPubKey] = append(traces, trace)
		return trace
	}

	// find the trace for the role
	for _, trace := range traces {
		if trace.Role == role {
			return trace
		}
	}

	// create a new trace for the role
	trace := &validatorDutyTrace{
		ValidatorDutyTrace: model.ValidatorDutyTrace{
			Slot: phase0.Slot(slot),
			Role: role,
		},
	}
	mp.Value()[vPubKey] = append(traces, trace)
	return trace
}

func toBNRole(r spectypes.RunnerRole) (bnRole spectypes.BeaconRole) {
	switch r {
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

	return
}

func (n *InMemTracer) getOrCreateCommitteeTrace(slot uint64, committeeID spectypes.CommitteeID) *committeeDutyTrace {
	mp, _ := n.committeeTraces.GetOrSet(slot, make(map[spectypes.CommitteeID]*committeeDutyTrace))

	n.committeeTracesMu.Lock()
	defer n.committeeTracesMu.Unlock()

	trace, found := mp.Value()[committeeID]
	if !found {
		trace = &committeeDutyTrace{
			CommitteeDutyTrace: model.CommitteeDutyTrace{
				CommitteeID: committeeID,
				Slot:        phase0.Slot(slot),
			},
		}
		mp.Value()[committeeID] = trace
	}

	return trace
}

func (n *InMemTracer) decodeJustificationWithPrepares(justifications [][]byte) []*model.QBFTTrace {
	var traces = make([]*model.QBFTTrace, 0, len(justifications))
	for _, rcj := range justifications {
		var signedMsg = new(spectypes.SignedSSVMessage)
		err := signedMsg.Decode(rcj)
		if err != nil {
			n.logger.Error("decode round change justification", zap.Error(err))
			continue
		}

		var qbftMsg = new(specqbft.Message)
		err = qbftMsg.Decode(signedMsg.SSVMessage.GetData())
		if err != nil {
			n.logger.Error("decode signed message data", zap.Error(err))
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
			n.logger.Error("decode round change justification", zap.Error(err))
			continue
		}

		var qbftMsg = new(specqbft.Message)
		err = qbftMsg.Decode(signedMsg.SSVMessage.GetData())
		if err != nil {
			n.logger.Error("decode round change justification", zap.Error(err))
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
		m.ReceivedTime = time.Now()

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
		m.ReceivedTime = time.Now()

		round.Commits = append(round.Commits, m)

	case specqbft.RoundChangeMsgType:
		now := time.Now()
		roundChangeTrace := n.createRoundChangeTrace(msg, signedMsg, now)

		round.RoundChanges = append(round.RoundChanges, roundChangeTrace)
	}

	return nil
}

func (n *InMemTracer) processPartialSigValidator(msg *spectypes.PartialSignatureMessages, ssvMsg *queue.SSVMessage, pubkey spectypes.ValidatorPK) {
	runnerRole := ssvMsg.MsgID.GetRoleType()
	role := toBNRole(runnerRole)
	slot := uint64(msg.Slot)
	trace := n.getOrCreateValidatorTrace(slot, role, pubkey)
	trace.Lock()
	defer trace.Unlock()

	if trace.Validator == 0 {
		trace.Validator = msg.Messages[0].ValidatorIndex
	}

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

func (n *InMemTracer) processPartialSigCommittee(msg *spectypes.PartialSignatureMessages, committeeID spectypes.CommitteeID) {
	slot := uint64(msg.Slot)

	trace := n.getOrCreateCommitteeTrace(slot, committeeID)
	trace.Lock()
	defer trace.Unlock()

	{ // TODO(moshe) confirm is needed
		cmt, err := n.validators.Committee(trace.CommitteeID)
		_ = err
		if cmt != nil && len(cmt.Operators) > 0 {
			trace.OperatorIDs = cmt.Operators
		}
	}

	// store the link between validator index and committee id
	n.saveValidatorToCommitteeLink(phase0.Slot(slot), msg, committeeID)

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

func (n *InMemTracer) saveValidatorToCommitteeLink(slot phase0.Slot, msg *spectypes.PartialSignatureMessages, committeeID spectypes.CommitteeID) {
	n.validatorCommitteeMappingMu.Lock()
	defer n.validatorCommitteeMappingMu.Unlock()

	cacheItem, _ := n.validatorCommitteeMapping.GetOrSet(slot, make(map[phase0.ValidatorIndex]spectypes.CommitteeID))
	for _, msg := range msg.Messages {
		cacheItem.Value()[msg.ValidatorIndex] = committeeID
		s, f := n.validators.ValidatorByIndex(msg.ValidatorIndex)
		if !f {
			continue
		}
		n.logger.Info("store link for", fields.Slot(slot), zap.Uint64("index", uint64(msg.ValidatorIndex)), fields.CommitteeID(committeeID), fields.Validator(s.ValidatorPubKey[:]))
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
// 		logger.Debug("get sync committee domain", zap.Error(err))
// 		continue
// 	}
// 	// Eth root
// 	blockRoot := spectypes.SSZBytes(beaconVote.BlockRoot[:])
// 	root, err := spectypes.ComputeETHSigningRoot(blockRoot, domain)
// 	if err != nil {
// 		n.logger.Debug("compute sync committee root", zap.Error(err))
// 		return
// 	}

// 	root, err := spectypes.ComputeETHSigningRoot(spectypes.SSZBytes(object[:]), phase0.Domain(spectypes.DomainSyncCommittee))
// 	if err != nil {
// 		return fmt.Errorf("compute eth signing root for domain: %w", err)
// 	}

// 	trace.syncCommitteeRoot = root
// 	return nil
// }

func (n *InMemTracer) populateProposer(round *model.RoundTrace, committeeID spectypes.CommitteeID, subMsg *specqbft.Message) {
	committee, found := n.validators.Committee(committeeID)
	if !found {
		// TODO(me): uncomment
		// n.logger.Error("could not find committee by id", fields.CommitteeID(committeeID))
		return
	}

	operatorIDs := committee.Operators

	if len(operatorIDs) > 0 {
		mockState := n.toMockState(subMsg, operatorIDs)
		round.Proposer = specqbft.RoundRobinProposer(mockState, subMsg.Round)
	}
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

func (n *InMemTracer) Trace(msg *queue.SSVMessage) {
	switch msg.MsgType {
	case spectypes.SSVConsensusMsgType:
		if subMsg, ok := msg.Body.(*specqbft.Message); ok {
			slot := uint64(subMsg.Height)
			msgID := spectypes.MessageID(subMsg.Identifier[:]) // validator + pubkey + role + network
			executorID := msgID.GetDutyExecutorID()            // validator pubkey or committee id

			switch role := msgID.GetRoleType(); role {
			case spectypes.RoleCommittee:
				var committeeID spectypes.CommitteeID
				copy(committeeID[:], executorID[16:])

				trace := n.getOrCreateCommitteeTrace(slot, committeeID) // committe id

				trace.Lock()
				defer trace.Unlock()

				// first step fill in sync committee roots
				// to be later read in 'processPartialSigCommittee'
				// err := n.fillInSyncCommitteeRoots(trace, msg.Slot, msg.SignedSSVMessage.FullData)
				// if err != nil {
				// 	n.logger.Error("fill sync committee roots", zap.Error(err))
				// }

				round := getOrCreateRound(&trace.ConsensusTrace, uint64(subMsg.Round))

				// TODO(moshe) populate proposer or not?
				n.populateProposer(round, committeeID, subMsg)

				decided := n.processConsensus(subMsg, msg.SignedSSVMessage, round)
				if decided != nil {
					n.logger.Info("committee decideds", fields.Slot(phase0.Slot(subMsg.Height)), fields.CommitteeID(committeeID))
					trace.Decideds = append(trace.Decideds, decided)
				}
			default:
				var validatorPK spectypes.ValidatorPK
				copy(validatorPK[:], executorID)

				bnRole := toBNRole(role)

				trace := n.getOrCreateValidatorTrace(slot, bnRole, validatorPK)

				if msg.MsgType == spectypes.SSVConsensusMsgType {
					var qbftMsg specqbft.Message
					err := qbftMsg.Decode(msg.Data)
					if err != nil {
						n.logger.Error("decode validator consensus data", zap.Error(err))
						return
					}

					if qbftMsg.MsgType == specqbft.ProposalMsgType {
						stop := func() bool {
							trace.Lock()
							defer trace.Unlock()

							var data = new(spectypes.ValidatorConsensusData)
							err := data.Decode(msg.SignedSSVMessage.FullData)
							if err != nil {
								n.logger.Error("decode validator proposal data", zap.Error(err))
								return true
							}

							if trace.Validator == 0 {
								trace.Validator = data.Duty.ValidatorIndex
							}

							return false
						}()

						if stop {
							return
						}
					}
				}

				trace.Lock()
				defer trace.Unlock()

				round := getOrCreateRound(&trace.ConsensusTrace, uint64(subMsg.Round))

				// TODO(moshe) populate proposer or not?
				// n.populateProposer(round, validatorPK, subMsg)

				decided := n.processConsensus(subMsg, msg.SignedSSVMessage, round)
				if decided != nil {
					n.logger.Info("validator decideds", fields.Slot(phase0.Slot(subMsg.Height)), fields.Validator(validatorPK[:]))
					trace.Decideds = append(trace.Decideds, decided)
				}
			}
		}
	case spectypes.SSVPartialSignatureMsgType:
		pSigMessages := new(spectypes.PartialSignatureMessages)
		err := pSigMessages.Decode(msg.SignedSSVMessage.SSVMessage.GetData())
		if err != nil {
			n.logger.Error("decode partial signature messages", zap.Error(err))
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
				// TODO(me): uncomment
				// n.logger.Error("get share by index", zap.Uint64("validator", uint64(msg.ValidatorIndex)))
				continue
			}

			var sharePubkey spectypes.ShareValidatorPK
			for _, cmt := range share.Committee {
				if cmt.Signer == msg.Signer {
					sharePubkey = cmt.SharePubKey
					break
				}
			}

			err = types.VerifyReconstructedSignature(sig, sharePubkey, msg.SigningRoot)
			if err != nil {
				n.logger.Error("bls verification failed", zap.Error(err))
				return
			}
		}

		executorID := msg.MsgID.GetDutyExecutorID()

		if msg.MsgID.GetRoleType() == spectypes.RoleCommittee {
			var committeeID spectypes.CommitteeID
			copy(committeeID[:], executorID[16:])

			n.processPartialSigCommittee(pSigMessages, committeeID)

			return
		}

		var validatorPK spectypes.ValidatorPK
		copy(validatorPK[:], executorID)

		n.processPartialSigValidator(pSigMessages, msg, validatorPK)
	}
}

type validatorDutyTrace struct {
	sync.Mutex
	model.ValidatorDutyTrace
}

type committeeDutyTrace struct {
	sync.Mutex
	syncCommitteeRoot phase0.Root
	model.CommitteeDutyTrace
}

// must be called under parent trace lock
func getOrCreateRound(trace *model.ConsensusTrace, rnd uint64) *model.RoundTrace {
	var count = len(trace.Rounds)
	for rnd > uint64(count) { //nolint:gosec
		trace.Rounds = append(trace.Rounds, &model.RoundTrace{})
		count = len(trace.Rounds)
	}

	return trace.Rounds[rnd-1]
}

func (n *InMemTracer) getValidatorDuty(bnRole spectypes.BeaconRole, slot phase0.Slot, pubkey spectypes.ValidatorPK) (*model.ValidatorDutyTrace, error) {
	m := n.validatorTraces.Get(uint64(slot))
	if m == nil {
		return n.getValidatorDutyFromDisk(bnRole, slot, pubkey)
	}

	n.validatorTracesMu.Lock()
	defer n.validatorTracesMu.Unlock()

	traces, ok := m.Value()[pubkey]
	if !ok {
		return nil, errors.New("validator not found")
	}

	// find the trace for the role
	for _, trace := range traces {
		if trace.Role == bnRole {
			trace.Lock()
			defer trace.Unlock()

			// TODO(me): deep copy
			return &trace.ValidatorDutyTrace, nil
		}
	}

	return nil, errors.New("validator duty not found")
}

func (n *InMemTracer) getValidatorDutyFromDisk(bnRole spectypes.BeaconRole, slot phase0.Slot, pubkey spectypes.ValidatorPK) (*model.ValidatorDutyTrace, error) {
	vIndex, found := n.validators.ValidatorIndex(pubkey)
	if !found {
		return nil, fmt.Errorf("validator not found by pubkey: %x", pubkey)
	}

	trace, err := n.store.GetValidatorDuty(slot, bnRole, vIndex)
	if err != nil {
		return nil, fmt.Errorf("get validator duty from disk: %w", err)
	}

	return trace, nil
}

func (n *InMemTracer) getCommitteeDuty(slot phase0.Slot, committeeID spectypes.CommitteeID) (*model.CommitteeDutyTrace, error) {
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

	n.committeeTracesMu.Lock()
	defer n.committeeTracesMu.Unlock()

	trace, ok := m.Value()[committeeID]
	if !ok {
		return nil, errors.New("committe ID not found: " + hex.EncodeToString(committeeID[:]))
	}

	// TODO(me): create deep copies
	trace.Lock()
	defer trace.Unlock()

	return &trace.CommitteeDutyTrace, nil
}

func decodeSig(in []byte) (*bls.Sign, error) {
	sign := new(bls.Sign)
	err := sign.Deserialize(in)
	return sign, err
}
