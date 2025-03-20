package validator

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
	"go.uber.org/zap"

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

	beacon spectypes.BeaconNetwork

	store      DutyTraceStore
	client     DomainDataProvider
	validators registrystorage.ValidatorStore
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

func (c *Collector) StartEvictionJob(ctx context.Context, tickerProvider slotticker.Provider) {
	c.logger.Info("start duty tracer cache to disk evictor")
	ticker := tickerProvider()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.Next():
			currentSlot := ticker.Slot()
			c.evictCommitteeTraces(currentSlot)
			c.evictValidatorTraces(currentSlot)
			c.evictValidatorCommitteeLinks(currentSlot)
			// remove old SC roots
			c.syncCommitteeRootsCache.DeleteExpired()
		}
	}
}

/*
all validatorDutyTrace objects contain a collection of model.ValidatorDutyTrace(s) - one per role.
so when we request a certain trace we return both of them:
- validatorDutyTrace object contains the lock
- the model.* ValidatorDutyTrace the data that we enrich subsequently
*/
func (c *Collector) getOrCreateValidatorTrace(slot phase0.Slot, role spectypes.BeaconRole, vPubKey spectypes.ValidatorPK) (*validatorDutyTrace, *model.ValidatorDutyTrace) {
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
		return traces, roleDutyTrace
	}

	traces.Lock()
	defer traces.Unlock()

	// find the trace for the role
	for _, t := range traces.Roles {
		if t.Role == role {
			return traces, t
		}
	}

	// or create a new one
	roleDutyTrace := &model.ValidatorDutyTrace{
		Slot: slot,
		Role: role,
	}
	traces.Roles = append(traces.Roles, roleDutyTrace)

	return traces, roleDutyTrace
}

func toBNRole(r spectypes.RunnerRole) (bnRole spectypes.BeaconRole) {
	switch r {
	case spectypes.RoleCommittee:
		panic("unexpected committee role") // TODO(me): replace by error?
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

func (c *Collector) getOrCreateCommitteeTrace(slot phase0.Slot, committeeID spectypes.CommitteeID) *committeeDutyTrace {
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

	return committeeTrace
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

		if len(signedMsg.OperatorIDs) == 0 {
			c.logger.Error("fatal error: no operator ids", fields.Slot(phase0.Slot(msg.Height)))
			return nil
		}

		signer := signedMsg.OperatorIDs[0]

		commit := &model.QBFTTrace{
			Round:        uint64(msg.Round),
			BeaconRoot:   msg.Root,
			Signer:       signer,
			ReceivedTime: receivedAt,
		}

		round.Commits = append(round.Commits, commit)

	case specqbft.RoundChangeMsgType:
		roundChangeTrace := c.createRoundChangeTrace(receivedAt, msg, signedMsg)

		round.RoundChanges = append(round.RoundChanges, roundChangeTrace)
	}

	return nil
}

func (c *Collector) processPartialSigValidator(receivedAt uint64, msg *spectypes.PartialSignatureMessages, ssvMsg *queue.SSVMessage, pubkey spectypes.ValidatorPK) {
	var (
		role = toBNRole(ssvMsg.MsgID.GetRoleType())
		slot = msg.Slot
	)

	trace, roleDutyTrace := c.getOrCreateValidatorTrace(slot, role, pubkey)

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
		return
	}

	roleDutyTrace.Pre = append(roleDutyTrace.Pre, tr)
}

func (c *Collector) processPartialSigCommittee(receivedAt uint64, msg *spectypes.PartialSignatureMessages, committeeID spectypes.CommitteeID) {
	slot := msg.Slot

	trace := c.getOrCreateCommitteeTrace(slot, committeeID)
	trace.Lock()
	defer trace.Unlock()

	{ // TODO(moshe) confirm is needed
		cmt, found := c.validators.Committee(committeeID)
		if found && len(cmt.Operators) > 0 {
			trace.OperatorIDs = cmt.Operators
		}
	}

	// store the link between validator index and committee id
	c.saveValidatorToCommitteeLink(slot, msg, committeeID)

	if len(msg.Messages) == 0 {
		c.logger.Warn("no partial sig messages", fields.Slot(slot), fields.CommitteeID(committeeID))
		return
	}

	var isSyncCommittee bool

	if bytes.Equal(trace.syncCommitteeRoot[:], msg.Messages[0].SigningRoot[:]) {
		isSyncCommittee = true
	}

	signers := make([]spectypes.OperatorID, 0, len(msg.Messages))
	for _, partialSigMsg := range msg.Messages {
		signers = append(signers, partialSigMsg.Signer)
	}

	slices.Sort(signers)
	signers = slices.Compact(signers)

	if isSyncCommittee {
		trace.SyncCommittee = append(trace.SyncCommittee, &model.SignerData{
			Signers:      signers,
			ReceivedTime: receivedAt,
		})

		c.logger.Info("got sync committee signers", fields.Slot(slot), fields.CommitteeID(committeeID), zap.String("signers", fmt.Sprintf("%v", signers)))

		return
	}

	trace.Attester = append(trace.Attester, &model.SignerData{
		Signers:      signers,
		ReceivedTime: receivedAt,
	})

	c.logger.Info("got attester signers", fields.Slot(slot), fields.CommitteeID(committeeID), zap.String("signers", fmt.Sprintf("%v", signers)))
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
}

func (c *Collector) Collect(ctx context.Context, msg *queue.SSVMessage, verifySig func(*spectypes.PartialSignatureMessages) error) {
	start := time.Now()

	tracerInFlightMessageCounter.Add(ctx, 1)
	defer func() {
		tracerInFlightMessageHist.Record(ctx, time.Since(start).Seconds())
	}()

	//nolint:gosec
	startTime := uint64(start.UnixNano() / int64(time.Millisecond))

	switch msg.MsgType {
	case spectypes.SSVConsensusMsgType:
		if subMsg, ok := msg.Body.(*specqbft.Message); ok {
			slot := phase0.Slot(subMsg.Height)
			msgID := spectypes.MessageID(subMsg.Identifier[:])
			executorID := msgID.GetDutyExecutorID()

			switch role := msgID.GetRoleType(); role {
			case spectypes.RoleCommittee:
				var committeeID spectypes.CommitteeID
				copy(committeeID[:], executorID[16:])

				trace := c.getOrCreateCommitteeTrace(slot, committeeID)

				trace.Lock()
				defer trace.Unlock()

				if len(msg.SignedSSVMessage.FullData) > 0 {
					// for future: check if it's a proposal message
					// if not, skip this step
					root, err := c.getSyncCommitteeRoot(slot, msg.SignedSSVMessage.FullData)
					if err != nil {
						c.logger.Error("get sync committee root", zap.Error(err))
					} else {
						trace.syncCommitteeRoot = root
					}
				}

				round := getOrCreateRound(&trace.ConsensusTrace, uint64(subMsg.Round))

				decided := c.processConsensus(startTime, subMsg, msg.SignedSSVMessage, round)
				if decided != nil {
					trace.Decideds = append(trace.Decideds, decided)
				}
			default:
				var validatorPK spectypes.ValidatorPK
				copy(validatorPK[:], executorID)

				bnRole := toBNRole(role)

				trace, roleDutyTrace := c.getOrCreateValidatorTrace(slot, bnRole, validatorPK)

				if msg.MsgType == spectypes.SSVConsensusMsgType {
					var qbftMsg = new(specqbft.Message)
					err := qbftMsg.Decode(msg.Data)
					if err != nil {
						c.logger.Error("decode validator consensus data", zap.Error(err))
					} else {
						if qbftMsg.MsgType == specqbft.ProposalMsgType && roleDutyTrace.Validator == 0 {
							var data = new(spectypes.ValidatorConsensusData)
							err := data.Decode(msg.SignedSSVMessage.FullData)
							if err != nil {
								c.logger.Error("decode validator proposal data", zap.Error(err))
							} else {
								func() {
									trace.Lock()
									defer trace.Unlock()

									roleDutyTrace.Validator = data.Duty.ValidatorIndex
								}()
							}
						}
					}
				}

				trace.Lock()
				defer trace.Unlock()

				round := getOrCreateRound(&roleDutyTrace.ConsensusTrace, uint64(subMsg.Round))

				decided := c.processConsensus(startTime, subMsg, msg.SignedSSVMessage, round)
				if decided != nil {
					roleDutyTrace.Decideds = append(roleDutyTrace.Decideds, decided)
				}
			}
		}
	case spectypes.SSVPartialSignatureMsgType:
		pSigMessages := new(spectypes.PartialSignatureMessages)
		err := pSigMessages.Decode(msg.SignedSSVMessage.SSVMessage.GetData())
		if err != nil {
			c.logger.Error("decode partial signature messages", zap.Error(err))
			return
		}

		if pSigMessages.Type == spectypes.PostConsensusPartialSig {
			if err := pSigMessages.Validate(); err != nil {
				c.logger.Error("validate partial sig", zap.Error(err))
				return
			}

			if err := verifySig(pSigMessages); err != nil {
				c.logger.Error("verify partial sig", zap.Error(err))
				return
			}
		}

		executorID := msg.MsgID.GetDutyExecutorID()

		if msg.MsgID.GetRoleType() == spectypes.RoleCommittee {
			var committeeID spectypes.CommitteeID
			copy(committeeID[:], executorID[16:])

			c.processPartialSigCommittee(startTime, pSigMessages, committeeID)

			return
		}

		var validatorPK spectypes.ValidatorPK
		copy(validatorPK[:], executorID)

		c.processPartialSigValidator(startTime, pSigMessages, msg, validatorPK)
	}
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
