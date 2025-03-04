package validator

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	model "github.com/ssvlabs/ssv/exporter/v2"
	"github.com/ssvlabs/ssv/exporter/v2/store"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/operator/slotticker"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

type InMemTracer struct {
	logger *zap.Logger

	// committeeID:slot:committeeDutyTrace
	committeeTraces *TypedSyncMap[spectypes.CommitteeID, *TypedSyncMap[phase0.Slot, *committeeDutyTrace]]

	// validatorPubKey:slot:validatorDutyTrace
	validatorTraces *TypedSyncMap[spectypes.ValidatorPK, *TypedSyncMap[phase0.Slot, *validatorDutyTrace]]

	// validatorIndex:slot:committeeID
	validatorIndexToCommitteeLinks *TypedSyncMap[phase0.ValidatorIndex, *TypedSyncMap[phase0.Slot, spectypes.CommitteeID]]

	syncCommitteeRoots *ttlcache.Cache[phase0.Slot, phase0.Root]

	beaconNetwork spectypes.BeaconNetwork

	verifyBLSSignatureFn func(*spectypes.PartialSignatureMessages, *spectypes.SignedSSVMessage) error

	store      *store.DutyTraceStore
	client     DomainDataProvider
	validators registrystorage.ValidatorStore
}

type DomainDataProvider interface {
	DomainData(epoch phase0.Epoch, domain phase0.DomainType) (phase0.Domain, error)
}

func NewTracer(ctx context.Context,
	logger *zap.Logger,
	validators registrystorage.ValidatorStore,
	client DomainDataProvider,
	store *store.DutyTraceStore,
	beaconNetwork spectypes.BeaconNetwork,
	disableBLSVerfication ...bool,
) *InMemTracer {

	tracer := &InMemTracer{
		logger:                         logger,
		store:                          store,
		client:                         client,
		beaconNetwork:                  beaconNetwork,
		validators:                     validators,
		committeeTraces:                NewTypedSyncMap[spectypes.CommitteeID, *TypedSyncMap[phase0.Slot, *committeeDutyTrace]](),
		validatorTraces:                NewTypedSyncMap[spectypes.ValidatorPK, *TypedSyncMap[phase0.Slot, *validatorDutyTrace]](),
		validatorIndexToCommitteeLinks: NewTypedSyncMap[phase0.ValidatorIndex, *TypedSyncMap[phase0.Slot, spectypes.CommitteeID]](),
		syncCommitteeRoots:             ttlcache.New(ttlcache.WithTTL[phase0.Slot, phase0.Root](ttlRoot)),
		verifyBLSSignatureFn:           func(*spectypes.PartialSignatureMessages, *spectypes.SignedSSVMessage) error { return nil },
	}

	if len(disableBLSVerfication) == 0 {
		tracer.verifyBLSSignatureFn = tracer.verifyBLSSignature
	}

	return tracer
}

func (n *InMemTracer) StartEvictionJob(ctx context.Context, tickerProvider slotticker.Provider) {
	n.logger.Info("start duty tracer cache to disk evictor")
	ticker := tickerProvider()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.Next():
			currentSlot := ticker.Slot()
			n.evictCommitteeTraces(currentSlot)
			n.evictValidatorTraces(currentSlot)
			n.evictValidatorCommitteeLinks(currentSlot)
			// remove old SC roots
			n.syncCommitteeRoots.DeleteExpired()
		}
	}
}

func (n *InMemTracer) getOrCreateValidatorTrace(slot phase0.Slot, role spectypes.BeaconRole, vPubKey spectypes.ValidatorPK) (*validatorDutyTrace, *model.ValidatorDutyTrace) {
	validatorSlots, found := n.validatorTraces.Load(vPubKey)
	if !found {
		validatorSlots, _ = n.validatorTraces.LoadOrStore(vPubKey, NewTypedSyncMap[phase0.Slot, *validatorDutyTrace]())
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

func (n *InMemTracer) getOrCreateCommitteeTrace(slot phase0.Slot, committeeID spectypes.CommitteeID) *committeeDutyTrace {
	committeeSlots, found := n.committeeTraces.Load(committeeID)
	if !found {
		committeeSlots, _ = n.committeeTraces.LoadOrStore(committeeID, NewTypedSyncMap[phase0.Slot, *committeeDutyTrace]())
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

		justificationTrace := model.QBFTTrace{
			Round:        uint64(qbftMsg.Round),
			BeaconRoot:   qbftMsg.Root,
			Signer:       signedMsg.OperatorIDs[0],
			ReceivedTime: time.Now(),
		}

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
	return &model.RoundChangeTrace{
		QBFTTrace: model.QBFTTrace{
			Round:        uint64(msg.Round),
			BeaconRoot:   msg.Root,
			Signer:       signedMsg.OperatorIDs[0],
			ReceivedTime: receivedTime,
		},
		PreparedRound:   uint64(msg.DataRound),
		PrepareMessages: n.decodeJustificationWithPrepares(msg.RoundChangeJustification),
	}
}

func (n *InMemTracer) createProposalTrace(msg *specqbft.Message, signedMsg *spectypes.SignedSSVMessage) *model.ProposalTrace {
	return &model.ProposalTrace{
		QBFTTrace: model.QBFTTrace{
			Round:        uint64(msg.Round),
			BeaconRoot:   msg.Root,
			Signer:       signedMsg.OperatorIDs[0],
			ReceivedTime: time.Now(), // correct
		},
		RoundChanges:    n.decodeJustificationWithRoundChanges(msg.RoundChangeJustification),
		PrepareMessages: n.decodeJustificationWithPrepares(msg.PrepareJustification),
	}
}

func (n *InMemTracer) processConsensus(msg *specqbft.Message, signedMsg *spectypes.SignedSSVMessage, round *model.RoundTrace) *model.DecidedTrace {
	switch msg.MsgType {
	case specqbft.ProposalMsgType:
		round.ProposalTrace = n.createProposalTrace(msg, signedMsg)

	case specqbft.PrepareMsgType:
		prepare := &model.QBFTTrace{
			Round:        uint64(msg.Round),
			BeaconRoot:   msg.Root,
			Signer:       signedMsg.OperatorIDs[0],
			ReceivedTime: time.Now(),
		}

		round.Prepares = append(round.Prepares, prepare)

	case specqbft.CommitMsgType:
		if len(signedMsg.OperatorIDs) > 1 {
			return &model.DecidedTrace{
				Round:        uint64(msg.Round),
				BeaconRoot:   msg.Root,
				Signers:      signedMsg.OperatorIDs,
				ReceivedTime: time.Now(),
			}
		}

		commit := &model.QBFTTrace{
			Round:      uint64(msg.Round),
			BeaconRoot: msg.Root,
			//  TODO(Moshe/Matheus) - need to get the signer from the message?
			// Signer:       signedMsg.OperatorIDs[0],
			ReceivedTime: time.Now(),
		}

		round.Commits = append(round.Commits, commit)

	case specqbft.RoundChangeMsgType:
		now := time.Now()
		roundChangeTrace := n.createRoundChangeTrace(msg, signedMsg, now)

		round.RoundChanges = append(round.RoundChanges, roundChangeTrace)
	}

	return nil
}

func (n *InMemTracer) processPartialSigValidator(msg *spectypes.PartialSignatureMessages, ssvMsg *queue.SSVMessage, pubkey spectypes.ValidatorPK) {
	var (
		role = toBNRole(ssvMsg.MsgID.GetRoleType())
		slot = msg.Slot
	)

	trace, roleDutyTrace := n.getOrCreateValidatorTrace(slot, role, pubkey)

	trace.Lock()
	defer trace.Unlock()

	if roleDutyTrace.Validator == 0 {
		roleDutyTrace.Validator = msg.Messages[0].ValidatorIndex
	}

	tr := &model.PartialSigTrace{
		Type:         msg.Type,
		BeaconRoot:   msg.Messages[0].SigningRoot,
		Signer:       msg.Messages[0].Signer,
		ReceivedTime: time.Now(),
	}

	if msg.Type == spectypes.PostConsensusPartialSig {
		roleDutyTrace.Post = append(roleDutyTrace.Post, tr)
		return
	}

	roleDutyTrace.Pre = append(roleDutyTrace.Pre, tr)
}

func (n *InMemTracer) processPartialSigCommittee(msg *spectypes.PartialSignatureMessages, committeeID spectypes.CommitteeID) {
	slot := msg.Slot

	trace := n.getOrCreateCommitteeTrace(slot, committeeID)
	trace.Lock()
	defer trace.Unlock()

	{ // TODO(moshe) confirm is needed
		cmt, found := n.validators.Committee(committeeID)
		if found && len(cmt.Operators) > 0 {
			trace.OperatorIDs = cmt.Operators
		}
	}

	// store the link between validator index and committee id
	n.saveValidatorToCommitteeLink(slot, msg, committeeID)

	for _, partialSigMsg := range msg.Messages {
		signerData := model.SignerData{
			Signers:      []spectypes.OperatorID{partialSigMsg.Signer},
			ReceivedTime: time.Now(),
		}

		if bytes.Equal(trace.syncCommitteeRoot[:], partialSigMsg.SigningRoot[:]) {
			trace.SyncCommittee = append(trace.SyncCommittee, &signerData)
			continue
		}

		// TODO:me (electra) - implement a check for: attestation is correct instead of assuming
		trace.Attester = append(trace.Attester, &signerData)
	}
}

func (n *InMemTracer) saveValidatorToCommitteeLink(slot phase0.Slot, msg *spectypes.PartialSignatureMessages, committeeID spectypes.CommitteeID) {
	for _, msg := range msg.Messages {
		slotToCommittee, found := n.validatorIndexToCommitteeLinks.Load(msg.ValidatorIndex)
		if !found {
			slotToCommittee, _ = n.validatorIndexToCommitteeLinks.LoadOrStore(msg.ValidatorIndex, NewTypedSyncMap[phase0.Slot, spectypes.CommitteeID]())
		}

		slotToCommittee.Store(slot, committeeID)

		// TODO(me): remove this
		// n.logger.Info("store link for", fields.Slot(slot), zap.Uint64("index", uint64(msg.ValidatorIndex)), fields.CommitteeID(committeeID))
	}
}

func (n *InMemTracer) getSyncCommitteeRoot(slot phase0.Slot, in []byte) (phase0.Root, error) {
	// lookup in cache first
	cacheItem := n.syncCommitteeRoots.Get(slot)
	if cacheItem != nil {
		return cacheItem.Value(), nil
	}

	// call remote and cache the result
	var beaconVote = new(spectypes.BeaconVote)
	if err := beaconVote.Decode(in); err != nil {
		return phase0.Root{}, fmt.Errorf("decode beacon vote: %w", err)
	}

	epoch := n.beaconNetwork.EstimatedEpochAtSlot(slot)

	domain, err := n.client.DomainData(epoch, spectypes.DomainSyncCommittee)
	if err != nil {
		return phase0.Root{}, fmt.Errorf("get sync committee domain: %w", err)
	}

	blockRoot := spectypes.SSZBytes(beaconVote.BlockRoot[:])

	root, err := spectypes.ComputeETHSigningRoot(blockRoot, domain)
	if err != nil {
		return phase0.Root{}, fmt.Errorf("compute sync committee root: %w", err)
	}

	// cache the result with the same ttl as the committee
	_ = n.syncCommitteeRoots.Set(slot, root, ttlCommittee)

	return root, nil
}

func (n *InMemTracer) populateProposer(round *model.RoundTrace, committeeID spectypes.CommitteeID, subMsg *specqbft.Message) {
	committee, found := n.validators.Committee(committeeID)
	if !found {
		n.logger.Error("could not find committee by id", fields.CommitteeID(committeeID))
		return
	}

	operatorIDs := committee.Operators

	if len(operatorIDs) > 0 {
		mockState := n.toMockState(subMsg, operatorIDs)
		round.Proposer = specqbft.RoundRobinProposer(mockState, subMsg.Round)
	}
}

func (n *InMemTracer) toMockState(msg *specqbft.Message, operatorIDs []spectypes.OperatorID) *specqbft.State {
	// assemble operator IDs into a committee
	committee := make([]*spectypes.Operator, 0, len(operatorIDs))
	for _, operatorID := range operatorIDs {
		committee = append(committee, &spectypes.Operator{OperatorID: operatorID})
	}

	return &specqbft.State{
		Height: msg.Height,
		CommitteeMember: &spectypes.CommitteeMember{
			Committee: committee,
		},
	}
}

func (n *InMemTracer) verifyBLSSignature(pSigMessages *spectypes.PartialSignatureMessages, msg *spectypes.SignedSSVMessage) error {
	for _, msg := range pSigMessages.Messages {
		sig, err := decodeSig(msg.PartialSignature)
		if err != nil {
			return fmt.Errorf("decode partial signature: %w", err)
		}

		share, found := n.validators.ValidatorByIndex(msg.ValidatorIndex)
		if !found {
			return fmt.Errorf("get share by index: %w", err)
		}

		var sharePubkey spectypes.ShareValidatorPK

		found = false
		for _, cmt := range share.Committee {
			if cmt.Signer == msg.Signer {
				sharePubkey = cmt.SharePubKey
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf("share not found in committee by signer")
		}

		err = types.VerifyReconstructedSignature(sig, sharePubkey, msg.SigningRoot)
		if err != nil {
			return fmt.Errorf("verify reconstructed signature: %w", err)
		}
	}

	return nil
}

func (n *InMemTracer) Trace(msg *queue.SSVMessage) {
	switch msg.MsgType {
	case spectypes.SSVConsensusMsgType:
		if subMsg, ok := msg.Body.(*specqbft.Message); ok {
			slot := phase0.Slot(subMsg.Height)
			msgID := spectypes.MessageID(subMsg.Identifier[:]) // validator + pubkey + role + network
			executorID := msgID.GetDutyExecutorID()            // validator pubkey or committee id

			switch role := msgID.GetRoleType(); role {
			case spectypes.RoleCommittee:
				var committeeID spectypes.CommitteeID
				copy(committeeID[:], executorID[16:])

				trace := n.getOrCreateCommitteeTrace(slot, committeeID) // committe id

				trace.Lock()
				defer trace.Unlock()

				// fill in sync committee roots
				// to be later read in 'processPartialSigCommittee'
				// root, err := n.getSyncCommitteeRoot(slot, msg.SignedSSVMessage.FullData)
				// if err != nil {
				// 	n.logger.Error("get sync committee root", zap.Error(err))
				// }
				// trace.syncCommitteeRoot = root

				round := getOrCreateRound(&trace.ConsensusTrace, uint64(subMsg.Round))

				// TODO(moshe) populate proposer or not?
				n.populateProposer(round, committeeID, subMsg)

				decided := n.processConsensus(subMsg, msg.SignedSSVMessage, round)
				if decided != nil {
					trace.Decideds = append(trace.Decideds, decided)
				}
			default:
				var validatorPK spectypes.ValidatorPK
				copy(validatorPK[:], executorID)

				bnRole := toBNRole(role)

				trace, roleDutyTrace := n.getOrCreateValidatorTrace(slot, bnRole, validatorPK)

				if msg.MsgType == spectypes.SSVConsensusMsgType {
					var qbftMsg = new(specqbft.Message)
					err := qbftMsg.Decode(msg.Data)
					if err != nil {
						n.logger.Error("decode validator consensus data", zap.Error(err))
					} else {
						if qbftMsg.MsgType == specqbft.ProposalMsgType && roleDutyTrace.Validator == 0 {
							var data = new(spectypes.ValidatorConsensusData)
							err := data.Decode(msg.SignedSSVMessage.FullData)
							if err != nil {
								n.logger.Error("decode validator proposal data", zap.Error(err))
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

				// TODO(moshe) populate proposer or not?
				// n.populateProposer(round, validatorPK, subMsg)

				decided := n.processConsensus(subMsg, msg.SignedSSVMessage, round)
				if decided != nil {
					roleDutyTrace.Decideds = append(roleDutyTrace.Decideds, decided)
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

		// BLS signature verification
		// called via a member function to allow disabling during
		// benchmarking for more accurate readings
		if err = n.verifyBLSSignatureFn(pSigMessages, msg.SignedSSVMessage); err != nil {
			n.logger.Error("verify bls signature", zap.Error(err))
			return
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
