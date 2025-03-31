package validator

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	model "github.com/ssvlabs/ssv/exporter/v2"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/operator/slotticker"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

type Collector struct {
	logger *zap.Logger

	// committeeID:slot:committeeDutyTrace
	committeeTraces *TypedSyncMap[spectypes.CommitteeID, *TypedSyncMap[phase0.Slot, *committeeDutyTrace]]

	// validatorPubKey:slot:validatorDutyTrace
	validatorTraces *TypedSyncMap[spectypes.ValidatorPK, *TypedSyncMap[phase0.Slot, *validatorDutyTrace]]

	// validatorIndex:slot:committeeID
	validatorIndexToCommitteeLinks *TypedSyncMap[phase0.ValidatorIndex, *TypedSyncMap[phase0.Slot, spectypes.CommitteeID]]

	syncCommitteeRootsCache *ttlcache.Cache[scRootKey, phase0.Root]
	syncCommitteeRootsSf    singleflight.Group

	beacon spectypes.BeaconNetwork

	store      DutyTraceStore
	client     DomainDataProvider
	validators registrystorage.ValidatorStore

	currentSlot     atomic.Uint64
	lastEvictedSlot atomic.Uint64
}

type DomainDataProvider interface {
	DomainData(epoch phase0.Epoch, domain phase0.DomainType) (phase0.Domain, error)
}

func New(ctx context.Context,
	logger *zap.Logger,
	validators registrystorage.ValidatorStore,
	client DomainDataProvider,
	store DutyTraceStore,
	beaconNetwork spectypes.BeaconNetwork,
) *Collector {

	ttl := time.Duration(ttlCommitteeRoot) * beaconNetwork.SlotDurationSec()

	tracer := &Collector{
		logger:                         logger,
		store:                          store,
		client:                         client,
		beacon:                         beaconNetwork,
		validators:                     validators,
		committeeTraces:                NewTypedSyncMap[spectypes.CommitteeID, *TypedSyncMap[phase0.Slot, *committeeDutyTrace]](),
		validatorTraces:                NewTypedSyncMap[spectypes.ValidatorPK, *TypedSyncMap[phase0.Slot, *validatorDutyTrace]](),
		validatorIndexToCommitteeLinks: NewTypedSyncMap[phase0.ValidatorIndex, *TypedSyncMap[phase0.Slot, spectypes.CommitteeID]](),
		syncCommitteeRootsCache:        ttlcache.New(ttlcache.WithTTL[scRootKey, phase0.Root](ttl)),
	}

	return tracer
}

// scRootKey is a key for the sync committee root cache
type scRootKey struct {
	slot phase0.Slot
	root phase0.Root // block root
}

func (c *Collector) StartEvictionJob(ctx context.Context, tickerProvider slotticker.Provider, offset phase0.Slot) {
	c.logger.Info("start duty tracer cache to disk evictor")
	ticker := tickerProvider()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.Next():
			currentSlot := ticker.Slot() + offset // optional offset

			c.currentSlot.Store(uint64(currentSlot))
			// evict committee traces
			committeThreshold := currentSlot - ttlCommittee
			evicted := c.evictCommitteeTraces(committeThreshold)
			c.logger.Info("evicted committee duty traces to disk", fields.Slot(committeThreshold), zap.Int("count", evicted))

			// evict validator traces
			validatorThreshold := currentSlot - ttlValidator
			evicted = c.evictValidatorTraces(validatorThreshold)
			c.logger.Info("evicted validator duty traces to disk", fields.Slot(validatorThreshold), zap.Int("count", evicted))

			// evict validator committee links
			mappingThreshold := currentSlot - ttlMapping
			evicted = c.evictValidatorCommitteeLinks(mappingThreshold)
			c.logger.Info("evicted validator mappings to disk", fields.Slot(mappingThreshold), zap.Int("count", evicted))

			// remove old SC roots
			c.syncCommitteeRootsCache.DeleteExpired()

			c.lastEvictedSlot.Store(uint64(validatorThreshold))
		}
	}
}

/*
all validatorDutyTrace objects contain a collection of model.ValidatorDutyTrace(s) - per each role.
so when we request a certain trace we return both of them:
- validatorDutyTrace object contains the lock
- the model.* ValidatorDutyTrace that has the data that we enrich subsequently
.LoadOrStore is being used to take care of the case when two goroutines try to create the same trace at the same time
*/
func (c *Collector) getOrCreateValidatorTrace(slot phase0.Slot, role spectypes.BeaconRole, vPubKey spectypes.ValidatorPK) (*validatorDutyTrace, *model.ValidatorDutyTrace, error) {
	if uint64(slot) <= c.lastEvictedSlot.Load() {
		return nil, nil, fmt.Errorf("validator(%s) late arrival, trace slot(%d), last evicted(%d)", hex.EncodeToString(vPubKey[:]), slot, c.lastEvictedSlot.Load())
	}

	validatorSlots, found := c.validatorTraces.Load(vPubKey)
	if !found {
		validatorSlots, _ = c.validatorTraces.LoadOrStore(vPubKey, NewTypedSyncMap[phase0.Slot, *validatorDutyTrace]())
	}

	traces, found := validatorSlots.Load(slot)

	if !found {
		roleDutyTrace := &model.ValidatorDutyTrace{
			Slot: slot,
			Role: role,
		}
		newTrace := &validatorDutyTrace{
			Roles: []*model.ValidatorDutyTrace{roleDutyTrace},
		}
		traces, _ = validatorSlots.LoadOrStore(slot, newTrace)
		return traces, roleDutyTrace, nil
	}

	traces.Lock()
	defer traces.Unlock()

	// find the trace for the role
	for _, t := range traces.Roles {
		if t.Role == role {
			return traces, t, nil
		}
	}

	// or create a new one
	roleDutyTrace := &model.ValidatorDutyTrace{
		Slot: slot,
		Role: role,
	}
	traces.Roles = append(traces.Roles, roleDutyTrace)

	return traces, roleDutyTrace, nil
}

func (c *Collector) getOrCreateCommitteeTrace(slot phase0.Slot, committeeID spectypes.CommitteeID) (*committeeDutyTrace, error) {
	if uint64(slot) <= c.lastEvictedSlot.Load() {
		return nil, fmt.Errorf("committee(%s) late arrival, trace slot(%d), last evicted(%d)", hex.EncodeToString(committeeID[:]), slot, c.lastEvictedSlot.Load())
	}

	committeeSlots, found := c.committeeTraces.Load(committeeID)
	if !found {
		committeeSlots, _ = c.committeeTraces.LoadOrStore(committeeID, NewTypedSyncMap[phase0.Slot, *committeeDutyTrace]())
	}

	committeeTrace, found := committeeSlots.Load(slot)

	if !found {
		trace := &committeeDutyTrace{
			CommitteeDutyTrace: model.CommitteeDutyTrace{
				CommitteeID: committeeID,
				Slot:        slot,
			},
		}

		committeeTrace, _ = committeeSlots.LoadOrStore(slot, trace)
	}

	return committeeTrace, nil
}

func (c *Collector) decodeJustificationWithPrepares(justifications [][]byte) []*model.QBFTTrace {
	var traces = make([]*model.QBFTTrace, 0, len(justifications))
	for _, rcj := range justifications {
		var signedMsg = new(spectypes.SignedSSVMessage)
		err := signedMsg.Decode(rcj)
		if err != nil {
			c.logger.Error("decode round change justification", zap.Error(err))
			continue
		}

		var qbftMsg = new(specqbft.Message)
		err = qbftMsg.Decode(signedMsg.SSVMessage.GetData())
		if err != nil {
			c.logger.Error("decode signed message data", zap.Error(err))
			continue
		}

		justificationTrace := model.QBFTTrace{
			Round:      uint64(qbftMsg.Round),
			BeaconRoot: qbftMsg.Root,
			Signer:     signedMsg.OperatorIDs[0],
		}

		traces = append(traces, &justificationTrace)
	}

	return traces
}

func (c *Collector) decodeJustificationWithRoundChanges(justifications [][]byte) []*model.RoundChangeTrace {
	var traces = make([]*model.RoundChangeTrace, 0, len(justifications))
	for _, rcj := range justifications {
		var signedMsg = new(spectypes.SignedSSVMessage)
		err := signedMsg.Decode(rcj)
		if err != nil {
			c.logger.Error("decode round change justification", zap.Error(err))
			continue
		}

		var qbftMsg = new(specqbft.Message)
		err = qbftMsg.Decode(signedMsg.SSVMessage.GetData())
		if err != nil {
			c.logger.Error("decode round change justification", zap.Error(err))
			continue
		}

		var roundChangeTrace = c.createRoundChangeTrace(0, qbftMsg, signedMsg) // zero time
		traces = append(traces, roundChangeTrace)
	}

	return traces
}

func (c *Collector) createRoundChangeTrace(receivedAt uint64, msg *specqbft.Message, signedMsg *spectypes.SignedSSVMessage) *model.RoundChangeTrace {
	return &model.RoundChangeTrace{
		QBFTTrace: model.QBFTTrace{
			Round:        uint64(msg.Round),
			BeaconRoot:   msg.Root,
			Signer:       signedMsg.OperatorIDs[0],
			ReceivedTime: receivedAt,
		},
		PreparedRound:   uint64(msg.DataRound),
		PrepareMessages: c.decodeJustificationWithPrepares(msg.RoundChangeJustification),
	}
}

func (c *Collector) createProposalTrace(receivedAt uint64, msg *specqbft.Message, signedMsg *spectypes.SignedSSVMessage) *model.ProposalTrace {
	return &model.ProposalTrace{
		QBFTTrace: model.QBFTTrace{
			Round:        uint64(msg.Round),
			BeaconRoot:   msg.Root,
			Signer:       signedMsg.OperatorIDs[0],
			ReceivedTime: receivedAt,
		},
		RoundChanges:    c.decodeJustificationWithRoundChanges(msg.RoundChangeJustification),
		PrepareMessages: c.decodeJustificationWithPrepares(msg.PrepareJustification),
	}
}

func (c *Collector) processConsensus(receivedAt uint64, msg *specqbft.Message, signedMsg *spectypes.SignedSSVMessage, round *model.RoundTrace) (*model.DecidedTrace, error) {
	switch msg.MsgType {
	case specqbft.ProposalMsgType:
		round.ProposalTrace = c.createProposalTrace(receivedAt, msg, signedMsg)

	case specqbft.PrepareMsgType:
		prepare := &model.QBFTTrace{
			Round:        uint64(msg.Round),
			BeaconRoot:   msg.Root,
			Signer:       signedMsg.OperatorIDs[0],
			ReceivedTime: receivedAt,
		}

		round.Prepares = append(round.Prepares, prepare)

	case specqbft.CommitMsgType:
		if len(signedMsg.OperatorIDs) == 0 {
			return nil, errors.New("no operator IDs")
		}

		if len(signedMsg.OperatorIDs) > 1 {
			return &model.DecidedTrace{
				Round:        uint64(msg.Round),
				BeaconRoot:   msg.Root,
				Signers:      signedMsg.OperatorIDs,
				ReceivedTime: receivedAt,
			}, nil
		}

		commit := &model.QBFTTrace{
			Round:        uint64(msg.Round),
			BeaconRoot:   msg.Root,
			Signer:       signedMsg.OperatorIDs[0],
			ReceivedTime: receivedAt,
		}

		round.Commits = append(round.Commits, commit)

	case specqbft.RoundChangeMsgType:
		roundChangeTrace := c.createRoundChangeTrace(receivedAt, msg, signedMsg)

		round.RoundChanges = append(round.RoundChanges, roundChangeTrace)
	}

	return nil, nil // we're exhausting all cases in the switch
}

func (c *Collector) processPartialSigValidator(receivedAt uint64, msg *spectypes.PartialSignatureMessages, ssvMsg *queue.SSVMessage, pubkey spectypes.ValidatorPK) error {
	role, err := toBNRole(ssvMsg.MsgID.GetRoleType())
	if err != nil {
		return err
	}

	trace, roleDutyTrace, err := c.getOrCreateValidatorTrace(msg.Slot, role, pubkey)
	if err != nil {
		return err
	}

	trace.Lock()
	defer trace.Unlock()

	if roleDutyTrace.Validator == 0 {
		roleDutyTrace.Validator = msg.Messages[0].ValidatorIndex
	}

	tr := &model.PartialSigTrace{
		Type:         msg.Type,
		BeaconRoot:   msg.Messages[0].SigningRoot,
		Signer:       msg.Messages[0].Signer,
		ReceivedTime: receivedAt,
	}

	if msg.Type == spectypes.PostConsensusPartialSig {
		roleDutyTrace.Post = append(roleDutyTrace.Post, tr)
		return nil
	}

	roleDutyTrace.Pre = append(roleDutyTrace.Pre, tr)

	return nil
}

func (c *Collector) processPartialSigCommittee(receivedAt uint64, msg *spectypes.PartialSignatureMessages, committeeID spectypes.CommitteeID) error {
	slot := msg.Slot

	trace, err := c.getOrCreateCommitteeTrace(slot, committeeID)
	if err != nil {
		return err
	}
	trace.Lock()
	defer trace.Unlock()

	// add operator ids to the trace
	cmt, found := c.validators.Committee(committeeID)
	if found && len(cmt.Operators) > 0 {
		trace.OperatorIDs = cmt.Operators
	}

	if len(msg.Messages) == 0 {
		return fmt.Errorf("no partial sig messages")
	}

	// store the link between validator index and committee id
	c.saveValidatorToCommitteeLink(slot, msg, committeeID)

	var isSyncCommittee bool

	if bytes.Equal(trace.syncCommitteeRoot[:], msg.Messages[0].SigningRoot[:]) {
		isSyncCommittee = true
	}

	signer := msg.Messages[0].Signer

	indices := make([]phase0.ValidatorIndex, 0, len(msg.Messages))
	for _, partialSigMsg := range msg.Messages {
		indices = append(indices, partialSigMsg.ValidatorIndex)
	}

	slices.Sort(indices)
	indices = slices.Compact(indices)

	if isSyncCommittee {
		trace.SyncCommittee = append(trace.SyncCommittee, &model.SignerData{
			Signer:       signer,
			ValidatorIdx: indices,
			ReceivedTime: receivedAt,
		})

		return nil
	}

	trace.Attester = append(trace.Attester, &model.SignerData{
		Signer:       signer,
		ValidatorIdx: indices,
		ReceivedTime: receivedAt,
	})

	return nil
}

func (c *Collector) saveValidatorToCommitteeLink(slot phase0.Slot, msg *spectypes.PartialSignatureMessages, committeeID spectypes.CommitteeID) {
	for _, msg := range msg.Messages {
		slotToCommittee, found := c.validatorIndexToCommitteeLinks.Load(msg.ValidatorIndex)
		if !found {
			slotToCommittee, _ = c.validatorIndexToCommitteeLinks.LoadOrStore(msg.ValidatorIndex, NewTypedSyncMap[phase0.Slot, spectypes.CommitteeID]())
		}

		slotToCommittee.Store(slot, committeeID)
	}
}
func (c *Collector) getSyncCommitteeRoot(slot phase0.Slot, in []byte) (phase0.Root, error) {
	var beaconVote = new(spectypes.BeaconVote)
	if err := beaconVote.Decode(in); err != nil {
		return phase0.Root{}, fmt.Errorf("decode beacon vote: %w", err)
	}

	key := scRootKey{slot: slot, root: beaconVote.BlockRoot}

	// lookup in cache first
	cacheItem := c.syncCommitteeRootsCache.Get(key)
	if cacheItem != nil {
		return cacheItem.Value(), nil
	}

	// Use singleflight to ensure only one goroutine computes the root for a given key
	sfKey := fmt.Sprintf("%d-%s", slot, beaconVote.BlockRoot.String())
	val, err, _ := c.syncCommitteeRootsSf.Do(sfKey, func() (any, error) {
		// Check cache again in case another goroutine has populated it while we were waiting
		if cacheItem := c.syncCommitteeRootsCache.Get(key); cacheItem != nil {
			return cacheItem.Value(), nil
		}

		c.logger.Info("fetching sync committee root", fields.Slot(slot), fields.Root(beaconVote.BlockRoot))

		epoch := c.beacon.EstimatedEpochAtSlot(slot)

		domain, err := c.client.DomainData(epoch, spectypes.DomainSyncCommittee)
		if err != nil {
			return phase0.Root{}, fmt.Errorf("get sync committee domain data: %w", err)
		}

		// Beacon root
		blockRoot := spectypes.SSZBytes(beaconVote.BlockRoot[:])
		signingRoot, err := spectypes.ComputeETHSigningRoot(blockRoot, domain)
		if err != nil {
			return phase0.Root{}, fmt.Errorf("compute sync committee root: %w", err)
		}

		ttl := time.Duration(ttlCommitteeRoot) * c.beacon.SlotDurationSec()
		_ = c.syncCommitteeRootsCache.Set(key, signingRoot, ttl)

		return signingRoot, nil
	})

	if err != nil {
		return phase0.Root{}, err
	}

	return val.(phase0.Root), nil
}

func (c *Collector) Collect(ctx context.Context, msg *queue.SSVMessage, verifySig func(*spectypes.PartialSignatureMessages) error) error {
	start := time.Now()

	tracerInFlightMessageCounter.Add(ctx, 1)
	defer func() {
		tracerInFlightMessageHist.Record(ctx, time.Since(start).Seconds())
	}()

	//nolint:gosec
	startTime := uint64(start.UnixNano() / int64(time.Millisecond))

	switch msg.MsgType {
	case spectypes.SSVConsensusMsgType:
		subMsg, ok := msg.Body.(*specqbft.Message)
		if !ok {
			return nil
		}

		slot := phase0.Slot(subMsg.Height)
		msgID := spectypes.MessageID(subMsg.Identifier[:])
		executorID := msgID.GetDutyExecutorID()

		switch role := msgID.GetRoleType(); role {
		case spectypes.RoleCommittee:
			var committeeID spectypes.CommitteeID
			copy(committeeID[:], executorID[16:])

			trace, err := c.getOrCreateCommitteeTrace(slot, committeeID)
			if err != nil {
				return err
			}

			trace.Lock()
			defer trace.Unlock()

			if len(msg.SignedSSVMessage.FullData) > 0 {
				// save proposal data
				if subMsg.MsgType == specqbft.ProposalMsgType {
					// c.logger.Info("proposal data", fields.Slot(slot), fields.CommitteeID(committeeID), zap.Int("size", len(msg.SignedSSVMessage.FullData))) // TODO(me): remove this
					// committee duty will contain the BeaconVote data
					trace.ProposalData = msg.SignedSSVMessage.FullData
				}
				// for future: check if it's a proposal message
				// if not, skip this step
				root, err := c.getSyncCommitteeRoot(slot, msg.SignedSSVMessage.FullData)
				if err != nil {
					return fmt.Errorf("get sync committee root: %w", err)
				}
				// save sync committee root
				trace.syncCommitteeRoot = root
			}

			round := getOrCreateRound(&trace.ConsensusTrace, uint64(subMsg.Round))

			decided, err := c.processConsensus(startTime, subMsg, msg.SignedSSVMessage, round)
			if err != nil {
				return err
			}
			if decided != nil {
				trace.Decideds = append(trace.Decideds, decided)
			}
		default:
			var validatorPK spectypes.ValidatorPK
			copy(validatorPK[:], executorID)

			bnRole, err := toBNRole(role)
			if err != nil {
				return err
			}

			trace, roleDutyTrace, err := c.getOrCreateValidatorTrace(slot, bnRole, validatorPK)
			if err != nil {
				return err
			}

			func() {
				var qbftMsg = new(specqbft.Message)
				err := qbftMsg.Decode(msg.Data)
				if err != nil {
					c.logger.Error("decode validator consensus data", zap.Error(err), fields.Slot(slot), fields.Validator(validatorPK[:]))
					return
				}

				if qbftMsg.MsgType == specqbft.ProposalMsgType {
					var data = new(spectypes.ValidatorConsensusData)
					err := data.Decode(msg.SignedSSVMessage.FullData)
					if err != nil {
						// c.logger.Error("decode validator proposal data", zap.Error(err), fields.Slot(slot), fields.Validator(validatorPK[:]))
						return
					}

					trace.Lock()
					defer trace.Unlock()

					if roleDutyTrace.Validator == 0 {
						roleDutyTrace.Validator = data.Duty.ValidatorIndex
					}

					c.logger.Info("proposal data", fields.Slot(slot), fields.Validator(validatorPK[:]), fields.ValidatorIndex(data.Duty.ValidatorIndex), zap.Int("size", len(data.DataSSZ))) // TODO(me): remove this

					// non-committee duty will contain the proposal data
					roleDutyTrace.ProposalData = data.DataSSZ
				}
			}()

			trace.Lock()
			defer trace.Unlock()

			round := getOrCreateRound(&roleDutyTrace.ConsensusTrace, uint64(subMsg.Round))

			decided, err := c.processConsensus(startTime, subMsg, msg.SignedSSVMessage, round)
			if err != nil {
				return err
			}
			if decided != nil {
				roleDutyTrace.Decideds = append(roleDutyTrace.Decideds, decided)
			}
		}

	case spectypes.SSVPartialSignatureMsgType:
		pSigMessages := new(spectypes.PartialSignatureMessages)
		err := pSigMessages.Decode(msg.SignedSSVMessage.SSVMessage.GetData())
		if err != nil {
			return fmt.Errorf("decode partial signature messages: %w", err)
		}

		if pSigMessages.Type == spectypes.PostConsensusPartialSig {
			if err := pSigMessages.Validate(); err != nil {
				return fmt.Errorf("validate partial sig: %w", err)
			}

			if err := verifySig(pSigMessages); err != nil {
				return fmt.Errorf("verify partial sig: %w", err)
			}
		}

		executorID := msg.MsgID.GetDutyExecutorID()

		if msg.MsgID.GetRoleType() == spectypes.RoleCommittee {
			var committeeID spectypes.CommitteeID
			copy(committeeID[:], executorID[16:])

			return c.processPartialSigCommittee(startTime, pSigMessages, committeeID)
		}

		var validatorPK spectypes.ValidatorPK
		copy(validatorPK[:], executorID)

		return c.processPartialSigValidator(startTime, pSigMessages, msg, validatorPK)
	}

	return nil
}

func toBNRole(r spectypes.RunnerRole) (bnRole spectypes.BeaconRole, err error) {
	switch r {
	case spectypes.RoleCommittee:
		return spectypes.BNRoleUnknown, errors.New("unexpected committee role")
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

type validatorDutyTrace struct {
	sync.Mutex
	Roles []*model.ValidatorDutyTrace
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
