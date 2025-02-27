package validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
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

type InMemTracer struct {
	logger *zap.Logger

	// committeeID:slot:committeeDutyTrace
	committeeTraces *TypedSyncMap[spectypes.CommitteeID, *TypedSyncMap[phase0.Slot, *committeeDutyTrace]]

	// validatorPubKey:slot:validatorDutyTrace
	validatorTraces *TypedSyncMap[spectypes.ValidatorPK, *TypedSyncMap[phase0.Slot, *validatorDutyTrace]]

	// validatorIndex:committeeID
	valToComMapping *TypedSyncMap[phase0.ValidatorIndex, *TypedSyncMap[phase0.Slot, spectypes.CommitteeID]]

	beaconNetwork spectypes.BeaconNetwork

	store      *store.DutyTraceStore
	client     *gc.GoClient
	validators registrystorage.ValidatorStore
}

func NewTracer(ctx context.Context,
	logger *zap.Logger,
	validators registrystorage.ValidatorStore,
	client *gc.GoClient,
	store *store.DutyTraceStore,
	beaconNetwork spectypes.BeaconNetwork) *InMemTracer {

	return &InMemTracer{
		logger:          logger,
		store:           store,
		client:          client,
		beaconNetwork:   beaconNetwork,
		committeeTraces: NewTypedSyncMap[spectypes.CommitteeID, *TypedSyncMap[phase0.Slot, *committeeDutyTrace]](),
		validatorTraces: NewTypedSyncMap[spectypes.ValidatorPK, *TypedSyncMap[phase0.Slot, *validatorDutyTrace]](),
		valToComMapping: NewTypedSyncMap[phase0.ValidatorIndex, *TypedSyncMap[phase0.Slot, spectypes.CommitteeID]](),
		validators:      validators,
	}
}

// TODO (Matus) - why is ticker not ticking
func (n *InMemTracer) StartEvictionJob(ctx context.Context, tickerProvider slotticker.Provider) {
	n.logger.Info("start duty tracer cache to disk evictor")
	ticker := tickerProvider()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.Next():
			currentSlot := ticker.Slot() + 3707879
			n.evictCommitteeTraces(currentSlot)
			n.evictValidatorTraces(currentSlot)
			// TODO: do we evict validator committee mapping?
			// Do we store them against a slot?
			n.evictValidatorCommitteeMapping(currentSlot)
		}
	}
}

func (n *InMemTracer) Store() DutyTraceStore {
	return &adapter{
		tracer: n,
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

	var tr model.PartialSigTrace
	tr.Type = msg.Type
	tr.BeaconRoot = msg.Messages[0].SigningRoot
	tr.Signer = msg.Messages[0].Signer
	tr.ReceivedTime = time.Now()

	if msg.Type == spectypes.PostConsensusPartialSig {
		roleDutyTrace.Post = append(roleDutyTrace.Post, &tr)
		return
	}

	roleDutyTrace.Pre = append(roleDutyTrace.Pre, &tr)
}

func (n *InMemTracer) processPartialSigCommittee(msg *spectypes.PartialSignatureMessages, committeeID spectypes.CommitteeID) {
	slot := msg.Slot

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
	n.saveValidatorToCommitteeLink(slot, msg, committeeID)

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
	for _, msg := range msg.Messages {
		slotToCommittee, found := n.valToComMapping.Load(msg.ValidatorIndex)
		if !found {
			slotToCommittee, _ = n.valToComMapping.LoadOrStore(msg.ValidatorIndex, NewTypedSyncMap[phase0.Slot, spectypes.CommitteeID]())
		}

		slotToCommittee.Store(slot, committeeID)

		n.logger.Info("store link for", fields.Slot(slot), zap.Uint64("index", uint64(msg.ValidatorIndex)), fields.CommitteeID(committeeID))
	}
}

// nolint:unused
func (n *InMemTracer) getSyncCommitteeRoot(slot phase0.Slot, in []byte) (phase0.Root, error) {
	var beaconVote = new(spectypes.BeaconVote)
	if err := beaconVote.Decode(in); err != nil {
		return phase0.Root{}, fmt.Errorf("decode beacon vote: %w", err)
	}

	epoch := n.beaconNetwork.EstimatedEpochAtSlot(slot)

	// Root
	domain, err := n.client.DomainData(epoch, spectypes.DomainSyncCommittee)
	if err != nil {
		return phase0.Root{}, fmt.Errorf("get sync committee domain: %w", err)
	}

	// Eth root
	blockRoot := spectypes.SSZBytes(beaconVote.BlockRoot[:])

	root, err := spectypes.ComputeETHSigningRoot(blockRoot, domain)
	if err != nil {
		return phase0.Root{}, fmt.Errorf("compute sync committee root: %w", err)
	}

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

				// first step fill in sync committee roots
				// to be later read in 'processPartialSigCommittee'
				// root, err := n.getSyncCommitteeRoot(slot, msg.SignedSSVMessage.FullData)
				// if err != nil {
				// 	n.logger.Error("get sync committee roots", zap.Error(err))
				// }
				// trace.syncCommitteeRoot = root

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

				trace, roleDutyTrace := n.getOrCreateValidatorTrace(slot, bnRole, validatorPK)

				if msg.MsgType == spectypes.SSVConsensusMsgType {
					var qbftMsg specqbft.Message
					err := qbftMsg.Decode(msg.Data)
					if err != nil {
						n.logger.Error("decode validator consensus data", zap.Error(err))
						return
					}

					if qbftMsg.MsgType == specqbft.ProposalMsgType {
						stop := func() bool {
							var data = new(spectypes.ValidatorConsensusData)
							err := data.Decode(msg.SignedSSVMessage.FullData)
							if err != nil {
								n.logger.Error("decode validator proposal data", zap.Error(err))
								return true
							}

							trace.Lock()
							defer trace.Unlock()
							if roleDutyTrace.Validator == 0 {
								roleDutyTrace.Validator = data.Duty.ValidatorIndex
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

				round := getOrCreateRound(&roleDutyTrace.ConsensusTrace, uint64(subMsg.Round))

				// TODO(moshe) populate proposer or not?
				// n.populateProposer(round, validatorPK, subMsg)

				decided := n.processConsensus(subMsg, msg.SignedSSVMessage, round)
				if decided != nil {
					n.logger.Info("validator decideds", fields.Slot(phase0.Slot(subMsg.Height)), fields.Validator(validatorPK[:]))
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
		for _, msg := range pSigMessages.Messages {
			sig, err := decodeSig(msg.PartialSignature)
			if err != nil {
				n.logger.Error("decode partial signature", zap.Error(err))
				continue
			}

			share, found := n.validators.ValidatorByIndex(msg.ValidatorIndex)
			if !found {
				n.logger.Error("get share by index", zap.Uint64("validator", uint64(msg.ValidatorIndex)))
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

// Getters and deep copy functions

func (n *InMemTracer) getValidatorDuty(bnRole spectypes.BeaconRole, slot phase0.Slot, pubkey spectypes.ValidatorPK) (*model.ValidatorDutyTrace, error) {
	validatorSlots, found := n.validatorTraces.Load(pubkey)
	if !found {
		// should only happen if we request a validator duty right after startup
		return nil, errors.New("validator not found")
	}

	traces, found := validatorSlots.Load(slot)
	if !found {
		return n.getValidatorDutyFromDisk(bnRole, slot, pubkey)
	}

	traces.Lock()
	defer traces.Unlock()

	// find the trace for the role
	for _, trace := range traces.Roles {
		if trace.Role == bnRole {
			return deepCopyValidatorDutyTrace(trace), nil
		}
	}

	return nil, fmt.Errorf("validator duty not found for role: %s", bnRole)
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
	committeeSlots, found := n.committeeTraces.Load(committeeID)
	if !found {
		// there is always going to be a committee key in the map
		return nil, errors.New("committee not found")
	}

	trace, found := committeeSlots.Load(slot)
	if !found {
		trace, err := n.store.GetCommitteeDuty(slot, committeeID)
		if err != nil {
			return nil, fmt.Errorf("get committee duty from disk: %w", err)
		}

		if trace != nil {
			return trace, nil
		}

		return nil, errors.New("slot not found")
	}

	trace.Lock()
	defer trace.Unlock()

	return deepCopyCommitteeDutyTrace(&trace.CommitteeDutyTrace), nil
}

func (n *InMemTracer) getCommitteeIDBySlotAndIndex(slot phase0.Slot, index phase0.ValidatorIndex) (spectypes.CommitteeID, error) {
	slotToCommittee, found := n.valToComMapping.Load(index)
	if !found {
		return spectypes.CommitteeID{}, fmt.Errorf("committee not found by index: %d", index)
	}

	committeeID, found := slotToCommittee.Load(slot)
	if !found {
		return n.store.GetCommitteeDutyLink(slot, index)
	}

	return committeeID, nil
}

func decodeSig(in []byte) (*bls.Sign, error) {
	sign := new(bls.Sign)
	err := sign.Deserialize(in)
	return sign, err
}

func deepCopyCommitteeDutyTrace(trace *model.CommitteeDutyTrace) *model.CommitteeDutyTrace {
	return &model.CommitteeDutyTrace{
		ConsensusTrace: model.ConsensusTrace{
			Rounds:   deepCopyRounds(trace.Rounds),
			Decideds: deepCopyDecideds(trace.Decideds),
		},
		Slot:          trace.Slot,
		CommitteeID:   trace.CommitteeID,
		OperatorIDs:   deepCopyOperatorIDs(trace.OperatorIDs),
		SyncCommittee: deepCopySigners(trace.SyncCommittee),
		Attester:      deepCopySigners(trace.Attester),
	}
}

func deepCopyDecideds(decideds []*model.DecidedTrace) []*model.DecidedTrace {
	copy := make([]*model.DecidedTrace, len(decideds))
	for i, d := range decideds {
		copy[i] = deepCopyDecided(d)
	}
	return copy
}

func deepCopyDecided(trace *model.DecidedTrace) *model.DecidedTrace {
	return &model.DecidedTrace{
		Round:        trace.Round,
		BeaconRoot:   trace.BeaconRoot,
		Signers:      deepCopyOperatorIDs(trace.Signers),
		ReceivedTime: trace.ReceivedTime,
	}
}

func deepCopyRounds(rounds []*model.RoundTrace) []*model.RoundTrace {
	copy := make([]*model.RoundTrace, len(rounds))
	for i, r := range rounds {
		copy[i] = deepCopyRound(r)
	}
	return copy
}

func deepCopyRound(round *model.RoundTrace) *model.RoundTrace {
	return &model.RoundTrace{
		Proposer:      round.Proposer,
		Prepares:      deepCopyPrepares(round.Prepares),
		ProposalTrace: deepCopyProposalTrace(round.ProposalTrace),
		Commits:       deepCopyCommits(round.Commits),
		RoundChanges:  deepCopyRoundChanges(round.RoundChanges),
	}
}

func deepCopyProposalTrace(trace *model.ProposalTrace) *model.ProposalTrace {
	return &model.ProposalTrace{
		QBFTTrace: model.QBFTTrace{
			Round:        trace.Round,
			BeaconRoot:   trace.BeaconRoot,
			Signer:       trace.Signer,
			ReceivedTime: trace.ReceivedTime,
		},
		RoundChanges:    deepCopyRoundChanges(trace.RoundChanges),
		PrepareMessages: deepCopyPrepares(trace.PrepareMessages),
	}
}

func deepCopyCommits(commits []*model.QBFTTrace) []*model.QBFTTrace {
	copy := make([]*model.QBFTTrace, len(commits))
	for i, c := range commits {
		copy[i] = deepCopyQBFTTrace(c)
	}
	return copy
}

func deepCopyRoundChanges(roundChanges []*model.RoundChangeTrace) []*model.RoundChangeTrace {
	copy := make([]*model.RoundChangeTrace, len(roundChanges))
	for i, r := range roundChanges {
		copy[i] = deepCopyRoundChange(r)
	}
	return copy
}

func deepCopyRoundChange(trace *model.RoundChangeTrace) *model.RoundChangeTrace {
	return &model.RoundChangeTrace{
		QBFTTrace: model.QBFTTrace{
			Round:        trace.Round,
			BeaconRoot:   trace.BeaconRoot,
			Signer:       trace.Signer,
			ReceivedTime: trace.ReceivedTime,
		},
		PreparedRound:   trace.PreparedRound,
		PrepareMessages: deepCopyPrepares(trace.PrepareMessages),
	}
}

func deepCopyPrepares(prepares []*model.QBFTTrace) []*model.QBFTTrace {
	copy := make([]*model.QBFTTrace, len(prepares))
	for i, p := range prepares {
		copy[i] = deepCopyQBFTTrace(p)
	}
	return copy
}

func deepCopyQBFTTrace(trace *model.QBFTTrace) *model.QBFTTrace {
	return &model.QBFTTrace{
		Round:        trace.Round,
		Signer:       trace.Signer,
		ReceivedTime: trace.ReceivedTime,
	}
}

func deepCopySigners(committee []*model.SignerData) []*model.SignerData {
	copy := make([]*model.SignerData, len(committee))
	for i, c := range committee {
		copy[i] = deepCopySignerData(c)
	}
	return copy
}

func deepCopySignerData(data *model.SignerData) *model.SignerData {
	return &model.SignerData{
		Signers:      deepCopyOperatorIDs(data.Signers),
		ReceivedTime: data.ReceivedTime,
	}
}

func deepCopyOperatorIDs(ids []spectypes.OperatorID) []spectypes.OperatorID {
	copy := make([]spectypes.OperatorID, len(ids))
	copy = append(copy, ids...)
	return copy
}

func deepCopyValidatorDutyTrace(trace *model.ValidatorDutyTrace) *model.ValidatorDutyTrace {
	return &model.ValidatorDutyTrace{
		ConsensusTrace: model.ConsensusTrace{
			Rounds:   trace.Rounds,
			Decideds: trace.Decideds,
		},
		Slot:      trace.Slot,
		Role:      trace.Role,
		Validator: trace.Validator,
		Pre:       deepCopyPartialSigs(trace.Pre),
		Post:      deepCopyPartialSigs(trace.Post),
	}
}

func deepCopyPartialSigs(partialSigs []*model.PartialSigTrace) []*model.PartialSigTrace {
	copy := make([]*model.PartialSigTrace, len(partialSigs))
	for i, p := range partialSigs {
		copy[i] = deepCopyPartialSig(p)
	}
	return copy
}

func deepCopyPartialSig(trace *model.PartialSigTrace) *model.PartialSigTrace {
	return &model.PartialSigTrace{
		Type:         trace.Type,
		BeaconRoot:   trace.BeaconRoot,
		Signer:       trace.Signer,
		ReceivedTime: trace.ReceivedTime,
	}
}
