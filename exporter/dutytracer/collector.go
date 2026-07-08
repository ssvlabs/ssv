package dutytracer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter/rolemask"
	"github.com/ssvlabs/ssv/exporter/store"
	"github.com/ssvlabs/ssv/exporter/traces"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/operator/slotticker"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

type DecidedInfo struct {
	Index   phase0.ValidatorIndex
	Slot    phase0.Slot
	Role    spectypes.BeaconRole
	Signers []spectypes.OperatorID
}

type committeeTraceKey struct {
	id   spectypes.CommitteeID
	role spectypes.RunnerRole
}

type Collector struct {
	logger *zap.Logger

	// committeeID+role:slot:committeeDutyTrace (role separates committee vs aggregator-committee duties).
	committeeTraces *hashmap.Map[committeeTraceKey, *hashmap.Map[phase0.Slot, *committeeDutyTrace]]

	// validatorIndex:slot:validatorDutyTrace
	validatorTraces *hashmap.Map[phase0.ValidatorIndex, *hashmap.Map[phase0.Slot, *validatorDutyTrace]]

	// validatorIndex:slot:committeeID
	validatorIndexToCommitteeLinks *hashmap.Map[phase0.ValidatorIndex, *hashmap.Map[phase0.Slot, spectypes.CommitteeID]]

	syncCommitteeRootsCache *ttlcache.Cache[scRootKey, phase0.Root]
	syncCommitteeRootsSf    singleflight.Group

	aggregatorSelectionRootsCache *ttlcache.Cache[phase0.Slot, phase0.Root]
	aggregatorSelectionRootsSf    singleflight.Group

	beacon *networkconfig.Beacon

	store      DutyTraceStore
	client     DomainDataProvider
	validators registrystorage.ValidatorStore

	lastEvictedSlot atomic.Uint64

	inFlightCommittee hashmap.Map[committeeTraceKey, struct{}]
	inFlightValidator hashmap.Map[phase0.ValidatorIndex, struct{}]

	decidedListenerFunc func(msg DecidedInfo)

	// duties is the runtime duty store used to derive per-slot scheduled roles.
	duties *dutystore.Store

	// scheduleJobs is a bounded queue for async schedule computation to avoid
	// blocking the hot path and DB with synchronous writes.
	scheduleJobs chan phase0.Slot

	// lateWG tracks the detached goroutines Collect spawns to retry late messages
	// (they persist to the DB). Start joins them on shutdown so a retry can't write
	// to a closed DB; lateClosed (under lateMu) refuses new spawns once that join
	// has begun, which keeps lateWG.Go's Add from racing lateWG.Wait. The close is
	// one-way: Start runs once per Collector and never re-arms lateClosed.
	lateWG     sync.WaitGroup
	lateMu     sync.Mutex
	lateClosed bool
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
		committeeTraces:                hashmap.New[committeeTraceKey, *hashmap.Map[phase0.Slot, *committeeDutyTrace]](),
		validatorTraces:                hashmap.New[phase0.ValidatorIndex, *hashmap.Map[phase0.Slot, *validatorDutyTrace]](),
		validatorIndexToCommitteeLinks: hashmap.New[phase0.ValidatorIndex, *hashmap.Map[phase0.Slot, spectypes.CommitteeID]](),
		syncCommitteeRootsCache:        ttlcache.New(ttlcache.WithTTL[scRootKey, phase0.Root](ttl)),
		aggregatorSelectionRootsCache:  ttlcache.New(ttlcache.WithTTL[phase0.Slot, phase0.Root](ttl)),
		inFlightCommittee:              hashmap.Map[committeeTraceKey, struct{}]{},
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

// Start runs the eviction loop until ctx is canceled, then waits for the goroutines
// tied to the collector's lifetime — the schedule filler/worker and any in-flight
// late-message retries — to return before returning itself. A caller that closes the
// DB the collector writes to must wait for Start to return first: otherwise an
// in-flight evict, schedule, or late-message write can run against a closed DB and panic.
func (c *Collector) Start(ctx context.Context, tickerProvider slotticker.Provider) {
	c.logger.Info("start duty tracer cache to disk evictor")
	ticker := tickerProvider()

	// The filler and worker share Start's lifetime; join them on the way out so no
	// schedule write outlives Start (runScheduleWorker persists schedules to disk).
	var helpers sync.WaitGroup
	// Start schedule filler in a separate goroutine to avoid blocking eviction.
	helpers.Go(func() {
		c.startScheduleFiller(ctx, tickerProvider)
	})
	// Start a single worker to process schedule writes asynchronously.
	helpers.Go(func() {
		c.runScheduleWorker(ctx)
	})

	for {
		select {
		case <-ctx.Done():
			// Join the filler/worker first, then the late-message retries. This
			// ordering is deadlock-free only because the late path (collect) never
			// enqueues to scheduleJobs; if it did, a late goroutine could block on a
			// full scheduleJobs after the worker has exited, hanging stopLateCollect
			// (and thus shutdown).
			helpers.Wait()
			c.stopLateCollect()
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

	// CRITICAL: Update lastEvictedSlot BEFORE dumping to ensure messages arriving
	// during eviction take the late path and fetch from disk instead of creating
	// new incomplete in-memory traces
	c.lastEvictedSlot.Store(uint64(threshold))

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

	// remove expired signing-root caches
	c.syncCommitteeRootsCache.DeleteExpired()
	c.aggregatorSelectionRootsCache.DeleteExpired()
}

func (c *Collector) getOrCreateValidatorTrace(slot phase0.Slot, role spectypes.BeaconRole, index phase0.ValidatorIndex) (*validatorDutyTrace, bool, error) {
	// check late arrival
	if uint64(slot) <= c.lastEvictedSlot.Load() {
		if _, found := c.inFlightValidator.GetOrSet(index, struct{}{}); found {
			return nil, false, errInFlight
		}

		trace, err := c.getValidatorDutyFromDiskIndex(role, slot, index)
		if errors.Is(err, store.ErrNotFound) {
			roleDutyTrace := &traces.ValidatorDutyTrace{
				Slot: slot,
				Role: role,
			}
			wrappedTrace := &validatorDutyTrace{
				roles: []*traces.ValidatorDutyTrace{roleDutyTrace},
			}
			return wrappedTrace, true, nil
		}
		if err != nil {
			_ = c.inFlightValidator.Delete(index)
			return nil, false, err
		}

		wrappedTrace := &validatorDutyTrace{
			roles: []*traces.ValidatorDutyTrace{trace},
		}

		return wrappedTrace, true, nil
	}

	validatorSlots, found := c.validatorTraces.Get(index)
	if !found {
		validatorSlots, _ = c.validatorTraces.GetOrSet(index, hashmap.New[phase0.Slot, *validatorDutyTrace]())
	}

	slotTrace, found := validatorSlots.Get(slot)

	if !found {
		roleDutyTrace := &traces.ValidatorDutyTrace{
			Slot: slot,
			Role: role,
		}
		newTrace := &validatorDutyTrace{
			roles: []*traces.ValidatorDutyTrace{roleDutyTrace},
		}
		slotTrace, _ = validatorSlots.GetOrSet(slot, newTrace)
		return slotTrace, false, nil
	}

	return slotTrace, false, nil
}

var errInFlight = errors.New("in flight")

func (c *Collector) getOrCreateCommitteeTrace(slot phase0.Slot, committeeID spectypes.CommitteeID, role spectypes.RunnerRole) (*committeeDutyTrace, bool, error) {
	key := committeeTraceKey{id: committeeID, role: role}
	// check late arrival
	if uint64(slot) <= c.lastEvictedSlot.Load() {
		if _, found := c.inFlightCommittee.GetOrSet(key, struct{}{}); found {
			return nil, false, errInFlight
		}

		diskTrace, err := c.getCommitteeDutyFromDisk(slot, role, committeeID)
		if errors.Is(err, store.ErrNotFound) {
			trace := &committeeDutyTrace{
				CommitteeDutyTrace: traces.CommitteeDutyTrace{
					CommitteeID: committeeID,
					Slot:        slot,
					Role:        role,
				},
			}
			return trace, true, nil
		}
		if err != nil {
			_ = c.inFlightCommittee.Delete(key)
			return nil, false, fmt.Errorf("get late committee duty data: %w", err)
		}

		trace := &committeeDutyTrace{
			CommitteeDutyTrace: *diskTrace,
		}

		return trace, true, nil
	}

	committeeSlots, found := c.committeeTraces.Get(key)
	if !found {
		committeeSlots, _ = c.committeeTraces.GetOrSet(key, hashmap.New[phase0.Slot, *committeeDutyTrace]())
	}

	committeeTrace, found := committeeSlots.Get(slot)

	if !found {
		trace := &committeeDutyTrace{
			CommitteeDutyTrace: traces.CommitteeDutyTrace{
				CommitteeID: committeeID,
				Slot:        slot,
				Role:        role,
			},
		}

		committeeTrace, _ = committeeSlots.GetOrSet(slot, trace)
	}

	return committeeTrace, false, nil
}

func (c *Collector) decodeJustificationWithPrepares(justifications [][]byte) []*traces.QBFTTrace {
	out := make([]*traces.QBFTTrace, 0, len(justifications))
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

		justificationTrace := traces.QBFTTrace{
			Round:      uint64(qbftMsg.Round),
			BeaconRoot: qbftMsg.Root,
			Signer:     signedMsg.OperatorIDs[0],
		}

		out = append(out, &justificationTrace)
	}

	return out
}

func (c *Collector) decodeJustificationWithRoundChanges(justifications [][]byte) []*traces.RoundChangeTrace {
	out := make([]*traces.RoundChangeTrace, 0, len(justifications))
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
		out = append(out, roundChangeTrace)
	}

	return out
}

func (c *Collector) createRoundChangeTrace(receivedAt uint64, msg *specqbft.Message, signedMsg *spectypes.SignedSSVMessage) *traces.RoundChangeTrace {
	return &traces.RoundChangeTrace{
		QBFTTrace: traces.QBFTTrace{
			Round:        uint64(msg.Round),
			BeaconRoot:   msg.Root,
			Signer:       signedMsg.OperatorIDs[0],
			ReceivedTime: receivedAt,
		},
		PreparedRound:   uint64(msg.DataRound),
		PrepareMessages: c.decodeJustificationWithPrepares(msg.RoundChangeJustification),
	}
}

func (c *Collector) createProposalTrace(receivedAt uint64, msg *specqbft.Message, signedMsg *spectypes.SignedSSVMessage) *traces.ProposalTrace {
	return &traces.ProposalTrace{
		QBFTTrace: traces.QBFTTrace{
			Round:        uint64(msg.Round),
			BeaconRoot:   msg.Root,
			Signer:       signedMsg.OperatorIDs[0],
			ReceivedTime: receivedAt,
		},
		RoundChanges:    c.decodeJustificationWithRoundChanges(msg.RoundChangeJustification),
		PrepareMessages: c.decodeJustificationWithPrepares(msg.PrepareJustification),
	}
}

func (c *Collector) processConsensus(receivedAt uint64, msg *specqbft.Message, signedMsg *spectypes.SignedSSVMessage, round *traces.RoundTrace) *traces.DecidedTrace {
	switch msg.MsgType {
	case specqbft.ProposalMsgType:
		round.ProposalTrace = c.createProposalTrace(receivedAt, msg, signedMsg)

	case specqbft.PrepareMsgType:
		prepare := &traces.QBFTTrace{
			Round:        uint64(msg.Round),
			BeaconRoot:   msg.Root,
			Signer:       signedMsg.OperatorIDs[0],
			ReceivedTime: receivedAt,
		}

		round.Prepares = append(round.Prepares, prepare)

	case specqbft.CommitMsgType:
		if len(signedMsg.OperatorIDs) > 1 {
			return &traces.DecidedTrace{
				Round:        uint64(msg.Round),
				BeaconRoot:   msg.Root,
				Signers:      signedMsg.OperatorIDs,
				ReceivedTime: receivedAt,
			}
		}

		commit := &traces.QBFTTrace{
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

func (c *Collector) processPartialSigCommittee(
	receivedAt uint64,
	msg *spectypes.PartialSignatureMessages,
	trace *committeeDutyTrace,
	aggSelectionRoot phase0.Root,
	aggSelectionRootReady bool,
) {
	// add operator ids to the trace
	cmt, found := c.validators.Committee(trace.CommitteeID)
	if found && len(cmt.Operators) > 0 {
		trace.OperatorIDs = cmt.Operators
	}

	signer := ssvtypes.PartialSigMsgSigner(msg)
	var attIdxs []phase0.ValidatorIndex
	var scIdxs []phase0.ValidatorIndex

	if msg.Type == spectypes.AggregatorCommitteePartialSig && aggSelectionRootReady && !trace.aggSelectionRootReady {
		trace.aggSelectionRoot = aggSelectionRoot
		trace.aggSelectionRootReady = true
	}

	for _, partialSigMsg := range msg.Messages {
		root := partialSigMsg.SigningRoot
		trace.addRootSigner(root, partialSigMsg.ValidatorIndex, signer)
		if msg.Type == spectypes.AggregatorCommitteePartialSig && trace.aggSelectionRootReady {
			if bytes.Equal(trace.aggSelectionRoot[:], root[:]) {
				attIdxs = append(attIdxs, partialSigMsg.ValidatorIndex)
			} else {
				scIdxs = append(scIdxs, partialSigMsg.ValidatorIndex)
			}
			continue
		}

		if role, ok := trace.classifyRootForPending(root); ok {
			if role == spectypes.BNRoleSyncCommittee || role == spectypes.BNRoleSyncCommitteeContribution {
				scIdxs = append(scIdxs, partialSigMsg.ValidatorIndex)
			} else {
				attIdxs = append(attIdxs, partialSigMsg.ValidatorIndex)
			}
			continue
		}

		// Not ready: buffer for later classification
		trace.addPending(root, signer, partialSigMsg.ValidatorIndex, receivedAt)
	}

	if len(scIdxs) > 0 {
		slices.Sort(scIdxs)
		scIdxs = slices.Compact(scIdxs)
		trace.SyncCommittee = append(trace.SyncCommittee, &traces.SignerData{
			Signer:       signer,
			ValidatorIdx: scIdxs,
			ReceivedTime: receivedAt,
		})
	}
	if len(attIdxs) > 0 {
		slices.Sort(attIdxs)
		attIdxs = slices.Compact(attIdxs)
		trace.Attester = append(trace.Attester, &traces.SignerData{
			Signer:       signer,
			ValidatorIdx: attIdxs,
			ReceivedTime: receivedAt,
		})
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

func (c *Collector) getAggregatorSelectionRoot(ctx context.Context, slot phase0.Slot) (phase0.Root, error) {
	if item := c.aggregatorSelectionRootsCache.Get(slot); item != nil {
		return item.Value(), nil
	}

	val, err, _ := c.aggregatorSelectionRootsSf.Do(strconv.FormatUint(uint64(slot), 10), func() (any, error) {
		if item := c.aggregatorSelectionRootsCache.Get(slot); item != nil {
			return item.Value(), nil
		}

		epoch := c.beacon.EstimatedEpochAtSlot(slot)
		domain, domainErr := c.client.DomainData(ctx, epoch, spectypes.DomainSelectionProof)
		if domainErr != nil {
			return phase0.Root{}, fmt.Errorf("get aggregator selection domain data: %w", domainErr)
		}
		root, rootErr := spectypes.ComputeETHSigningRoot(spectypes.SSZUint64(slot), domain)
		if rootErr != nil {
			return phase0.Root{}, fmt.Errorf("compute aggregator selection root: %w", rootErr)
		}

		_ = c.aggregatorSelectionRootsCache.Set(slot, root, ttlcache.DefaultTTL)
		return root, nil
	})
	if err != nil {
		return phase0.Root{}, err
	}

	return val.(phase0.Root), nil
}

// computeAggregatorCommitteePostConsensusRoles derives the beacon role for each
// signing root present in an AggregatorCommittee proposal's consensus data, so
// post-consensus partial signatures can be classified into Attester/SyncCommittee
// buckets (Aggregator -> Attester bucket, SyncCommitteeContribution -> SyncCommittee bucket).
func (c *Collector) computeAggregatorCommitteePostConsensusRoles(
	ctx context.Context,
	slot phase0.Slot,
	in []byte,
) (map[phase0.Root]spectypes.BeaconRole, error) {
	data := &spectypes.AggregatorCommitteeConsensusData{}
	if err := data.Decode(in); err != nil {
		return nil, fmt.Errorf("decode aggregator committee consensus data: %w", err)
	}

	epoch := c.beacon.EstimatedEpochAtSlot(slot)
	roleByRoot := make(map[phase0.Root]spectypes.BeaconRole)

	aggregateAndProofs, err := data.GetAggregateAndProofs()
	if err != nil {
		return nil, fmt.Errorf("get aggregate and proofs: %w", err)
	}

	dAgg, err := c.client.DomainData(ctx, epoch, spectypes.DomainAggregateAndProof)
	if err != nil {
		return nil, fmt.Errorf("get aggregate and proof domain data: %w", err)
	}
	for _, aggAndProof := range aggregateAndProofs {
		hashRoot, err := spectypes.GetAggregateAndProofHashRoot(aggAndProof)
		if err != nil {
			c.logger.Warn("failed to get aggregate-and-proof hash root",
				zap.Error(err),
				fields.Slot(slot))
			continue
		}
		root, err := spectypes.ComputeETHSigningRoot(hashRoot, dAgg)
		if err != nil {
			c.logger.Warn("failed to compute aggregate-and-proof signing root",
				zap.Error(err),
				fields.Slot(slot))
			continue
		}
		roleByRoot[root] = spectypes.BNRoleAggregator
	}

	contribs, err := data.GetSyncCommitteeContributions()
	if err != nil {
		return nil, fmt.Errorf("get sync committee contributions: %w", err)
	}

	dContrib, err := c.client.DomainData(ctx, epoch, spectypes.DomainContributionAndProof)
	if err != nil {
		return nil, fmt.Errorf("get contribution and proof domain data: %w", err)
	}

	for i, contrib := range contribs {
		cp := &altair.ContributionAndProof{
			AggregatorIndex: data.Contributors[i].ValidatorIndex,
			Contribution:    &contrib.Contribution,
			SelectionProof:  data.Contributors[i].SelectionProof,
		}
		root, err := spectypes.ComputeETHSigningRoot(cp, dContrib)
		if err != nil {
			c.logger.Warn("failed to compute sync-committee-contribution signing root",
				zap.Error(err),
				fields.Slot(slot),
				zap.Int("contribution_index", i))
			continue
		}
		roleByRoot[root] = spectypes.BNRoleSyncCommitteeContribution
	}

	return roleByRoot, nil
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
		c.collectLateAsync(ctx, msg, verifySig)
		return nil
	}
	return err
}

// collectLateAsync retries a late message in a tracked goroutine so Start can join
// it on shutdown. Once shutdown has begun (Start is joining), it drops the retry
// rather than spawn a goroutine that could outlive — and write to — a closed DB.
func (c *Collector) collectLateAsync(ctx context.Context, msg *queue.SSVMessage, verifySig func(*spectypes.PartialSignatureMessages) error) {
	c.lateMu.Lock()
	defer c.lateMu.Unlock()
	if c.lateClosed {
		return
	}
	c.lateWG.Go(func() {
		c.collectLateMessage(ctx, msg, verifySig)
	})
}

// stopLateCollect refuses further late-message goroutines and waits for the
// in-flight ones to return. Start calls it once ctx is canceled, before returning,
// so the caller can close the DB without racing a late write.
func (c *Collector) stopLateCollect() {
	c.lateMu.Lock()
	c.lateClosed = true
	c.lateMu.Unlock()
	c.lateWG.Wait()
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

type partialSigVerifyCtx struct {
	logger          *zap.Logger
	runnerRole      spectypes.RunnerRole
	slot            phase0.Slot
	signer          spectypes.OperatorID
	root            phase0.Root
	committeeID     spectypes.CommitteeID
	partialMsgsSize int
}

func (c *Collector) newPartialSigVerifyCtx(msg *queue.SSVMessage, pSigMessages *spectypes.PartialSignatureMessages) partialSigVerifyCtx {
	runnerRole := msg.MsgID.GetRoleType()
	slot := pSigMessages.Slot
	var signer spectypes.OperatorID
	var root phase0.Root
	if len(pSigMessages.Messages) > 0 {
		signer = ssvtypes.PartialSigMsgSigner(pSigMessages)
		root = pSigMessages.Messages[0].SigningRoot
	}

	logger := c.logger.With(
		fields.MessageID(msg.MsgID),
		fields.MessageType(msg.MsgType),
		fields.RunnerRole(runnerRole),
		fields.Slot(slot),
		fields.OperatorID(signer),
		fields.Root(root),
	)

	ctx := partialSigVerifyCtx{
		logger:          logger,
		runnerRole:      runnerRole,
		slot:            slot,
		signer:          signer,
		root:            root,
		partialMsgsSize: len(pSigMessages.Messages),
	}

	if isCommitteeRunnerRole(runnerRole) {
		executorID := msg.MsgID.GetDutyExecutorID()
		if len(executorID) >= 32 {
			// committeeID is the last 16 bytes of the executorID
			copy(ctx.committeeID[:], executorID[16:])
			ctx.logger = logger.With(fields.CommitteeID(ctx.committeeID))
		}
	}

	return ctx
}

func (c *Collector) summarizeMissingValidatorIndices(pSigMessages *spectypes.PartialSignatureMessages) (first phase0.ValidatorIndex, count int) {
	for _, partialMsg := range pSigMessages.Messages {
		if _, ok := c.validators.ValidatorByIndex(partialMsg.ValidatorIndex); !ok {
			count++
			if count == 1 {
				first = partialMsg.ValidatorIndex
			}
		}
	}
	return first, count
}

func (c *Collector) wrapVerifyPartialSigErr(ctx partialSigVerifyCtx, pSigMessages *spectypes.PartialSignatureMessages, err error) error {
	missingFirst, missingCount := c.summarizeMissingValidatorIndices(pSigMessages)

	ctx.logger.Debug("❌ verify partial sig failed",
		zap.Error(err),
		zap.Int("partial_msgs_count", ctx.partialMsgsSize),
		zap.Uint64("missing_first_validator_index", uint64(missingFirst)),
		zap.Int("missing_validator_index_count", missingCount),
	)

	if isCommitteeRunnerRole(ctx.runnerRole) {
		return fmt.Errorf(
			"verify partial sig (slot=%d committee_id=%x signer=%d root=%x partial_msgs=%d missing_first_validator_index=%d missing_validator_index_count=%d): %w",
			ctx.slot,
			ctx.committeeID,
			ctx.signer,
			ctx.root,
			ctx.partialMsgsSize,
			missingFirst,
			missingCount,
			err,
		)
	}

	return fmt.Errorf(
		"verify partial sig (slot=%d runner_role=%d signer=%d root=%x partial_msgs=%d missing_first_validator_index=%d missing_validator_index_count=%d): %w",
		ctx.slot,
		ctx.runnerRole,
		ctx.signer,
		ctx.root,
		ctx.partialMsgsSize,
		missingFirst,
		missingCount,
		err,
	)
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
		if !ok || subMsg == nil {
			return nil
		}

		slot := phase0.Slot(subMsg.Height)
		msgID := spectypes.MessageID(subMsg.Identifier[:])
		executorID := msgID.GetDutyExecutorID()

		role := msgID.GetRoleType()
		if isCommitteeRunnerRole(role) {
			var committeeID spectypes.CommitteeID
			// committeeID is the last 16 bytes of the executorID
			copy(committeeID[:], executorID[16:])

			trace, late, err := c.getOrCreateCommitteeTrace(slot, committeeID, role)
			if err != nil {
				return err
			}

			trace.Lock()
			defer trace.Unlock()

			if len(msg.SignedSSVMessage.FullData) > 0 && subMsg.MsgType == specqbft.ProposalMsgType {
				// save proposal data and compute role roots
				trace.ProposalData = msg.SignedSSVMessage.FullData

				switch role {
				case spectypes.RoleCommittee:
					if syncRoot, attRoot, err := c.computeRoleRoots(ctx, slot, msg.SignedSSVMessage.FullData); err == nil {
						trace.syncCommitteeRoot = syncRoot
						trace.attestationRoot = attRoot
						trace.roleRootsReady = true
						trace.flushPending()
						// Check quorum for all validators after flushing pending signatures.
						// This ensures quorum detection happens immediately when signatures that
						// arrived before the proposal are reclassified into role buckets.
						c.checkQuorumAfterFlush(c.logger, committeeID, slot, trace)
					} else {
						// CRITICAL: If we fail to compute role roots, pending signatures will be dropped.
						pendingCount := 0
						for _, perSigner := range trace.pendingByRoot {
							for _, byTs := range perSigner {
								for _, idxs := range byTs {
									pendingCount += len(idxs)
								}
							}
						}
						c.logger.Error("CRITICAL: failed to compute role roots from proposal - pending signatures will be dropped",
							zap.Error(err),
							fields.Slot(slot),
							fields.CommitteeID(committeeID),
							zap.Int("pending_signature_count", pendingCount),
							pendingDetails(trace.pendingByRoot))
					}
				case spectypes.RoleAggregatorCommittee:
					if rolesByRoot, err := c.computeAggregatorCommitteePostConsensusRoles(ctx, slot, msg.SignedSSVMessage.FullData); err == nil {
						trace.aggPostConsensusRoles = rolesByRoot
						trace.flushPending()
						// Check quorum for all validators after flushing pending signatures.
						// This ensures quorum detection happens immediately when signatures that
						// arrived before the proposal are reclassified into role buckets.
						c.checkQuorumAfterFlush(c.logger, committeeID, slot, trace)
					} else {
						// CRITICAL: If we fail to compute role roots, pending signatures will be dropped.
						pendingCount := 0
						for _, perSigner := range trace.pendingByRoot {
							for _, byTs := range perSigner {
								for _, idxs := range byTs {
									pendingCount += len(idxs)
								}
							}
						}
						c.logger.Error("CRITICAL: failed to compute aggregator committee roots from proposal - pending signatures will be dropped",
							zap.Error(err),
							fields.Slot(slot),
							fields.CommitteeID(committeeID),
							zap.Int("pending_signature_count", pendingCount),
							pendingDetails(trace.pendingByRoot))
					}
				default:
					c.logger.Warn("unexpected committee role on committee trace path", zap.Int32("role", int32(role)))
				}
			}

			round := getOrCreateRound(&trace.ConsensusTrace, uint64(subMsg.Round))

			decided := c.processConsensus(startTime, subMsg, msg.SignedSSVMessage, round)
			if decided != nil {
				trace.Decideds = append(trace.Decideds, decided)
			}

			if late {
				err := c.store.SaveCommitteeDuty(role, &trace.CommitteeDutyTrace)
				_ = c.inFlightCommittee.Delete(committeeTraceKey{id: committeeID, role: role})
				return err
			}

			return nil
		}

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
				var data = new(spectypes.ProposerConsensusData)
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
			err := c.store.SaveValidatorDuty(roleDutyTrace)
			_ = c.inFlightValidator.Delete(index)
			return err
		}

		return nil
	}

	if msg.MsgType == spectypes.SSVPartialSignatureMsgType {
		pSigMessages := new(spectypes.PartialSignatureMessages)
		err := pSigMessages.Decode(msg.SignedSSVMessage.SSVMessage.GetData())
		if err != nil {
			return fmt.Errorf("decode partial signature messages: %w", err)
		}

		if len(pSigMessages.Messages) == 0 {
			return fmt.Errorf("no partial sig messages")
		}

		verifyCtx := c.newPartialSigVerifyCtx(msg, pSigMessages)

		if pSigMessages.Type == spectypes.PostConsensusPartialSig {
			if err := pSigMessages.Validate(); err != nil {
				return fmt.Errorf("validate partial sig: %w", err)
			}

			if err := verifySig(pSigMessages); err != nil {
				return c.wrapVerifyPartialSigErr(verifyCtx, pSigMessages, err)
			}
		}

		// process partial sig for committee
		if isCommitteeRunnerRole(verifyCtx.runnerRole) {
			var aggSelectionRoot phase0.Root
			aggSelectionRootReady := false
			if pSigMessages.Type == spectypes.AggregatorCommitteePartialSig {
				root, err := c.getAggregatorSelectionRoot(ctx, verifyCtx.slot)
				if err != nil {
					c.logger.Warn("failed to compute aggregator selection root",
						zap.Error(err),
						fields.Slot(verifyCtx.slot),
						fields.CommitteeID(verifyCtx.committeeID))
				} else {
					aggSelectionRoot = root
					aggSelectionRootReady = true
				}
			}

			trace, late, err := c.getOrCreateCommitteeTrace(verifyCtx.slot, verifyCtx.committeeID, verifyCtx.runnerRole)
			if err != nil {
				return err
			}

			trace.Lock()
			defer trace.Unlock()

			c.processPartialSigCommittee(startTime, pSigMessages, trace, aggSelectionRoot, aggSelectionRootReady)
			c.checkAndPublishQuorum(verifyCtx.logger, pSigMessages, verifyCtx.committeeID, trace)

			if late {
				err := c.store.SaveCommitteeDuty(verifyCtx.runnerRole, &trace.CommitteeDutyTrace)
				_ = c.inFlightCommittee.Delete(committeeTraceKey{id: verifyCtx.committeeID, role: verifyCtx.runnerRole})
				return err
			}

			return nil
		}

		// process partial sig for validator
		bnRole, err := toBNRole(verifyCtx.runnerRole)
		if err != nil {
			return err
		}

		trace, late, err := c.getOrCreateValidatorTrace(pSigMessages.Slot, bnRole, pSigMessages.Messages[0].ValidatorIndex)
		if err != nil {
			return err
		}

		trace.Lock()
		defer trace.Unlock()

		roleDutyTrace := trace.getOrCreate(pSigMessages.Slot, bnRole)

		if roleDutyTrace.Validator == 0 {
			roleDutyTrace.Validator = pSigMessages.Messages[0].ValidatorIndex
		}

		tr := &traces.PartialSigTrace{
			Type:         pSigMessages.Type,
			BeaconRoot:   pSigMessages.Messages[0].SigningRoot,
			Signer:       ssvtypes.PartialSigMsgSigner(pSigMessages),
			ReceivedTime: startTime,
		}

		if pSigMessages.Type == spectypes.PostConsensusPartialSig {
			roleDutyTrace.Post = append(roleDutyTrace.Post, tr)
		} else {
			roleDutyTrace.Pre = append(roleDutyTrace.Pre, tr)
		}

		if late {
			err := c.store.SaveValidatorDuty(roleDutyTrace)
			_ = c.inFlightValidator.Delete(pSigMessages.Messages[0].ValidatorIndex)
			return err
		}

		return nil
	}

	return nil
}

func toBNRole(r spectypes.RunnerRole) (bnRole spectypes.BeaconRole, err error) {
	switch r {
	case spectypes.RoleCommittee:
		return spectypes.BNRoleUnknown, errors.New("unexpected committee role")
	case spectypes.RoleAggregatorCommittee:
		return spectypes.BNRoleUnknown, errors.New("unexpected aggregator committee role")
	case spectypes.RoleProposer:
		bnRole = spectypes.BNRoleProposer
	case ssvtypes.RoleAggregator:
		bnRole = spectypes.BNRoleAggregator
	case ssvtypes.RoleSyncCommitteeContribution:
		bnRole = spectypes.BNRoleSyncCommitteeContribution
	case spectypes.RoleValidatorRegistration:
		bnRole = spectypes.BNRoleValidatorRegistration
	case spectypes.RoleVoluntaryExit:
		bnRole = spectypes.BNRoleVoluntaryExit
	default:
		return spectypes.BNRoleUnknown, fmt.Errorf("unexpected runner role %d", r)
	}

	return
}

// isCommitteeRunnerRole reports whether the runner role represents a committee-keyed
// duty trace (Committee or AggregatorCommittee), as opposed to a per-validator duty.
func isCommitteeRunnerRole(role spectypes.RunnerRole) bool {
	return role == spectypes.RoleCommittee || role == spectypes.RoleAggregatorCommittee
}

type validatorDutyTrace struct {
	sync.Mutex
	roles []*traces.ValidatorDutyTrace
}

func (dt *validatorDutyTrace) getOrCreate(slot phase0.Slot, role spectypes.BeaconRole) *traces.ValidatorDutyTrace {
	// find the trace for the role
	for _, t := range dt.roles {
		if t.Role == role {
			return t
		}
	}

	// or create a new one
	roleDutyTrace := &traces.ValidatorDutyTrace{
		Slot: slot,
		Role: role,
	}
	dt.roles = append(dt.roles, roleDutyTrace)

	return roleDutyTrace
}

func (dt *validatorDutyTrace) roleTraces() (roles []*traces.ValidatorDutyTrace) {
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
	// Aggregator committee root metadata
	aggSelectionRoot      phase0.Root
	aggSelectionRootReady bool
	aggPostConsensusRoles map[phase0.Root]spectypes.BeaconRole
	traces.CommitteeDutyTrace

	// Track published quorums to avoid duplicates (validator -> role -> signers hash)
	// Not part of the model.CommitteeDutyTrace, because it's not persisted to disk
	publishedQuorums map[phase0.ValidatorIndex]map[spectypes.BeaconRole]string

	// Pending signatures grouped by SigningRoot and signer until role roots are known.
	// Shape: pendingByRoot[root][signer][receivedAt] = []validatorIndices
	pendingByRoot map[phase0.Root]map[spectypes.OperatorID]map[uint64][]phase0.ValidatorIndex

	// Root-specific signer sets used for quorum checks that must not mix multiple roots
	// for the same role (e.g., sync committee contribution in aggregator-committee duties).
	signersByRoot map[phase0.Root]map[phase0.ValidatorIndex]map[spectypes.OperatorID]struct{}
}

// safeDeepCopy returns a deep copy of the trace data with internal locking.
// Use this when you don't already hold the lock. For manual locking, call DeepCopy() directly while holding the lock.
func (dt *committeeDutyTrace) safeDeepCopy() *traces.CommitteeDutyTrace {
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

// addRootSigner records that signer signed for a given (root, validatorIndex) pair,
// used for root-scoped quorum checks that must not mix signatures for distinct roots
// sharing the same beacon-role bucket (e.g. aggregator committee's two selection roots).
func (dt *committeeDutyTrace) addRootSigner(root phase0.Root, idx phase0.ValidatorIndex, signer spectypes.OperatorID) {
	if dt.signersByRoot == nil {
		dt.signersByRoot = make(map[phase0.Root]map[phase0.ValidatorIndex]map[spectypes.OperatorID]struct{})
	}
	perValidator, ok := dt.signersByRoot[root]
	if !ok {
		perValidator = make(map[phase0.ValidatorIndex]map[spectypes.OperatorID]struct{})
		dt.signersByRoot[root] = perValidator
	}
	signers, ok := perValidator[idx]
	if !ok {
		signers = make(map[spectypes.OperatorID]struct{})
		perValidator[idx] = signers
	}
	signers[signer] = struct{}{}
}

// flushPending routes buffered entries into Attester/SyncCommittee buckets
// according to derived role roots. Caller must hold dt.Lock().
func (dt *committeeDutyTrace) flushPending() {
	if len(dt.pendingByRoot) == 0 {
		return
	}
	for root, perSigner := range dt.pendingByRoot {
		role, ok := dt.classifyRootForPending(root)
		if !ok {
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
				sd := &traces.SignerData{Signer: signer, ValidatorIdx: idxs, ReceivedTime: ts}
				if role == spectypes.BNRoleSyncCommittee || role == spectypes.BNRoleSyncCommitteeContribution {
					dt.SyncCommittee = append(dt.SyncCommittee, sd)
				} else {
					dt.Attester = append(dt.Attester, sd)
				}
			}
		}
		delete(dt.pendingByRoot, root)
	}
}

// classifyRootForPending resolves the beacon role a given signing root belongs to,
// using either the RoleCommittee-derived roots (sync committee / attestation) or the
// AggregatorCommittee-derived post-consensus roles (aggregator / sync committee contribution).
func (dt *committeeDutyTrace) classifyRootForPending(root phase0.Root) (spectypes.BeaconRole, bool) {
	if dt.roleRootsReady {
		if root == dt.syncCommitteeRoot {
			return spectypes.BNRoleSyncCommittee, true
		}
		if root == dt.attestationRoot {
			return spectypes.BNRoleAttester, true
		}
	}

	if dt.aggPostConsensusRoles != nil {
		if role, ok := dt.aggPostConsensusRoles[root]; ok {
			return role, true
		}
	}

	return spectypes.BNRoleUnknown, false
}

func getOrCreateRound(trace *traces.ConsensusTrace, rnd uint64) *traces.RoundTrace {
	var count = len(trace.Rounds)
	for rnd > uint64(count) { //nolint:gosec
		trace.Rounds = append(trace.Rounds, &traces.RoundTrace{})
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

		// Determine role from the signing root and check quorum only for that role.
		// This prevents false positives where signatures for one role are counted toward another.
		if msg.Type == spectypes.AggregatorCommitteePartialSig && trace.aggSelectionRootReady {
			if bytes.Equal(trace.aggSelectionRoot[:], partialMsg.SigningRoot[:]) {
				c.checkAndPublishQuorumForRole(logger, trace, spectypes.BNRoleAggregator, msg, partialMsg, threshold)
			} else {
				c.checkAndPublishQuorumForRole(logger, trace, spectypes.BNRoleSyncCommitteeContribution, msg, partialMsg, threshold)
			}
			continue
		}
		if role, ok := trace.classifyRootForPending(partialMsg.SigningRoot); ok {
			c.checkAndPublishQuorumForRole(logger, trace, role, msg, partialMsg, threshold)
			continue
		}
		// If roots are not ready yet, signatures are in pending buffer.
		// Quorum will be checked after flushPending() is called when proposal arrives.
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
	var signerData []*traces.SignerData

	switch role {
	case spectypes.BNRoleAttester, spectypes.BNRoleAggregator:
		signerData = trace.Attester
	case spectypes.BNRoleSyncCommittee, spectypes.BNRoleSyncCommitteeContribution:
		signerData = trace.SyncCommittee
	default:
		return
	}

	signers := c.countUniqueSignersForValidatorAndRoot(trace, signerData, partialMsg.ValidatorIndex, partialMsg.SigningRoot)
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

// countUniqueSignersForValidator counts unique signers for a specific validator across the
// given role-bucket signer data, without root-scoping.
func (c *Collector) countUniqueSignersForValidator(signerData []*traces.SignerData, validatorIndex phase0.ValidatorIndex) []spectypes.OperatorID {
	signers := make(map[spectypes.OperatorID]struct{})

	for _, data := range signerData {
		if slices.Contains(data.ValidatorIdx, validatorIndex) {
			signers[data.Signer] = struct{}{}
		}
	}

	return sortedSigners(signers)
}

// countUniqueSignersForValidatorAndRoot counts signers for a specific validator and signing root.
// If root-scoped data is unavailable, it falls back to role-bucket signerData.
func (c *Collector) countUniqueSignersForValidatorAndRoot(
	trace *committeeDutyTrace,
	signerData []*traces.SignerData,
	validatorIndex phase0.ValidatorIndex,
	root phase0.Root,
) []spectypes.OperatorID {
	if root == (phase0.Root{}) || trace == nil {
		return c.countUniqueSignersForValidator(signerData, validatorIndex)
	}

	byValidator, found := trace.signersByRoot[root]
	if !found {
		return c.countUniqueSignersForValidator(signerData, validatorIndex)
	}
	if signers, found := byValidator[validatorIndex]; found {
		return sortedSigners(signers)
	}

	return nil
}

func sortedSigners(signers map[spectypes.OperatorID]struct{}) []spectypes.OperatorID {
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

// checkQuorumAfterFlush checks quorum for all validators after flushing pending signatures.
// This handles the case where signatures arrived before the proposal and were buffered.
// IMPORTANT: trace must be locked by the caller before calling this function.
func (c *Collector) checkQuorumAfterFlush(logger *zap.Logger, committeeID spectypes.CommitteeID, slot phase0.Slot, trace *committeeDutyTrace) {
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

	switch trace.Role {
	case spectypes.RoleAggregatorCommittee:
		c.checkRoleQuorumForValidatorsByRoot(logger, trace, spectypes.BNRoleAggregator, slot, threshold)
		c.checkRoleQuorumForValidatorsByRoot(logger, trace, spectypes.BNRoleSyncCommitteeContribution, slot, threshold)
	default:
		c.checkRoleQuorumForValidators(logger, trace, spectypes.BNRoleAttester, trace.Attester, slot, threshold)
		c.checkRoleQuorumForValidators(logger, trace, spectypes.BNRoleSyncCommittee, trace.SyncCommittee, slot, threshold)
	}
}

// checkRoleQuorumForValidators checks quorum for all validators in the given role's signer data.
// IMPORTANT: trace must be locked by the caller before calling this function.
func (c *Collector) checkRoleQuorumForValidators(
	logger *zap.Logger,
	trace *committeeDutyTrace,
	role spectypes.BeaconRole,
	signerData []*traces.SignerData,
	slot phase0.Slot,
	threshold uint64,
) {
	// Collect all unique validator indices for this role
	validators := make(map[phase0.ValidatorIndex]struct{})
	for _, sd := range signerData {
		for _, idx := range sd.ValidatorIdx {
			validators[idx] = struct{}{}
		}
	}

	// Check quorum for each validator
	for validatorIndex := range validators {
		_, exists := c.validators.ValidatorByIndex(validatorIndex)
		if !exists {
			logger.Debug("validator not found by index during quorum check after flush",
				zap.Uint64("validator_index", uint64(validatorIndex)),
				fields.BeaconRole(role),
				fields.Slot(slot))
			continue
		}
		if trace.publishedQuorums[validatorIndex] == nil {
			trace.publishedQuorums[validatorIndex] = make(map[spectypes.BeaconRole]string)
		}
		c.checkAndPublishQuorumForRoleByIndex(logger, trace, role, slot, validatorIndex, threshold)
	}
}

// checkRoleQuorumForValidatorsByRoot checks quorum for all validators in a specific role and root.
// IMPORTANT: trace must be locked by the caller before calling this function.
func (c *Collector) checkRoleQuorumForValidatorsByRoot(
	logger *zap.Logger,
	trace *committeeDutyTrace,
	role spectypes.BeaconRole,
	slot phase0.Slot,
	threshold uint64,
) {
	for root, byValidator := range trace.signersByRoot {
		classifiedRole, ok := trace.classifyRootForPending(root)
		if !ok || classifiedRole != role {
			continue
		}

		for validatorIndex := range byValidator {
			_, exists := c.validators.ValidatorByIndex(validatorIndex)
			if !exists {
				logger.Debug("validator not found by index during root-specific quorum check after flush",
					zap.Uint64("validator_index", uint64(validatorIndex)),
					fields.BeaconRole(role),
					fields.Slot(slot))
				continue
			}
			if trace.publishedQuorums[validatorIndex] == nil {
				trace.publishedQuorums[validatorIndex] = make(map[spectypes.BeaconRole]string)
			}
			c.checkAndPublishQuorumForRoleByIndexAndRoot(logger, trace, role, slot, validatorIndex, root, threshold)
		}
	}
}

// checkAndPublishQuorumForRoleByIndex checks quorum for a specific validator and role after flush.
// Similar to checkAndPublishQuorumForRole but works with validator index directly.
// IMPORTANT: trace must be locked by the caller before calling this function.
func (c *Collector) checkAndPublishQuorumForRoleByIndex(
	logger *zap.Logger,
	trace *committeeDutyTrace,
	role spectypes.BeaconRole,
	slot phase0.Slot,
	validatorIndex phase0.ValidatorIndex,
	threshold uint64,
) {
	c.checkAndPublishQuorumForRoleByIndexCommon(logger, trace, role, slot, validatorIndex, phase0.Root{}, threshold)
}

// checkAndPublishQuorumForRoleByIndexAndRoot checks quorum for a specific validator and role after
// flush, scoped to a specific signing root.
// IMPORTANT: trace must be locked by the caller before calling this function.
func (c *Collector) checkAndPublishQuorumForRoleByIndexAndRoot(
	logger *zap.Logger,
	trace *committeeDutyTrace,
	role spectypes.BeaconRole,
	slot phase0.Slot,
	validatorIndex phase0.ValidatorIndex,
	root phase0.Root,
	threshold uint64,
) {
	c.checkAndPublishQuorumForRoleByIndexCommon(logger, trace, role, slot, validatorIndex, root, threshold)
}

func (c *Collector) checkAndPublishQuorumForRoleByIndexCommon(
	logger *zap.Logger,
	trace *committeeDutyTrace,
	role spectypes.BeaconRole,
	slot phase0.Slot,
	validatorIndex phase0.ValidatorIndex,
	root phase0.Root,
	threshold uint64,
) {
	var signerData []*traces.SignerData

	switch role {
	case spectypes.BNRoleAttester, spectypes.BNRoleAggregator:
		signerData = trace.Attester
	case spectypes.BNRoleSyncCommittee, spectypes.BNRoleSyncCommitteeContribution:
		signerData = trace.SyncCommittee
	default:
		return
	}

	signers := c.countUniqueSignersForValidatorAndRoot(trace, signerData, validatorIndex, root)
	if uint64(len(signers)) < threshold {
		return
	}

	// Initialize the maps if needed
	if trace.publishedQuorums == nil {
		trace.publishedQuorums = make(map[phase0.ValidatorIndex]map[spectypes.BeaconRole]string)
	}
	if trace.publishedQuorums[validatorIndex] == nil {
		trace.publishedQuorums[validatorIndex] = make(map[spectypes.BeaconRole]string)
	}

	signersKey := c.signersToKey(signers)
	lastPublished := trace.publishedQuorums[validatorIndex][role]

	// Only publish the FIRST time quorum is reached
	if lastPublished == "" {
		trace.publishedQuorums[validatorIndex][role] = signersKey

		decidedInfo := DecidedInfo{
			Index:   validatorIndex,
			Slot:    slot,
			Role:    role,
			Signers: signers,
		}
		c.decidedListenerFunc(decidedInfo)

		logger.Debug("quorum reached after flush",
			zap.Uint64("validator_index", uint64(validatorIndex)),
			fields.BeaconRole(role),
			zap.Int("signers_count", len(signers)))
	}
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
			if d == nil {
				continue
			}
			schedule[d.ValidatorIndex] |= rolemask.BitAttester
		}
	}

	// Proposer indices for this slot
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

	if err := c.store.SaveScheduled(slot, schedule); err != nil {
		return fmt.Errorf("save scheduled: %w", err)
	}

	// Populate committee links only for validators with scheduled duties
	committees := c.validators.ParticipatingCommittees(epoch)
	committeeByValidator := buildValidatorToCommitteeIndex(committees)

	for validatorIndex := range schedule {
		if committeeID, found := committeeByValidator[validatorIndex]; found {
			slotToCommittee, _ := c.validatorIndexToCommitteeLinks.GetOrSet(validatorIndex, hashmap.New[phase0.Slot, spectypes.CommitteeID]())
			slotToCommittee.Set(slot, committeeID)
		}
	}

	return nil
}

// buildValidatorToCommitteeIndex creates a reverse lookup map from validator index to committee ID.
func buildValidatorToCommitteeIndex(committees []*registrystorage.Committee) map[phase0.ValidatorIndex]spectypes.CommitteeID {
	result := make(map[phase0.ValidatorIndex]spectypes.CommitteeID)
	for _, cmt := range committees {
		for _, validatorIndex := range cmt.Indices {
			result[validatorIndex] = cmt.ID
		}
	}
	return result
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
