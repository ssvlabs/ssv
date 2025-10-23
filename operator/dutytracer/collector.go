package validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/exporter/rolemask"
	"github.com/ssvlabs/ssv/exporter/store"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/operator/slotticker"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

type DecidedInfo struct {
	Index   phase0.ValidatorIndex
	Slot    phase0.Slot
	Role    spectypes.BeaconRole
	Signers []spectypes.OperatorID
}

type Collector struct {
	logger *zap.Logger

	// committeeID:slot:committeeDutyTrace
	committeeTraces *hashmap.Map[spectypes.CommitteeID, *hashmap.Map[phase0.Slot, *committeeDutyTrace]]

	// validatorIndex:slot:validatorDutyTrace
	validatorTraces *hashmap.Map[phase0.ValidatorIndex, *hashmap.Map[phase0.Slot, *validatorDutyTrace]]

	// validatorIndex:slot:committeeID
	validatorIndexToCommitteeLinks *hashmap.Map[phase0.ValidatorIndex, *hashmap.Map[phase0.Slot, spectypes.CommitteeID]]

	syncCommitteeRootsCache *ttlcache.Cache[scRootKey, phase0.Root]
	syncCommitteeRootsSf    singleflight.Group

	beacon *networkconfig.Beacon

	store      DutyTraceStore
	client     DomainDataProvider
	validators registrystorage.ValidatorStore

	lastEvictedSlot atomic.Uint64

	inFlightCommittee hashmap.Map[spectypes.CommitteeID, struct{}]
	inFlightValidator hashmap.Map[phase0.ValidatorIndex, struct{}]

	decidedListenerFunc func(msg DecidedInfo)

	// duties is the runtime duty store used to derive per-slot scheduled roles.
	duties *dutystore.Store

	// scheduleJobs is a bounded queue for async schedule computation to avoid
	// blocking the hot path and DB with synchronous writes.
	scheduleJobs chan phase0.Slot
}

type DomainDataProvider interface {
	DomainData(context.Context, phase0.Epoch, phase0.DomainType) (phase0.Domain, error)
}

func New(
	logger *zap.Logger,
	validators registrystorage.ValidatorStore,
	client DomainDataProvider,
	store DutyTraceStore,
	beaconNetwork *networkconfig.Beacon,
	decidedListenerFunc func(msg DecidedInfo),
	duties *dutystore.Store,
) *Collector {
	ttl := time.Duration(slotTTL) * beaconNetwork.SlotDuration

	collector := &Collector{
		logger:                         logger.Named("dutytracer"),
		store:                          store,
		client:                         client,
		beacon:                         beaconNetwork,
		validators:                     validators,
		committeeTraces:                hashmap.New[spectypes.CommitteeID, *hashmap.Map[phase0.Slot, *committeeDutyTrace]](),
		validatorTraces:                hashmap.New[phase0.ValidatorIndex, *hashmap.Map[phase0.Slot, *validatorDutyTrace]](),
		validatorIndexToCommitteeLinks: hashmap.New[phase0.ValidatorIndex, *hashmap.Map[phase0.Slot, spectypes.CommitteeID]](),
		syncCommitteeRootsCache:        ttlcache.New(ttlcache.WithTTL[scRootKey, phase0.Root](ttl)),
		inFlightCommittee:              hashmap.Map[spectypes.CommitteeID, struct{}]{},
		inFlightValidator:              hashmap.Map[phase0.ValidatorIndex, struct{}]{},
		decidedListenerFunc:            decidedListenerFunc,
		duties:                         duties,
		scheduleJobs:                   make(chan phase0.Slot, 32),
	}

	return collector
}

// scRootKey is a key for the sync committee root cache
type scRootKey struct {
	slot      phase0.Slot
	blockRoot phase0.Root
}

func (c *Collector) Start(ctx context.Context, tickerProvider slotticker.Provider) {
	c.logger.Info("start duty tracer cache to disk evictor")
	ticker := tickerProvider()
	// Start schedule filler in a separate goroutine to avoid blocking eviction.
	go c.startScheduleFiller(ctx, tickerProvider)
	// Start a single worker to process schedule writes asynchronously.
	go c.runScheduleWorker(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.Next():
			currentSlot := ticker.Slot()
			c.evict(currentSlot)
		}
	}
}

const slotTTL = 4

func (c *Collector) evict(currentSlot phase0.Slot) {
	// evict committee traces
	start := time.Now()
	threshold := currentSlot - slotTTL

	evicted := c.dumpCommitteeToDBPeriodically(threshold)
	c.logger.Info("evicted committee duty traces to disk", fields.Slot(threshold), zap.Int("count", evicted), fields.Took(time.Since(start)))

	// evict validator traces
	start = time.Now()
	evicted = c.dumpValidatorToDBPeriodically(threshold)
	c.logger.Info("evicted validator duty traces to disk", fields.Slot(threshold), zap.Int("count", evicted), fields.Took(time.Since(start)))

	// evict validator committee links
	start = time.Now()
	evicted = c.dumpLinkToDBPeriodically(threshold)
	c.logger.Info("evicted validator mappings to disk", fields.Slot(threshold), zap.Int("count", evicted), fields.Took(time.Since(start)))

	// remove old SC roots
	c.syncCommitteeRootsCache.DeleteExpired()

	// update last evicted slot
	c.lastEvictedSlot.Store(uint64(threshold))
}

func (c *Collector) getOrCreateValidatorTrace(slot phase0.Slot, role spectypes.BeaconRole, index phase0.ValidatorIndex) (*validatorDutyTrace, bool, error) {
	// check late arrival
	if uint64(slot) <= c.lastEvictedSlot.Load() {
		if _, found := c.inFlightValidator.GetOrSet(index, struct{}{}); found {
			return nil, false, errInFlight
		}

		trace, err := c.getValidatorDutyFromDiskIndex(role, slot, index)
		if errors.Is(err, store.ErrNotFound) {
			roleDutyTrace := &exporter.ValidatorDutyTrace{
				Slot: slot,
				Role: role,
			}
			wrappedTrace := &validatorDutyTrace{
				roles: []*exporter.ValidatorDutyTrace{roleDutyTrace},
			}
			return wrappedTrace, true, nil
		}
		if err != nil {
			_ = c.inFlightValidator.Delete(index)
			return nil, false, err
		}

		wrappedTrace := &validatorDutyTrace{
			roles: []*exporter.ValidatorDutyTrace{trace},
		}

		return wrappedTrace, true, nil
	}

	validatorSlots, found := c.validatorTraces.Get(index)
	if !found {
		validatorSlots, _ = c.validatorTraces.GetOrSet(index, hashmap.New[phase0.Slot, *validatorDutyTrace]())
	}

	traces, found := validatorSlots.Get(slot)

	if !found {
		roleDutyTrace := &exporter.ValidatorDutyTrace{
			Slot: slot,
			Role: role,
		}
		newTrace := &validatorDutyTrace{
			roles: []*exporter.ValidatorDutyTrace{roleDutyTrace},
		}
		traces, _ = validatorSlots.GetOrSet(slot, newTrace)
		return traces, false, nil
	}

	return traces, false, nil
}

var errInFlight = errors.New("in flight")

func (c *Collector) getOrCreateCommitteeTrace(slot phase0.Slot, committeeID spectypes.CommitteeID) (*committeeDutyTrace, bool, error) {
	// check late arrival
	if uint64(slot) <= c.lastEvictedSlot.Load() {
		if _, found := c.inFlightCommittee.GetOrSet(committeeID, struct{}{}); found {
			return nil, false, errInFlight
		}

		diskTrace, err := c.getCommitteeDutyFromDisk(slot, committeeID)
		if errors.Is(err, store.ErrNotFound) {
			trace := &committeeDutyTrace{
				CommitteeDutyTrace: exporter.CommitteeDutyTrace{
					CommitteeID: committeeID,
					Slot:        slot,
				},
			}
			return trace, true, nil
		}
		if err != nil {
			_ = c.inFlightCommittee.Delete(committeeID)
			return nil, false, fmt.Errorf("get late committee duty data: %w", err)
		}

		trace := &committeeDutyTrace{
			CommitteeDutyTrace: *diskTrace,
		}

		return trace, true, nil
	}

	committeeSlots, found := c.committeeTraces.Get(committeeID)
	if !found {
		committeeSlots, _ = c.committeeTraces.GetOrSet(committeeID, hashmap.New[phase0.Slot, *committeeDutyTrace]())
	}

	committeeTrace, found := committeeSlots.Get(slot)

	if !found {
		trace := &committeeDutyTrace{
			CommitteeDutyTrace: exporter.CommitteeDutyTrace{
				CommitteeID: committeeID,
				Slot:        slot,
			},
		}

		committeeTrace, _ = committeeSlots.GetOrSet(slot, trace)
	}

	return committeeTrace, false, nil
}

func (c *Collector) decodeJustificationWithPrepares(justifications [][]byte) []*exporter.QBFTTrace {
	var traces = make([]*exporter.QBFTTrace, 0, len(justifications))
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

		justificationTrace := exporter.QBFTTrace{
			Round:      uint64(qbftMsg.Round),
			BeaconRoot: qbftMsg.Root,
			Signer:     signedMsg.OperatorIDs[0],
		}

		traces = append(traces, &justificationTrace)
	}

	return traces
}

func (c *Collector) decodeJustificationWithRoundChanges(justifications [][]byte) []*exporter.RoundChangeTrace {
	var traces = make([]*exporter.RoundChangeTrace, 0, len(justifications))
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

func (c *Collector) createRoundChangeTrace(receivedAt uint64, msg *specqbft.Message, signedMsg *spectypes.SignedSSVMessage) *exporter.RoundChangeTrace {
	return &exporter.RoundChangeTrace{
		QBFTTrace: exporter.QBFTTrace{
			Round:        uint64(msg.Round),
			BeaconRoot:   msg.Root,
			Signer:       signedMsg.OperatorIDs[0],
			ReceivedTime: receivedAt,
		},
		PreparedRound:   uint64(msg.DataRound),
		PrepareMessages: c.decodeJustificationWithPrepares(msg.RoundChangeJustification),
	}
}

func (c *Collector) createProposalTrace(receivedAt uint64, msg *specqbft.Message, signedMsg *spectypes.SignedSSVMessage) *exporter.ProposalTrace {
	return &exporter.ProposalTrace{
		QBFTTrace: exporter.QBFTTrace{
			Round:        uint64(msg.Round),
			BeaconRoot:   msg.Root,
			Signer:       signedMsg.OperatorIDs[0],
			ReceivedTime: receivedAt,
		},
		RoundChanges:    c.decodeJustificationWithRoundChanges(msg.RoundChangeJustification),
		PrepareMessages: c.decodeJustificationWithPrepares(msg.PrepareJustification),
	}
}

func (c *Collector) processConsensus(receivedAt uint64, msg *specqbft.Message, signedMsg *spectypes.SignedSSVMessage, round *exporter.RoundTrace) *exporter.DecidedTrace {
	switch msg.MsgType {
	case specqbft.ProposalMsgType:
		round.ProposalTrace = c.createProposalTrace(receivedAt, msg, signedMsg)

	case specqbft.PrepareMsgType:
		prepare := &exporter.QBFTTrace{
			Round:        uint64(msg.Round),
			BeaconRoot:   msg.Root,
			Signer:       signedMsg.OperatorIDs[0],
			ReceivedTime: receivedAt,
		}

		round.Prepares = append(round.Prepares, prepare)

	case specqbft.CommitMsgType:
		if len(signedMsg.OperatorIDs) > 1 {
			return &exporter.DecidedTrace{
				Round:        uint64(msg.Round),
				BeaconRoot:   msg.Root,
				Signers:      signedMsg.OperatorIDs,
				ReceivedTime: receivedAt,
			}
		}

		commit := &exporter.QBFTTrace{
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

	return nil // we're exhausting all cases in the switch
}

func (c *Collector) processPartialSigCommittee(receivedAt uint64, msg *spectypes.PartialSignatureMessages, committeeID spectypes.CommitteeID, trace *committeeDutyTrace) {
	// add operator ids to the trace
	cmt, found := c.validators.Committee(committeeID)
	if found && len(cmt.Operators) > 0 {
		trace.OperatorIDs = cmt.Operators
	}

	signer := msg.Messages[0].Signer
	var attIdxs []phase0.ValidatorIndex
	var scIdxs []phase0.ValidatorIndex

	for _, partialSigMsg := range msg.Messages {
		root := partialSigMsg.SigningRoot
		if trace.roleRootsReady {
			if bytes.Equal(trace.syncCommitteeRoot[:], root[:]) {
				scIdxs = append(scIdxs, partialSigMsg.ValidatorIndex)
			} else if bytes.Equal(trace.attestationRoot[:], root[:]) {
				attIdxs = append(attIdxs, partialSigMsg.ValidatorIndex)
			} else {
				trace.addPending(root, signer, partialSigMsg.ValidatorIndex, receivedAt)
			}
			continue
		}
		// Not ready: buffer for later classification
		trace.addPending(root, signer, partialSigMsg.ValidatorIndex, receivedAt)
	}

	if len(scIdxs) > 0 {
		slices.Sort(scIdxs)
		scIdxs = slices.Compact(scIdxs)
		trace.SyncCommittee = append(trace.SyncCommittee, &exporter.SignerData{
			Signer:       signer,
			ValidatorIdx: scIdxs,
			ReceivedTime: receivedAt,
		})
	}
	if len(attIdxs) > 0 {
		slices.Sort(attIdxs)
		attIdxs = slices.Compact(attIdxs)
		trace.Attester = append(trace.Attester, &exporter.SignerData{
			Signer:       signer,
			ValidatorIdx: attIdxs,
			ReceivedTime: receivedAt,
		})
	}
}

func (c *Collector) saveValidatorToCommitteeLink(slot phase0.Slot, msg *spectypes.PartialSignatureMessages, committeeID spectypes.CommitteeID) {
	for _, msg := range msg.Messages {
		slotToCommittee, found := c.validatorIndexToCommitteeLinks.Get(msg.ValidatorIndex)
		if !found {
			slotToCommittee, _ = c.validatorIndexToCommitteeLinks.GetOrSet(msg.ValidatorIndex, hashmap.New[phase0.Slot, spectypes.CommitteeID]())
		}

		slotToCommittee.Set(slot, committeeID)
	}
}

func (c *Collector) saveLateValidatorToCommiteeLinks(slot phase0.Slot, msg *spectypes.PartialSignatureMessages, committeeID spectypes.CommitteeID) {
	for _, msg := range msg.Messages {
		if err := c.store.SaveCommitteeDutyLink(slot, msg.ValidatorIndex, committeeID); err != nil {
			// for late messages we can log with warn
			c.logger.Warn("save late validator to committee links", zap.Error(err), fields.Slot(slot), fields.ValidatorIndex(msg.ValidatorIndex), fields.CommitteeID(committeeID))
		}
	}
}

func (c *Collector) getSyncCommitteeRoot(ctx context.Context, slot phase0.Slot, in []byte) (phase0.Root, error) {
	var beaconVote = new(spectypes.BeaconVote)
	if err := beaconVote.Decode(in); err != nil {
		return phase0.Root{}, fmt.Errorf("decode beacon vote: %w", err)
	}

	key := scRootKey{slot: slot, blockRoot: beaconVote.BlockRoot}

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

		domain, err := c.client.DomainData(ctx, epoch, spectypes.DomainSyncCommittee)
		if err != nil {
			return phase0.Root{}, fmt.Errorf("get sync committee domain data: %w", err)
		}

		// Beacon root
		blockRoot := spectypes.SSZBytes(beaconVote.BlockRoot[:])
		signingRoot, err := spectypes.ComputeETHSigningRoot(blockRoot, domain)
		if err != nil {
			return phase0.Root{}, fmt.Errorf("compute sync committee root: %w", err)
		}

		_ = c.syncCommitteeRootsCache.Set(key, signingRoot, ttlcache.DefaultTTL)

		return signingRoot, nil
	})

	if err != nil {
		return phase0.Root{}, err
	}

	return val.(phase0.Root), nil
}

// computeRoleRoots derives both sync-committee and attestation signing roots
// from a proposal FullData (BeaconVote) for the given slot.
func (c *Collector) computeRoleRoots(ctx context.Context, slot phase0.Slot, in []byte) (phase0.Root, phase0.Root, error) {
	syncRoot, err := c.getSyncCommitteeRoot(ctx, slot, in)
	if err != nil {
		return phase0.Root{}, phase0.Root{}, err
	}

	var vote spectypes.BeaconVote
	if err := vote.Decode(in); err != nil {
		return phase0.Root{}, phase0.Root{}, fmt.Errorf("decode beacon vote: %w", err)
	}
	epoch := c.beacon.EstimatedEpochAtSlot(slot)
	domain, err := c.client.DomainData(ctx, epoch, spectypes.DomainAttester)
	if err != nil {
		return phase0.Root{}, phase0.Root{}, fmt.Errorf("get attester domain data: %w", err)
	}
	attData := &phase0.AttestationData{
		Slot:            slot,
		Index:           0, // Electra semantics (EIP-7549)
		BeaconBlockRoot: vote.BlockRoot,
		Source:          vote.Source,
		Target:          vote.Target,
	}
	attRoot, err := spectypes.ComputeETHSigningRoot(attData, domain)
	if err != nil {
		return phase0.Root{}, phase0.Root{}, fmt.Errorf("compute attester root: %w", err)
	}
	return syncRoot, attRoot, nil
}

func (c *Collector) Collect(ctx context.Context, msg *queue.SSVMessage, verifySig func(*spectypes.PartialSignatureMessages) error) error {
	err := c.collect(ctx, msg, verifySig)
	if errors.Is(err, errInFlight) {
		go c.collectLateMessage(ctx, msg, verifySig)
		return nil
	}
	return err
}

const maxRetryCount = 3

func (c *Collector) collectLateMessage(ctx context.Context, msg *queue.SSVMessage, verifySig func(*spectypes.PartialSignatureMessages) error) {
	var (
		err   error
		tries int
	)

	defer func() {
		if err != nil {
			c.logger.Error("collect late message", zap.Error(err), fields.MessageID(msg.MsgID))
		}
	}()

	// if another late message is in flight (for the same ID) - try `maxRetryCount` times before giving up
	for tries < maxRetryCount {
		err = c.collect(ctx, msg, verifySig)
		if !errors.Is(err, errInFlight) {
			return
		}
		tries++
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
	c.logger.Warn("exhausted retries for late message", fields.MessageID(msg.MsgID), zap.Int("tries", tries))
}

func (c *Collector) collect(ctx context.Context, msg *queue.SSVMessage, verifySig func(*spectypes.PartialSignatureMessages) error) error {
	start := time.Now()
	//nolint:gosec
	startTime := uint64(start.UnixMilli())

	tracerInFlightMessageCounter.Add(ctx, 1)
	defer func() {
		tracerInFlightMessageHist.Record(ctx, time.Since(start).Seconds())
	}()

	if msg.MsgType == spectypes.SSVConsensusMsgType {
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
			// committeeID is the last 16 bytes of the executorID
			copy(committeeID[:], executorID[16:])

			trace, late, err := c.getOrCreateCommitteeTrace(slot, committeeID)
			if err != nil {
				return err
			}

			trace.Lock()
			defer trace.Unlock()

			if len(msg.SignedSSVMessage.FullData) > 0 && subMsg.MsgType == specqbft.ProposalMsgType {
				// save proposal data and compute role roots
				trace.ProposalData = msg.SignedSSVMessage.FullData

				if syncRoot, attRoot, err := c.computeRoleRoots(ctx, slot, msg.SignedSSVMessage.FullData); err == nil {
					trace.syncCommitteeRoot = syncRoot
					trace.attestationRoot = attRoot
					trace.roleRootsReady = true
					trace.flushPending()
				} else {
					c.logger.Debug("compute role roots from proposal", zap.Error(err), fields.Slot(slot))
				}
			}

			round := getOrCreateRound(&trace.ConsensusTrace, uint64(subMsg.Round))

			decided := c.processConsensus(startTime, subMsg, msg.SignedSSVMessage, round)
			if decided != nil {
				trace.Decideds = append(trace.Decideds, decided)
			}

			if late {
				_ = c.inFlightCommittee.Delete(committeeID)
				return c.store.SaveCommitteeDuty(&trace.CommitteeDutyTrace)
			}

			return nil

		default:
			var validatorPK spectypes.ValidatorPK
			copy(validatorPK[:], executorID)

			bnRole, err := toBNRole(role)
			if err != nil {
				return err
			}

			// map pubkey to validator index for internal storage
			index, found := c.validators.ValidatorIndex(validatorPK)
			if !found {
				c.logger.Error("validator not found by pubkey", fields.Validator(validatorPK[:]))
				return fmt.Errorf("validator not found by pubkey: %x", validatorPK[:])
			}

			trace, late, err := c.getOrCreateValidatorTrace(slot, bnRole, index)
			if err != nil {
				return err
			}

			var qbftMsg = new(specqbft.Message)
			if err = qbftMsg.Decode(msg.Data); err == nil {
				if qbftMsg.MsgType == specqbft.ProposalMsgType {
					var data = new(spectypes.ValidatorConsensusData)
					if err := data.Decode(msg.SignedSSVMessage.FullData); err == nil {
						func() {
							trace.Lock()
							defer trace.Unlock()

							roleDutyTrace := trace.getOrCreate(slot, bnRole)

							if roleDutyTrace.Validator == 0 {
								roleDutyTrace.Validator = data.Duty.ValidatorIndex
							}
							// non-committee duty will contain the proposal data
							roleDutyTrace.ProposalData = data.DataSSZ
						}()
					}
				}
			}

			trace.Lock()
			defer trace.Unlock()

			roleDutyTrace := trace.getOrCreate(slot, bnRole)

			round := getOrCreateRound(&roleDutyTrace.ConsensusTrace, uint64(subMsg.Round))

			decided := c.processConsensus(startTime, subMsg, msg.SignedSSVMessage, round)
			if decided != nil {
				roleDutyTrace.Decideds = append(roleDutyTrace.Decideds, decided)
			}

			if late {
				_ = c.inFlightValidator.Delete(index)
				return c.store.SaveValidatorDuty(roleDutyTrace)
			}

			return nil
		}
	}

	if msg.MsgType == spectypes.SSVPartialSignatureMsgType {
		logger := c.logger.With(zap.String("msg_id", msg.MsgID.String()))

		pSigMessages := new(spectypes.PartialSignatureMessages)
		err := pSigMessages.Decode(msg.SignedSSVMessage.SSVMessage.GetData())
		if err != nil {
			return fmt.Errorf("decode partial signature messages: %w", err)
		}

		if len(pSigMessages.Messages) == 0 {
			return fmt.Errorf("no partial sig messages")
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

		// process partial sig for committee
		if msg.MsgID.GetRoleType() == spectypes.RoleCommittee {
			var committeeID spectypes.CommitteeID
			// committeeID is the last 16 bytes of the executorID
			copy(committeeID[:], executorID[16:])

			slot := pSigMessages.Slot

			trace, late, err := c.getOrCreateCommitteeTrace(slot, committeeID)
			if err != nil {
				return err
			}

			trace.Lock()
			defer trace.Unlock()

			c.processPartialSigCommittee(startTime, pSigMessages, committeeID, trace)
			c.checkAndPublishQuorum(logger, pSigMessages, committeeID, trace)

			if late {
				_ = c.inFlightCommittee.Delete(committeeID)
				c.saveLateValidatorToCommiteeLinks(slot, pSigMessages, committeeID)
				return c.store.SaveCommitteeDuty(&trace.CommitteeDutyTrace)
			}

			// cache the link between validator index and committee id
			c.saveValidatorToCommitteeLink(slot, pSigMessages, committeeID)
			return nil
		}

		// process partial sig for validator
		role, err := toBNRole(msg.MsgID.GetRoleType())
		if err != nil {
			return err
		}

		trace, late, err := c.getOrCreateValidatorTrace(pSigMessages.Slot, role, pSigMessages.Messages[0].ValidatorIndex)
		if err != nil {
			return err
		}

		trace.Lock()
		defer trace.Unlock()

		roleDutyTrace := trace.getOrCreate(pSigMessages.Slot, role)

		if roleDutyTrace.Validator == 0 {
			roleDutyTrace.Validator = pSigMessages.Messages[0].ValidatorIndex
		}

		tr := &exporter.PartialSigTrace{
			Type:         pSigMessages.Type,
			BeaconRoot:   pSigMessages.Messages[0].SigningRoot,
			Signer:       pSigMessages.Messages[0].Signer,
			ReceivedTime: startTime,
		}

		if pSigMessages.Type == spectypes.PostConsensusPartialSig {
			roleDutyTrace.Post = append(roleDutyTrace.Post, tr)
		} else {
			roleDutyTrace.Pre = append(roleDutyTrace.Pre, tr)
		}

		if late {
			_ = c.inFlightValidator.Delete(pSigMessages.Messages[0].ValidatorIndex)
			return c.store.SaveValidatorDuty(roleDutyTrace)
		}

		return nil
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
	roles []*exporter.ValidatorDutyTrace
}

func (dt *validatorDutyTrace) getOrCreate(slot phase0.Slot, role spectypes.BeaconRole) *exporter.ValidatorDutyTrace {
	// find the trace for the role
	for _, t := range dt.roles {
		if t.Role == role {
			return t
		}
	}

	// or create a new one
	roleDutyTrace := &exporter.ValidatorDutyTrace{
		Slot: slot,
		Role: role,
	}
	dt.roles = append(dt.roles, roleDutyTrace)

	return roleDutyTrace
}

func (dt *validatorDutyTrace) roleTraces() (roles []*exporter.ValidatorDutyTrace) {
	dt.Lock()
	defer dt.Unlock()

	for _, role := range dt.roles {
		roles = append(roles, role.DeepCopy())
	}

	return
}

type committeeDutyTrace struct {
	sync.Mutex
	// Derived roots for classifying committee partial signatures
	syncCommitteeRoot phase0.Root
	attestationRoot   phase0.Root
	roleRootsReady    bool
	exporter.CommitteeDutyTrace

	// Track published quorums to avoid duplicates (validator -> role -> signers hash)
	// Not part of the model.CommitteeDutyTrace, because it's not persisted to disk
	publishedQuorums map[phase0.ValidatorIndex]map[spectypes.BeaconRole]string

	// Pending signatures grouped by SigningRoot and signer until role roots are known.
	// Shape: pendingByRoot[root][signer][receivedAt] = []validatorIndices
	pendingByRoot map[phase0.Root]map[spectypes.OperatorID]map[uint64][]phase0.ValidatorIndex
}

func (dt *committeeDutyTrace) trace() *exporter.CommitteeDutyTrace {
	dt.Lock()
	defer dt.Unlock()
	return dt.DeepCopy()
}

// addPending buffers a validator index for a given root and signer.
func (dt *committeeDutyTrace) addPending(root phase0.Root, signer spectypes.OperatorID, idx phase0.ValidatorIndex, receivedAt uint64) {
	if dt.pendingByRoot == nil {
		dt.pendingByRoot = make(map[phase0.Root]map[spectypes.OperatorID]map[uint64][]phase0.ValidatorIndex)
	}
	m := dt.pendingByRoot[root]
	if m == nil {
		m = make(map[spectypes.OperatorID]map[uint64][]phase0.ValidatorIndex)
		dt.pendingByRoot[root] = m
	}
	buckets := m[signer]
	if buckets == nil {
		buckets = make(map[uint64][]phase0.ValidatorIndex)
		m[signer] = buckets
	}
	buckets[receivedAt] = append(buckets[receivedAt], idx)
}

// flushPending routes buffered entries into Attester/SyncCommittee buckets
// according to derived role roots. Caller must hold dt.Lock().
func (dt *committeeDutyTrace) flushPending() {
	if len(dt.pendingByRoot) == 0 {
		return
	}
	for root, perSigner := range dt.pendingByRoot {
		var role spectypes.BeaconRole
		switch root {
		case dt.syncCommitteeRoot:
			role = spectypes.BNRoleSyncCommittee
		case dt.attestationRoot:
			role = spectypes.BNRoleAttester
		default:
			// Unknown root; keep buffered
			continue
		}
		for signer, byTs := range perSigner {
			if len(byTs) == 0 {
				continue
			}
			// For each timestamp bucket, sort/compact indices and emit a SignerData record
			for ts, idxs := range byTs {
				if len(idxs) == 0 {
					continue
				}
				slices.Sort(idxs)
				idxs = slices.Compact(idxs)
				sd := &exporter.SignerData{Signer: signer, ValidatorIdx: idxs, ReceivedTime: ts}
				if role == spectypes.BNRoleSyncCommittee {
					dt.SyncCommittee = append(dt.SyncCommittee, sd)
				} else {
					dt.Attester = append(dt.Attester, sd)
				}
			}
		}
		delete(dt.pendingByRoot, root)
	}
}

func getOrCreateRound(trace *exporter.ConsensusTrace, rnd uint64) *exporter.RoundTrace {
	var count = len(trace.Rounds)
	for rnd > uint64(count) { //nolint:gosec
		trace.Rounds = append(trace.Rounds, &exporter.RoundTrace{})
		count = len(trace.Rounds)
	}

	return trace.Rounds[rnd-1]
}

// checkAndPublishQuorum detects when quorum is reached and publishes decisions to websocket.
// IMPORTANT: trace must be locked by the caller before calling this function.
func (c *Collector) checkAndPublishQuorum(logger *zap.Logger, msg *spectypes.PartialSignatureMessages, committeeID spectypes.CommitteeID, trace *committeeDutyTrace) {
	if c.decidedListenerFunc == nil {
		return
	}

	committee, found := c.validators.Committee(committeeID)
	if !found || len(committee.Operators) == 0 {
		return
	}

	threshold := uint64(len(committee.Operators))*2/3 + 1

	if trace.publishedQuorums == nil {
		trace.publishedQuorums = make(map[phase0.ValidatorIndex]map[spectypes.BeaconRole]string)
	}

	// Check each validator in the partial signature for quorum
	for _, partialMsg := range msg.Messages {
		_, exists := c.validators.ValidatorByIndex(partialMsg.ValidatorIndex)
		if !exists {
			logger.Debug("validator not found by index",
				zap.Uint64("validator_index", uint64(partialMsg.ValidatorIndex)))
			continue
		}
		// Initialize tracking for this validator if needed
		if trace.publishedQuorums[partialMsg.ValidatorIndex] == nil {
			trace.publishedQuorums[partialMsg.ValidatorIndex] = make(map[spectypes.BeaconRole]string)
		}

		// Check quorum for both attester and sync committee roles
		c.checkAndPublishQuorumForRole(logger, trace, spectypes.BNRoleAttester, msg, partialMsg, threshold)
		c.checkAndPublishQuorumForRole(logger, trace, spectypes.BNRoleSyncCommittee, msg, partialMsg, threshold)
	}
}

// checkAndPublishQuorumForRole checks if quorum is reached for a specific role and publishes if it's the first time
func (c *Collector) checkAndPublishQuorumForRole(
	logger *zap.Logger,
	trace *committeeDutyTrace,
	role spectypes.BeaconRole,
	msg *spectypes.PartialSignatureMessages,
	partialMsg *spectypes.PartialSignatureMessage,
	threshold uint64,
) {
	var signerData []*exporter.SignerData

	switch role {
	case spectypes.BNRoleAttester:
		signerData = trace.Attester
	case spectypes.BNRoleSyncCommittee:
		signerData = trace.SyncCommittee
	default:
		return
	}

	signers := c.countUniqueSignersForValidatorAndRoot(logger, signerData, partialMsg.ValidatorIndex, partialMsg.SigningRoot)
	if uint64(len(signers)) < threshold {
		return
	}

	signersKey := c.signersToKey(signers)
	lastPublished := trace.publishedQuorums[partialMsg.ValidatorIndex][role]

	// Only publish the FIRST time quorum is reached, not for every signer set change
	if lastPublished == "" {
		trace.publishedQuorums[partialMsg.ValidatorIndex][role] = signersKey

		decidedInfo := DecidedInfo{
			Index:   partialMsg.ValidatorIndex,
			Slot:    msg.Slot,
			Role:    role,
			Signers: signers,
		}
		c.decidedListenerFunc(decidedInfo)
	}
}

// countUniqueSignersForValidatorAndRoot counts unique signers for a specific validator and signing root
func (c *Collector) countUniqueSignersForValidatorAndRoot(logger *zap.Logger, signerData []*exporter.SignerData, validatorIndex phase0.ValidatorIndex, _ phase0.Root) []spectypes.OperatorID {
	signers := make(map[spectypes.OperatorID]struct{})

	for _, data := range signerData {
		if slices.Contains(data.ValidatorIdx, validatorIndex) {
			signers[data.Signer] = struct{}{}
		}
	}

	// Convert map to sorted slice
	result := make([]spectypes.OperatorID, 0, len(signers))
	for signer := range signers {
		result = append(result, signer)
	}
	slices.Sort(result)

	return result
}

// signersToKey creates a string key from sorted signers for deduplication
func (c *Collector) signersToKey(signers []spectypes.OperatorID) string {
	parts := make([]string, 0, len(signers))
	for _, signer := range signers {
		parts = append(parts, fmt.Sprintf("%d", signer))
	}
	return strings.Join(parts, ",")
}

// SaveScheduled stores a compact schedule map for a slot (pass-through to disk store).
func (c *Collector) SaveScheduled(slot phase0.Slot, schedule map[phase0.ValidatorIndex]rolemask.Mask) error {
	if c.store == nil {
		return fmt.Errorf("store not initialized")
	}
	return c.store.SaveScheduled(slot, schedule)
}

// GetScheduled loads the compact schedule for a slot (pass-through to disk store).
func (c *Collector) GetScheduled(slot phase0.Slot) (map[phase0.ValidatorIndex]rolemask.Mask, error) {
	if c.store == nil {
		return nil, fmt.Errorf("store not initialized")
	}
	return c.store.GetScheduled(slot)
}

// startScheduleFiller runs a background loop that derives scheduled duties
// from the duty store each slot and persists them compactly.
func (c *Collector) startScheduleFiller(ctx context.Context, tickerProvider slotticker.Provider) {
	if c.duties == nil || c.beacon == nil || c.store == nil {
		c.logger.Debug("schedule filler disabled (missing deps)")
		return
	}
	t := tickerProvider()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.Next():
			slot := t.Slot()
			// Enqueue current slot quickly; if queue is full, drop to avoid backpressure.
			select {
			case c.scheduleJobs <- slot:
			default:
			}
			// Enqueue a tiny backfill (previous slot) to reduce races if queue permits.
			if slot > 0 {
				select {
				case c.scheduleJobs <- slot - 1:
				default:
				}
			}
		}
	}
}

// computeAndPersistScheduleForSlot builds a per-slot role mask map from dutystore
// for (ATTESTER, PROPOSER, SYNC_COMMITTEE). Idempotent and best-effort.
func (c *Collector) computeAndPersistScheduleForSlot(slot phase0.Slot) error {
	epoch := c.beacon.EstimatedEpochAtSlot(slot)
	schedule := make(map[phase0.ValidatorIndex]rolemask.Mask)

	// Attester indices for this slot (InCommittee only)
	if c.duties.Attester != nil {
		for _, d := range c.duties.Attester.CommitteeSlotDuties(epoch, slot) {
			// d may be nil in edge cases; guard defensively
			if d == nil {
				continue
			}
			schedule[d.ValidatorIndex] |= rolemask.BitAttester
		}
	}

	// Proposer indices for this slot (all scheduled proposers)
	if c.duties.Proposer != nil {
		for _, idx := range c.duties.Proposer.SlotIndices(epoch, slot) {
			schedule[idx] |= rolemask.BitProposer
		}
	}

	// Sync-committee membership for this slot (period members are scheduled every slot)
	if c.duties.SyncCommittee != nil {
		period := c.beacon.EstimatedSyncCommitteePeriodAtEpoch(epoch)
		for _, sc := range c.duties.SyncCommittee.CommitteePeriodDuties(period) {
			if sc == nil {
				continue
			}
			schedule[sc.ValidatorIndex] |= rolemask.BitSyncCommittee
		}
	}

	if len(schedule) == 0 {
		return nil
	}
	return c.store.SaveScheduled(slot, schedule)
}

// runScheduleWorker performs schedule computations and DB writes off the hot path.
func (c *Collector) runScheduleWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case s := <-c.scheduleJobs:
			if err := c.computeAndPersistScheduleForSlot(s); err != nil {
				c.logger.Debug("schedule worker compute/persist", fields.Slot(s), zap.Error(err))
			}
		}
	}
}
