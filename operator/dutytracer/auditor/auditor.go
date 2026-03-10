package auditor

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/exporter/rolemask"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/operator/slotticker"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

type Options struct {
	Enabled bool

	DelaySlots uint64

	// Retention is enforced by pruning older slots; defaults to 14 days.
	Retention time.Duration

	// RPCFallback enables beacon RPC checks for mismatching indices.
	RPCFallback bool

	// RPCMaxIndicesPerSlot caps the number of indices verified via RPC per slot (per role).
	RPCMaxIndicesPerSlot int
}

type BeaconRPC interface {
	AttesterDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error)
	SyncCommitteeDuties(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.SyncCommitteeDuty, error)
}

type TraceReader interface {
	GetCommitteeDuties(slot phase0.Slot, roles ...spectypes.BeaconRole) ([]*exporter.CommitteeDutyTrace, error)
	GetScheduled(slot phase0.Slot) (map[phase0.ValidatorIndex]rolemask.Mask, error)
	GetCommitteeDutyLinks(slot phase0.Slot) ([]*exporter.CommitteeDutyLink, error)
}

type obsItem struct {
	role        Role
	committeeID spectypes.CommitteeID
	index       phase0.ValidatorIndex
	signers     []uint64
	msgCount    int
}

// Reporter is implemented by the auditor and can be passed to other subsystems
// (e.g., duty fetching, schedule filler) to collect pipeline evidence.
type Reporter interface {
	RecordDutyFetch(ev DutyFetchEvent)
	RecordScheduleCompute(ev ScheduleComputeEvent)
	RecordScheduleJobDropped(slot phase0.Slot)
}

type DutyFetchEvent struct {
	Role spectypes.BeaconRole

	// Exactly one of Epoch / Period is relevant depending on Role.
	Epoch  *phase0.Epoch
	Period *uint64

	At        time.Time
	Took      time.Duration
	Requested int
	Returned  int
	Err       error
}

type ScheduleComputeEvent struct {
	Slot phase0.Slot
	At   time.Time
	Took time.Duration
	Size int
	Err  error
}

type Auditor struct {
	logger *zap.Logger
	cfg    *networkconfig.Beacon

	traces   TraceReader
	duties   *dutystore.Store
	registry registrystorage.ValidatorStore
	beacon   BeaconRPC

	store Store
	opts  Options

	mu sync.RWMutex

	lastAuditedSlot phase0.Slot

	// pipeline evidence
	lastDutyFetch       map[string]DutyFetchEvent
	lastScheduleCompute map[phase0.Slot]ScheduleComputeEvent
	scheduleJobDrops    map[phase0.Slot]uint64

	reauditCh chan phase0.Slot
}

func New(logger *zap.Logger, cfg *networkconfig.Beacon, traces TraceReader, duties *dutystore.Store, registry registrystorage.ValidatorStore, beacon BeaconRPC, store Store, opts Options) *Auditor {
	if opts.DelaySlots == 0 {
		opts.DelaySlots = 4
	}
	if opts.Retention == 0 {
		opts.Retention = 14 * 24 * time.Hour
	}
	if opts.RPCMaxIndicesPerSlot <= 0 {
		opts.RPCMaxIndicesPerSlot = 2048
	}
	return &Auditor{
		logger:              logger.Named("auditor"),
		cfg:                 cfg,
		traces:              traces,
		duties:              duties,
		registry:            registry,
		beacon:              beacon,
		store:               store,
		opts:                opts,
		lastDutyFetch:       make(map[string]DutyFetchEvent),
		lastScheduleCompute: make(map[phase0.Slot]ScheduleComputeEvent),
		scheduleJobDrops:    make(map[phase0.Slot]uint64),
		reauditCh:           make(chan phase0.Slot, 256),
	}
}

func (a *Auditor) Enabled() bool { return a != nil && a.opts.Enabled }

func (a *Auditor) LastAuditedSlot() phase0.Slot {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.lastAuditedSlot
}

func (a *Auditor) Query(q Query) (QueryResult, error) {
	if a == nil || a.store == nil {
		return QueryResult{}, fmt.Errorf("auditor store not initialized")
	}
	return a.store.Query(q)
}

// NotifyLateSlot schedules a re-audit for a slot that already passed the fixed-delay audit window.
func (a *Auditor) NotifyLateSlot(slot phase0.Slot) {
	if !a.Enabled() {
		return
	}
	select {
	case a.reauditCh <- slot:
	default:
	}
}

func (a *Auditor) Start(ctx context.Context, tickerProvider slotticker.Provider) {
	if !a.Enabled() {
		return
	}

	// Protect the node from auditor failures.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				a.logger.Error("auditor panicked", zap.Any("panic", r))
			}
		}()

		t := tickerProvider()
		pruneEvery := phase0.Slot(a.cfg.SlotsPerEpoch) // prune once/epoch

		for {
			select {
			case <-ctx.Done():
				return
			case <-t.Next():
				nowSlot := t.Slot()
				if uint64(nowSlot) < a.opts.DelaySlots {
					continue
				}
				auditSlot := phase0.Slot(uint64(nowSlot) - a.opts.DelaySlots)
				a.auditBestEffort(ctx, auditSlot)

				if pruneEvery > 0 && auditSlot%pruneEvery == 0 {
					a.pruneBestEffort(ctx, nowSlot)
				}
			case s := <-a.reauditCh:
				a.auditBestEffort(ctx, s)
			}
		}
	}()
}

func (a *Auditor) auditBestEffort(ctx context.Context, slot phase0.Slot) {
	if !a.Enabled() {
		return
	}
	if err := a.AuditSlot(ctx, slot); err != nil {
		a.logger.Warn("audit slot failed", fields.Slot(slot), zap.Error(err))
	}
	a.mu.Lock()
	if slot > a.lastAuditedSlot {
		a.lastAuditedSlot = slot
	}
	a.mu.Unlock()
	u := uint64(a.lastAuditedSlot)
	v := int64(u)
	if u > uint64(math.MaxInt64) {
		v = math.MaxInt64
	}
	lastAuditedSlotGauge.Record(context.Background(), v)
}

func (a *Auditor) pruneBestEffort(ctx context.Context, nowSlot phase0.Slot) {
	retSlotsI64 := a.opts.Retention / a.cfg.SlotDuration
	if retSlotsI64 <= 0 {
		return
	}
	// #nosec G115 -- retSlotsI64 is checked to be > 0 above.
	retSlots := phase0.Slot(uint64(retSlotsI64))
	if retSlots == 0 {
		return
	}
	if nowSlot <= retSlots {
		return
	}
	cutoff := nowSlot - retSlots
	if err := a.store.Prune(cutoff); err != nil {
		a.logger.Warn("prune failed", zap.Error(err), zap.Uint64("cutoff_slot", uint64(cutoff)))
	}
}

func (a *Auditor) RecordDutyFetch(ev DutyFetchEvent) {
	if !a.Enabled() {
		return
	}
	ev.At = ev.At.UTC()
	key := dutyFetchKey(ev.Role, ev.Epoch, ev.Period)
	a.mu.Lock()
	a.lastDutyFetch[key] = ev
	a.mu.Unlock()
}

func (a *Auditor) RecordScheduleCompute(ev ScheduleComputeEvent) {
	if !a.Enabled() {
		return
	}
	ev.At = ev.At.UTC()
	a.mu.Lock()
	a.lastScheduleCompute[ev.Slot] = ev
	a.mu.Unlock()
}

func (a *Auditor) RecordScheduleJobDropped(slot phase0.Slot) {
	if !a.Enabled() {
		return
	}
	a.mu.Lock()
	a.scheduleJobDrops[slot]++
	a.mu.Unlock()
}

func dutyFetchKey(role spectypes.BeaconRole, epoch *phase0.Epoch, period *uint64) string {
	if epoch != nil {
		return fmt.Sprintf("%s-e%d", role.String(), uint64(*epoch))
	}
	if period != nil {
		return fmt.Sprintf("%s-p%d", role.String(), *period)
	}
	return role.String()
}

func (a *Auditor) AuditSlot(ctx context.Context, slot phase0.Slot) error {
	epoch := a.cfg.EstimatedEpochAtSlot(slot)
	period := a.cfg.EstimatedSyncCommitteePeriodAtEpoch(epoch)

	traces, err := a.traces.GetCommitteeDuties(slot)
	if err != nil {
		return fmt.Errorf("get committee duties: %w", err)
	}
	if len(traces) == 0 {
		return nil
	}

	persisted, err := a.traces.GetScheduled(slot)
	if err != nil {
		// Continue with evidence; schedule read failures are part of the story.
		a.logger.Debug("get persisted schedule failed", fields.Slot(slot), zap.Error(err))
	}

	links, errLinks := a.traces.GetCommitteeDutyLinks(slot)
	if errLinks != nil {
		a.logger.Debug("get links failed", fields.Slot(slot), zap.Error(errLinks))
	}
	linksByIndex := make(map[phase0.ValidatorIndex]spectypes.CommitteeID, len(links))
	for _, l := range links {
		linksByIndex[l.ValidatorIndex] = l.CommitteeID
	}

	// Expected sets from duty store (best-effort).
	expectedAtt := a.expectedAttesterSet(epoch, slot)
	expectedSync := a.expectedSyncSet(period)

	// Registry mapping for the epoch.
	expectedCommitteeByIndex := buildValidatorToCommitteeIndex(a.registry.ParticipatingCommittees(epoch))

	// Collect mismatching indices that need RPC verification.
	var needRPCAtt []phase0.ValidatorIndex
	var needRPCSync []phase0.ValidatorIndex

	observed := make([]obsItem, 0, 1024)
	for _, tr := range traces {
		if tr == nil {
			continue
		}
		// Attester observed indices
		for _, sd := range tr.Attester {
			for _, idx := range sd.ValidatorIdx {
				observed = append(observed, obsItem{
					role: RoleAttester, committeeID: tr.CommitteeID, index: idx,
					signers: []uint64{sd.Signer}, msgCount: 1,
				})
			}
		}
		// Sync committee observed indices
		for _, sd := range tr.SyncCommittee {
			for _, idx := range sd.ValidatorIdx {
				observed = append(observed, obsItem{
					role: RoleSyncCommittee, committeeID: tr.CommitteeID, index: idx,
					signers: []uint64{sd.Signer}, msgCount: 1,
				})
			}
		}
	}
	if len(observed) == 0 {
		return nil
	}

	// Coalesce observed items (same slot,role,committee,index) to improve evidence quality.
	slices.SortFunc(observed, func(a, b obsItem) int {
		if a.role != b.role {
			return cmpString(string(a.role), string(b.role))
		}
		if a.index != b.index {
			return cmpU64(uint64(a.index), uint64(b.index))
		}
		return cmpBytes(a.committeeID[:], b.committeeID[:])
	})
	coalesced := make([]obsItem, 0, len(observed))
	for _, it := range observed {
		n := len(coalesced)
		if n == 0 || coalesced[n-1].role != it.role || coalesced[n-1].index != it.index || coalesced[n-1].committeeID != it.committeeID {
			coalesced = append(coalesced, it)
			continue
		}
		coalesced[n-1].msgCount += it.msgCount
		coalesced[n-1].signers = append(coalesced[n-1].signers, it.signers...)
	}
	for i := range coalesced {
		slices.Sort(coalesced[i].signers)
		coalesced[i].signers = slices.Compact(coalesced[i].signers)
		// keep evidence reasonably small
		if len(coalesced[i].signers) > 8 {
			coalesced[i].signers = coalesced[i].signers[:8]
		}
	}

	// Identify which indices require RPC fallback checks.
	for _, it := range coalesced {
		var expectedByDutyStore bool
		switch it.role {
		case RoleAttester:
			expectedByDutyStore = expectedAtt[it.index]
		case RoleSyncCommittee:
			expectedByDutyStore = expectedSync[it.index]
		default:
			continue
		}
		hasRoleInPersisted := persistedHasRole(persisted, it.index, it.role)
		if hasRoleInPersisted {
			continue
		}
		if expectedByDutyStore {
			continue
		}
		if !a.opts.RPCFallback {
			continue
		}
		if it.role == RoleAttester && len(needRPCAtt) < a.opts.RPCMaxIndicesPerSlot {
			needRPCAtt = append(needRPCAtt, it.index)
		}
		if it.role == RoleSyncCommittee && len(needRPCSync) < a.opts.RPCMaxIndicesPerSlot {
			needRPCSync = append(needRPCSync, it.index)
		}
	}

	rpcAtt, rpcAttErr := a.rpcCheckAttester(ctx, epoch, slot, needRPCAtt)
	rpcSync, rpcSyncErr := a.rpcCheckSync(ctx, epoch, needRPCSync)
	rpcReqAtt := make(map[phase0.ValidatorIndex]struct{}, len(needRPCAtt))
	for _, idx := range needRPCAtt {
		rpcReqAtt[idx] = struct{}{}
	}
	rpcReqSync := make(map[phase0.ValidatorIndex]struct{}, len(needRPCSync))
	for _, idx := range needRPCSync {
		rpcReqSync[idx] = struct{}{}
	}

	// Emit findings for mismatches.
	for _, it := range coalesced {
		role := it.role
		index := it.index
		committeeID := it.committeeID

		persistHasRole := persistedHasRole(persisted, index, role)
		if persistHasRole {
			// Schedule says it should be there; now verify mapping.
			if err := a.checkCommitteeMapping(slot, epoch, period, role, committeeID, index, expectedCommitteeByIndex, linksByIndex, persisted, it); err != nil {
				a.logger.Debug("mapping check error", zap.Error(err))
			}
			continue
		}

		expectedByDutyStore := false
		expectedOtherRole := false
		switch role {
		case RoleAttester:
			expectedByDutyStore = expectedAtt[index]
			expectedOtherRole = expectedSync[index]
		case RoleSyncCommittee:
			expectedByDutyStore = expectedSync[index]
			expectedOtherRole = expectedAtt[index]
		}

		rpc := RPCFallbackEvidence{Enabled: a.opts.RPCFallback}
		var rpcExpected bool
		var rpcExpectedSlot *uint64
		var rpcErr error
		rpcUsed := false
		rpcSkipped := false
		if a.opts.RPCFallback {
			switch role {
			case RoleAttester:
				if _, ok := rpcReqAtt[index]; ok {
					rpcUsed = true
					rpc.Used = true
					if rpcAttErr != nil {
						rpcErr = rpcAttErr
					} else if info, ok := rpcAtt[index]; ok {
						rpcExpected = info.expected
						if info.expectedSlot != nil {
							v := uint64(*info.expectedSlot)
							rpcExpectedSlot = &v
						}
					}
				} else {
					rpcSkipped = true
				}
			case RoleSyncCommittee:
				if _, ok := rpcReqSync[index]; ok {
					rpcUsed = true
					rpc.Used = true
					if rpcSyncErr != nil {
						rpcErr = rpcSyncErr
					} else if expected, ok := rpcSync[index]; ok {
						rpcExpected = expected
					}
				} else {
					rpcSkipped = true
				}
			}
			if rpcUsed {
				if rpcErr == nil {
					rpc.OK = ptrBool(true)
				} else {
					rpc.OK = ptrBool(false)
					rpc.Error = rpcErr.Error()
				}
				rpc.AttesterExpectedSlot = rpcExpectedSlot
			}
		}

		reason, ev := a.chooseReasonForScheduleMismatch(slot, epoch, period, role, index, persisted, expectedByDutyStore, rpcUsed, rpcSkipped, rpcExpected, rpcErr, expectedOtherRole)
		if reason == "" {
			continue
		}

		committeeHex := committeeIDHex(committeeID)
		indexU64 := uint64(index)
		roleCopy := role

		finding := &Finding{
			Version:   1,
			CreatedAt: time.Now().UTC(),
			Slot:      uint64(slot),
			Epoch:     uint64(epoch),
			Period: func() *uint64 {
				if role == RoleSyncCommittee {
					v := period
					return &v
				}
				return nil
			}(),
			Reason:         reason,
			Role:           &roleCopy,
			ValidatorIndex: &indexU64,
			CommitteeID:    &committeeHex,
			Evidence: Evidence{
				Observed: ObservedEvidence{
					SignersCount:  len(it.signers),
					Signers:       it.signers,
					MessagesCount: it.msgCount,
				},
				PersistedSchedule: ev.persistedSchedule,
				Expected: ExpectedEvidence{
					ByDutyStore:       ptrBool(expectedByDutyStore),
					RPCFallback:       rpc,
					ExpectedOtherRole: expectedOtherRole,
				},
				Registry: ev.registry,
				Links:    ev.links,
				Pipeline: ev.pipeline,
			},
		}

		a.emitFinding(finding)
	}

	return nil
}

type mappingInfo struct {
	expected     bool
	expectedSlot *phase0.Slot
}

func (a *Auditor) rpcCheckAttester(ctx context.Context, epoch phase0.Epoch, slot phase0.Slot, indices []phase0.ValidatorIndex) (map[phase0.ValidatorIndex]mappingInfo, error) {
	out := make(map[phase0.ValidatorIndex]mappingInfo, len(indices))
	if !a.opts.RPCFallback || a.beacon == nil || len(indices) == 0 {
		return out, nil
	}
	attesterRPCRequests.Add(context.Background(), 1)
	duties, err := a.beacon.AttesterDuties(ctx, epoch, indices)
	if err != nil {
		attesterRPCErrors.Add(context.Background(), 1)
		return out, err
	}
	// Build index->duty slot map.
	m := make(map[phase0.ValidatorIndex]phase0.Slot, len(duties))
	for _, d := range duties {
		if d != nil {
			m[d.ValidatorIndex] = d.Slot
		}
	}
	for _, idx := range indices {
		if dutySlot, ok := m[idx]; ok {
			exp := dutySlot == slot
			ds := dutySlot
			out[idx] = mappingInfo{expected: exp, expectedSlot: &ds}
		} else {
			out[idx] = mappingInfo{expected: false}
		}
	}
	return out, nil
}

func (a *Auditor) rpcCheckSync(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) (map[phase0.ValidatorIndex]bool, error) {
	out := make(map[phase0.ValidatorIndex]bool, len(indices))
	if !a.opts.RPCFallback || a.beacon == nil || len(indices) == 0 {
		return out, nil
	}
	syncRPCRequests.Add(context.Background(), 1)
	duties, err := a.beacon.SyncCommitteeDuties(ctx, epoch, indices)
	if err != nil {
		syncRPCErrors.Add(context.Background(), 1)
		return out, err
	}
	seen := make(map[phase0.ValidatorIndex]struct{}, len(duties))
	for _, d := range duties {
		if d != nil {
			seen[d.ValidatorIndex] = struct{}{}
		}
	}
	for _, idx := range indices {
		_, ok := seen[idx]
		out[idx] = ok
	}
	return out, nil
}

func (a *Auditor) expectedAttesterSet(epoch phase0.Epoch, slot phase0.Slot) map[phase0.ValidatorIndex]bool {
	out := make(map[phase0.ValidatorIndex]bool)
	if a.duties == nil || a.duties.Attester == nil {
		return out
	}
	for _, d := range a.duties.Attester.CommitteeSlotDuties(epoch, slot) {
		if d != nil {
			out[d.ValidatorIndex] = true
		}
	}
	return out
}

func (a *Auditor) expectedSyncSet(period uint64) map[phase0.ValidatorIndex]bool {
	out := make(map[phase0.ValidatorIndex]bool)
	if a.duties == nil || a.duties.SyncCommittee == nil {
		return out
	}
	for _, d := range a.duties.SyncCommittee.CommitteePeriodDuties(period) {
		if d != nil {
			out[d.ValidatorIndex] = true
		}
	}
	return out
}

func persistedHasRole(persisted map[phase0.ValidatorIndex]rolemask.Mask, index phase0.ValidatorIndex, role Role) bool {
	mask, ok := persisted[index]
	if !ok {
		return false
	}
	switch role {
	case RoleAttester:
		return rolemask.Has(mask, spectypes.BNRoleAttester)
	case RoleSyncCommittee:
		return rolemask.Has(mask, spectypes.BNRoleSyncCommittee)
	default:
		return false
	}
}

type reasonEvidence struct {
	persistedSchedule ScheduleEvidence
	registry          RegistryEvidence
	links             LinksEvidence
	pipeline          PipelineEvidence
}

func (a *Auditor) chooseReasonForScheduleMismatch(slot phase0.Slot, epoch phase0.Epoch, period uint64, role Role, index phase0.ValidatorIndex, persisted map[phase0.ValidatorIndex]rolemask.Mask, expectedByDutyStore bool, rpcUsed bool, rpcSkipped bool, rpcExpected bool, rpcErr error, expectedOtherRole bool) (ReasonCode, reasonEvidence) {
	ev := reasonEvidence{}

	mask, hasIndex := persisted[index]
	hasRole := false
	switch role {
	case RoleAttester:
		hasRole = rolemask.Has(mask, spectypes.BNRoleAttester)
	case RoleSyncCommittee:
		hasRole = rolemask.Has(mask, spectypes.BNRoleSyncCommittee)
	}
	ev.persistedSchedule = ScheduleEvidence{
		ScheduleSize: len(persisted),
		HasIndex:     hasIndex,
		MaskBits:     maskToBits(mask),
		HasRole:      hasRole,
	}

	// Populate registry evidence.
	if share, ok := a.registry.ValidatorByIndex(index); ok && share != nil {
		ev.registry.ValidatorKnown = true
		hasMeta := share.HasBeaconMetadata()
		ev.registry.HasBeaconMetadata = &hasMeta
		if share.MinParticipationEpoch() != 0 {
			mpe := uint64(share.MinParticipationEpoch())
			ev.registry.MinParticipationEpoch = &mpe
		}
	} else {
		ev.registry.ValidatorKnown = false
	}

	// Pipeline evidence snapshot.
	ev.pipeline.ScheduleJobDroppedCount = a.getJobDropCount(slot)
	if sc, ok := a.getScheduleCompute(slot); ok {
		ev.pipeline.ScheduleCompute = &ScheduleComputeEvidence{
			ComputedAt:           sc.At,
			OK:                   sc.Err == nil,
			Error:                errString(sc.Err),
			ComputedScheduleSize: sc.Size,
		}
	}
	if df, ok := a.getDutyFetchEvidence(role, epoch, period); ok {
		ev.pipeline.DutyFetch = df
	}

	// If schedule was computed before duties were ready (late duty fetch), attribute accordingly.
	if ev.pipeline.ScheduleCompute != nil && ev.pipeline.DutyFetch != nil && ev.pipeline.DutyFetch.OK {
		readyAt := ev.pipeline.DutyFetch.At.Add(time.Duration(ev.pipeline.DutyFetch.TookMs) * time.Millisecond)
		if ev.pipeline.ScheduleCompute.ComputedAt.Before(readyAt) {
			return ReasonScheduleBeforeDutiesReady, ev
		}
	}

	// If we have an explicit duty fetch failure, it is the most actionable.
	if ev.pipeline.DutyFetch != nil && !ev.pipeline.DutyFetch.OK {
		return ReasonDutyFetchFailed, ev
	}
	// If schedule compute failed or job was dropped, attribute to schedule pipeline.
	if ev.pipeline.ScheduleCompute != nil && !ev.pipeline.ScheduleCompute.OK {
		return ReasonScheduleComputeFailed, ev
	}
	if ev.pipeline.ScheduleJobDroppedCount > 0 {
		return ReasonScheduleJobDropped, ev
	}

	// Persisted schedule empty while we do have traces: treat as schedule not computed.
	if len(persisted) == 0 {
		return ReasonScheduleNotComputed, ev
	}

	// Expected by duty store: schedule is missing/stale.
	if expectedByDutyStore {
		if hasIndex {
			return ReasonScheduleRoleBitMissing, ev
		}
		return ReasonScheduleMissingIndex, ev
	}

	// Duty store does not expect it; if RPC confirms it, duty store is incomplete.
	if a.opts.RPCFallback {
		if rpcSkipped && !rpcUsed {
			return ReasonRPCFallbackSkipped, ev
		}
		if !rpcUsed {
			return ReasonUnexpectedWireTrace, ev
		}
		if rpcErr != nil {
			return ReasonRPCFallbackFailed, ev
		}
		if rpcExpected {
			return ReasonDutyStoreIncomplete, ev
		}
		if expectedOtherRole {
			return ReasonRoleClassificationSuspect, ev
		}
		return ReasonUnexpectedWireTrace, ev
	}

	return ReasonUnexpectedWireTrace, ev
}

func (a *Auditor) checkCommitteeMapping(slot phase0.Slot, epoch phase0.Epoch, period uint64, role Role, observedCommittee spectypes.CommitteeID, index phase0.ValidatorIndex, expectedCommittee map[phase0.ValidatorIndex]spectypes.CommitteeID, linksByIndex map[phase0.ValidatorIndex]spectypes.CommitteeID, persisted map[phase0.ValidatorIndex]rolemask.Mask, it obsItem) error {
	expCID, expOK := expectedCommittee[index]
	linkCID, linkOK := linksByIndex[index]

	committeeHex := committeeIDHex(observedCommittee)
	roleCopy := role
	indexU64 := uint64(index)

	ev := reasonEvidence{
		persistedSchedule: ScheduleEvidence{
			ScheduleSize: len(persisted),
			HasIndex:     true,
			MaskBits:     maskToBits(persisted[index]),
			HasRole:      true,
		},
	}

	if share, ok := a.registry.ValidatorByIndex(index); ok && share != nil {
		ev.registry.ValidatorKnown = true
		hasMeta := share.HasBeaconMetadata()
		ev.registry.HasBeaconMetadata = &hasMeta
	} else {
		ev.registry.ValidatorKnown = false
	}

	if expOK {
		h := committeeIDHex(expCID)
		ev.registry.ExpectedCommitteeID = &h
	}
	ev.links.LinkPresent = linkOK
	if linkOK {
		h := committeeIDHex(linkCID)
		ev.links.LinkedCommitteeID = &h
	}

	// Determine mismatch reason.
	var reason ReasonCode
	switch {
	case !expOK:
		reason = ReasonRegistryIndexNotFound
	case !linkOK:
		reason = ReasonCommitteeLinkMissing
	case linkCID != observedCommittee:
		reason = ReasonCommitteeLinkMismatch
	case expCID != observedCommittee:
		reason = ReasonRegistryCommitteeMismatch
	default:
		return nil
	}

	finding := &Finding{
		Version:   1,
		CreatedAt: time.Now().UTC(),
		Slot:      uint64(slot),
		Epoch:     uint64(epoch),
		Period: func() *uint64 {
			if role == RoleSyncCommittee {
				v := period
				return &v
			}
			return nil
		}(),
		Reason:         reason,
		Role:           &roleCopy,
		ValidatorIndex: &indexU64,
		CommitteeID:    &committeeHex,
		Evidence: Evidence{
			Observed: ObservedEvidence{
				SignersCount:  len(it.signers),
				Signers:       it.signers,
				MessagesCount: it.msgCount,
			},
			PersistedSchedule: ev.persistedSchedule,
			Expected: ExpectedEvidence{
				RPCFallback: RPCFallbackEvidence{Enabled: a.opts.RPCFallback, Used: false},
			},
			Registry: ev.registry,
			Links:    ev.links,
			Pipeline: ev.pipeline,
		},
	}
	a.emitFinding(finding)
	return nil
}

func (a *Auditor) emitFinding(f *Finding) {
	if f == nil {
		return
	}
	// Metrics always.
	findingsTotal.Add(context.Background(), 1, reasonAttr(string(f.Reason)))

	// Logs always (as requested). Keep it compact; evidence is still in stored record.
	a.logger.Warn("auditor mismatch",
		zap.String("reason", string(f.Reason)),
		zap.Uint64("slot", f.Slot),
		zap.String("committee_id", safeStr(f.CommitteeID)),
		zap.Any("role", f.Role),
		zap.Any("validator_index", f.ValidatorIndex),
	)

	res, err := a.store.PutFinding(f)
	if err != nil {
		droppedFindings.Add(context.Background(), 1, dropWhyAttr("store_error"), reasonAttr(string(f.Reason)))
		a.logger.Debug("failed to persist finding", zap.Error(err))
		return
	}
	if !res.Stored {
		droppedFindings.Add(context.Background(), 1, dropWhyAttr("cap_reached"), reasonAttr(string(f.Reason)))
	}
}

func (a *Auditor) getJobDropCount(slot phase0.Slot) uint64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.scheduleJobDrops[slot]
}

func (a *Auditor) getScheduleCompute(slot phase0.Slot) (ScheduleComputeEvent, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	ev, ok := a.lastScheduleCompute[slot]
	return ev, ok
}

func (a *Auditor) getDutyFetchEvidence(role Role, epoch phase0.Epoch, period uint64) (*DutyFetchEvidence, bool) {
	var key string
	switch role {
	case RoleAttester:
		key = dutyFetchKey(spectypes.BNRoleAttester, &epoch, nil)
	case RoleSyncCommittee:
		key = dutyFetchKey(spectypes.BNRoleSyncCommittee, nil, &period)
	default:
		return nil, false
	}
	a.mu.RLock()
	ev, ok := a.lastDutyFetch[key]
	a.mu.RUnlock()
	if !ok {
		return nil, false
	}
	return &DutyFetchEvidence{
		At:        ev.At,
		OK:        ev.Err == nil,
		Error:     errString(ev.Err),
		TookMs:    ev.Took.Milliseconds(),
		Requested: ev.Requested,
		Returned:  ev.Returned,
	}, true
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func safeStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func cmpU64(a, b uint64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func cmpString(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func cmpBytes(a, b []byte) int {
	return slices.Compare(a, b)
}
