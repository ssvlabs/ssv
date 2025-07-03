package validator

import (
	"bytes"
	"context"
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

	model "github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/exporter/store"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/slotticker"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

type Collector struct {
	logger *zap.Logger

	// committeeID:slot:committeeDutyTrace
	committeeTraces *hashmap.Map[spectypes.CommitteeID, *hashmap.Map[phase0.Slot, *committeeDutyTrace]]

	// validatorPubKey:slot:validatorDutyTrace
	validatorTraces *hashmap.Map[spectypes.ValidatorPK, *hashmap.Map[phase0.Slot, *validatorDutyTrace]]

	// validatorIndex:slot:committeeID
	validatorIndexToCommitteeLinks *hashmap.Map[phase0.ValidatorIndex, *hashmap.Map[phase0.Slot, spectypes.CommitteeID]]

	syncCommitteeRootsCache *ttlcache.Cache[scRootKey, phase0.Root]
	syncCommitteeRootsSf    singleflight.Group

	beacon *networkconfig.BeaconConfig

	store      DutyTraceStore
	client     DomainDataProvider
	validators registrystorage.ValidatorStore

	lastEvictedSlot atomic.Uint64

	inFlightCommittee hashmap.Map[spectypes.CommitteeID, struct{}]
	inFlightValidator hashmap.Map[spectypes.ValidatorPK, struct{}]
}

type DomainDataProvider interface {
	DomainData(context.Context, phase0.Epoch, phase0.DomainType) (phase0.Domain, error)
}

func New(
	logger *zap.Logger,
	validators registrystorage.ValidatorStore,
	client DomainDataProvider,
	store DutyTraceStore,
	beaconNetwork *networkconfig.BeaconConfig,
) *Collector {

	ttl := time.Duration(slotTTL) * beaconNetwork.SlotDuration

	collector := &Collector{
		logger:                         logger,
		store:                          store,
		client:                         client,
		beacon:                         beaconNetwork,
		validators:                     validators,
		committeeTraces:                hashmap.New[spectypes.CommitteeID, *hashmap.Map[phase0.Slot, *committeeDutyTrace]](),
		validatorTraces:                hashmap.New[spectypes.ValidatorPK, *hashmap.Map[phase0.Slot, *validatorDutyTrace]](),
		validatorIndexToCommitteeLinks: hashmap.New[phase0.ValidatorIndex, *hashmap.Map[phase0.Slot, spectypes.CommitteeID]](),
		syncCommitteeRootsCache:        ttlcache.New(ttlcache.WithTTL[scRootKey, phase0.Root](ttl)),
		inFlightCommittee:              hashmap.Map[spectypes.CommitteeID, struct{}]{},
		inFlightValidator:              hashmap.Map[spectypes.ValidatorPK, struct{}]{},
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

func (c *Collector) getOrCreateValidatorTrace(slot phase0.Slot, role spectypes.BeaconRole, vPubKey spectypes.ValidatorPK) (*validatorDutyTrace, bool, error) {
	// check late arrival
	if uint64(slot) <= c.lastEvictedSlot.Load() {
		if _, found := c.inFlightValidator.GetOrSet(vPubKey, struct{}{}); found {
			return nil, false, errInFlight
		}

		trace, err := c.getValidatorDutiesFromDisk(role, slot, vPubKey)
		if errors.Is(err, store.ErrNotFound) {
			roleDutyTrace := &model.ValidatorDutyTrace{
				Slot: slot,
				Role: role,
			}
			wrappedTrace := &validatorDutyTrace{
				roles: []*model.ValidatorDutyTrace{roleDutyTrace},
			}
			return wrappedTrace, true, nil
		}
		if err != nil {
			_ = c.inFlightValidator.Delete(vPubKey)
			return nil, false, err
		}

		role := &trace.ValidatorDutyTrace

		wrappedTrace := &validatorDutyTrace{
			roles: []*model.ValidatorDutyTrace{role},
		}

		return wrappedTrace, true, nil
	}

	validatorSlots, found := c.validatorTraces.Get(vPubKey)
	if !found {
		validatorSlots, _ = c.validatorTraces.GetOrSet(vPubKey, hashmap.New[phase0.Slot, *validatorDutyTrace]())
	}

	traces, found := validatorSlots.Get(slot)

	if !found {
		roleDutyTrace := &model.ValidatorDutyTrace{
			Slot: slot,
			Role: role,
		}
		newTrace := &validatorDutyTrace{
			roles: []*model.ValidatorDutyTrace{roleDutyTrace},
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
				CommitteeDutyTrace: model.CommitteeDutyTrace{
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
			CommitteeDutyTrace: model.CommitteeDutyTrace{
				CommitteeID: committeeID,
				Slot:        slot,
			},
		}

		committeeTrace, _ = committeeSlots.GetOrSet(slot, trace)
	}

	return committeeTrace, false, nil
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

func (c *Collector) processConsensus(receivedAt uint64, msg *specqbft.Message, signedMsg *spectypes.SignedSSVMessage, round *model.RoundTrace) *model.DecidedTrace {
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
		if len(signedMsg.OperatorIDs) > 1 {
			return &model.DecidedTrace{
				Round:        uint64(msg.Round),
				BeaconRoot:   msg.Root,
				Signers:      signedMsg.OperatorIDs,
				ReceivedTime: receivedAt,
			}
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

	return nil // we're exhausting all cases in the switch
}

func (c *Collector) processPartialSigCommittee(receivedAt uint64, msg *spectypes.PartialSignatureMessages, committeeID spectypes.CommitteeID, trace *committeeDutyTrace) {
	// add operator ids to the trace
	cmt, found := c.validators.Committee(committeeID)
	if found && len(cmt.Operators) > 0 {
		trace.OperatorIDs = cmt.Operators
	}

	// collect signers
	indices := make([]phase0.ValidatorIndex, 0, len(msg.Messages))
	scIndices := make([]phase0.ValidatorIndex, 0, len(msg.Messages))

	for _, partialSigMsg := range msg.Messages {
		if bytes.Equal(trace.syncCommitteeRoot[:], partialSigMsg.SigningRoot[:]) {
			scIndices = append(scIndices, partialSigMsg.ValidatorIndex)
			continue
		}

		indices = append(indices, partialSigMsg.ValidatorIndex)
	}

	slices.Sort(indices)
	indices = slices.Compact(indices)

	slices.Sort(scIndices)
	scIndices = slices.Compact(scIndices)

	if len(scIndices) > 0 {
		trace.SyncCommittee = append(trace.SyncCommittee, &model.SignerData{
			Signer:       msg.Messages[0].Signer,
			ValidatorIdx: scIndices,
			ReceivedTime: receivedAt,
		})
	}

	if len(indices) > 0 {
		trace.Attester = append(trace.Attester, &model.SignerData{
			Signer:       msg.Messages[0].Signer,
			ValidatorIdx: indices,
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
		time.Sleep(time.Second)
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
			// committeeID is the last 16 bytes of the executorID
			copy(committeeID[:], executorID[16:])

			trace, late, err := c.getOrCreateCommitteeTrace(slot, committeeID)
			if err != nil {
				return err
			}

			trace.Lock()
			defer trace.Unlock()

			if len(msg.SignedSSVMessage.FullData) > 0 && subMsg.MsgType == specqbft.ProposalMsgType {
				// save proposal data
				trace.ProposalData = msg.SignedSSVMessage.FullData

				root, err := c.getSyncCommitteeRoot(ctx, slot, msg.SignedSSVMessage.FullData)
				if err == nil {
					trace.syncCommitteeRoot = root
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

			trace, late, err := c.getOrCreateValidatorTrace(slot, bnRole, validatorPK)
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
				_ = c.inFlightValidator.Delete(validatorPK)
				return c.store.SaveValidatorDuty(roleDutyTrace)
			}

			return nil
		}
	case spectypes.SSVPartialSignatureMsgType:
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
		var validatorPK spectypes.ValidatorPK
		copy(validatorPK[:], executorID)

		role, err := toBNRole(msg.MsgID.GetRoleType())
		if err != nil {
			return err
		}

		trace, late, err := c.getOrCreateValidatorTrace(pSigMessages.Slot, role, validatorPK)
		if err != nil {
			return err
		}

		trace.Lock()
		defer trace.Unlock()

		roleDutyTrace := trace.getOrCreate(pSigMessages.Slot, role)

		if roleDutyTrace.Validator == 0 {
			roleDutyTrace.Validator = pSigMessages.Messages[0].ValidatorIndex
		}

		tr := &model.PartialSigTrace{
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
			_ = c.inFlightValidator.Delete(validatorPK)
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
	roles []*model.ValidatorDutyTrace
}

func (dt *validatorDutyTrace) getOrCreate(slot phase0.Slot, role spectypes.BeaconRole) *model.ValidatorDutyTrace {
	// find the trace for the role
	for _, t := range dt.roles {
		if t.Role == role {
			return t
		}
	}

	// or create a new one
	roleDutyTrace := &model.ValidatorDutyTrace{
		Slot: slot,
		Role: role,
	}
	dt.roles = append(dt.roles, roleDutyTrace)

	return roleDutyTrace
}

func (dt *validatorDutyTrace) roleTraces() (roles []*model.ValidatorDutyTrace) {
	dt.Lock()
	defer dt.Unlock()

	for _, role := range dt.roles {
		roles = append(roles, deepCopyValidatorDutyTrace(role))
	}

	return
}

type committeeDutyTrace struct {
	sync.Mutex
	syncCommitteeRoot phase0.Root
	model.CommitteeDutyTrace
}

func (dt *committeeDutyTrace) trace() *model.CommitteeDutyTrace {
	dt.Lock()
	defer dt.Unlock()
	return deepCopyCommitteeDutyTrace(&dt.CommitteeDutyTrace)
}

func getOrCreateRound(trace *model.ConsensusTrace, rnd uint64) *model.RoundTrace {
	var count = len(trace.Rounds)
	for rnd > uint64(count) { //nolint:gosec
		trace.Rounds = append(trace.Rounds, &model.RoundTrace{})
		count = len(trace.Rounds)
	}

	return trace.Rounds[rnd-1]
}
