package duties

import (
	"context"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// PTCAttestationHandler schedules the Gloas (ePBS) Payload Timeliness Committee attestation duty
// (SIP #94 §3): each epoch it fetches the PTC assignments and, for each slot holding one of this
// node's duties, executes it at the 75%-of-slot cutoff so the runner observes payload presence at
// that point and runs its partial-signature round in the otherwise-free [75%, 100%] window.
//
// Like the proposer and sync-committee handlers, it records every participating validator's duty
// (not just this node's) in the shared duty store — so the message validator can reject PTC messages
// from validators with no such assignment — and runs in both operator and exporter modes. InCommittee
// marks this node's own duties, which only operator mode executes.
type PTCAttestationHandler struct {
	baseHandler

	duties       *dutystore.Duties[gloas.PTCDuty]
	exporterMode bool
}

func NewPTCAttestationHandler(duties *dutystore.Duties[gloas.PTCDuty], exporterMode bool) *PTCAttestationHandler {
	return &PTCAttestationHandler{
		duties:       duties,
		exporterMode: exporterMode,
	}
}

func (h *PTCAttestationHandler) Name() string {
	return spectypes.BNRolePTCAttester.String()
}

func (h *PTCAttestationHandler) WaitShutdown() {}

func (h *PTCAttestationHandler) HandleDuties(ctx context.Context) {
	h.logger.Info("starting duty handler")
	defer h.logger.Info("duty handler exited")

	next := h.ticker.Next()
	for {
		select {
		case <-ctx.Done():
			return

		case <-next:
			slot := h.ticker.Slot()
			next = h.ticker.Next()
			epoch := h.netCfg.EstimatedEpochAtSlot(slot)

			// PTC is a Gloas-only duty.
			if !h.netCfg.IsGloas(epoch) {
				continue
			}

			h.fetchDuties(ctx, epoch)
			if h.shouldFetchNextEpoch(slot) {
				h.fetchDuties(ctx, epoch+1)
			}
			h.duties.EraseBefore(epoch)

			// Exporter records duties for message validation but does not execute them.
			if h.exporterMode {
				continue
			}
			if ptcDuties := h.duties.CommitteeSlotDuties(epoch, slot); len(ptcDuties) > 0 {
				specDuties := make([]*spectypes.ValidatorDuty, 0, len(ptcDuties))
				for _, d := range ptcDuties {
					specDuties = append(specDuties, h.toSpecDuty(d))
				}
				h.scheduleExecution(ctx, slot, specDuties)
			}

		case <-h.indicesChangeCh:
			h.invalidateDuties("indices change")
		case <-h.reorgEventsCh:
			h.invalidateDuties("reorg")
		}
	}
}

// HandleInitialDuties fetches the current epoch's PTC duties on startup — and the next epoch's near a
// boundary, so the ticker can't miss the rollover — populating the store before the first tick so the
// message validator can check assignments right away.
func (h *PTCAttestationHandler) HandleInitialDuties(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, h.netCfg.SlotDuration)
	defer cancel()

	slot := h.netCfg.EstimatedCurrentSlot()
	epoch := h.netCfg.EstimatedEpochAtSlot(slot)
	if !h.netCfg.IsGloas(epoch) {
		return
	}

	h.fetchDuties(ctx, epoch)
	if h.shouldFetchNextEpoch(slot) {
		h.fetchDuties(ctx, epoch+1)
	}
}

// invalidateDuties drops the cached PTC duties so the next tick re-fetches them — after a reorg
// (new dependent_root) or a validator-set change the authoritative response replaces the cached
// epochs rather than merging (SIP #94 §3).
func (h *PTCAttestationHandler) invalidateDuties(reason string) {
	h.logger.Debug("🔀 re-fetching PTC duties on next tick", zap.String("reason", reason))
	h.duties.Clear()
}

// fetchDuties records an epoch's PTC duties in the shared store once: every participating validator's
// duty, with this node's own marked InCommittee for execution.
func (h *PTCAttestationHandler) fetchDuties(ctx context.Context, epoch phase0.Epoch) {
	if h.duties.IsEpochSet(epoch) {
		return
	}

	var eligible []phase0.ValidatorIndex
	for _, share := range h.validatorProvider.Validators() {
		if share.IsParticipating(h.netCfg.Beacon, epoch) {
			eligible = append(eligible, share.ValidatorIndex)
		}
	}
	if len(eligible) == 0 {
		return
	}

	ptcDuties, err := h.beaconNode.PayloadAttestationDuties(ctx, epoch, eligible)
	if err != nil {
		h.logger.Warn("failed to fetch PTC duties", fields.Epoch(epoch), zap.Error(err))
		return
	}

	self := make(map[phase0.ValidatorIndex]struct{})
	for _, idx := range h.selfParticipatingIndices(epoch) {
		self[idx] = struct{}{}
	}

	storeDuties := make([]dutystore.StoreDuty[gloas.PTCDuty], 0, len(ptcDuties))
	for _, d := range ptcDuties {
		_, inCommittee := self[d.ValidatorIndex]
		storeDuties = append(storeDuties, dutystore.StoreDuty[gloas.PTCDuty]{
			Slot:           d.Slot,
			ValidatorIndex: d.ValidatorIndex,
			Duty:           d,
			InCommittee:    inCommittee,
		})
	}
	h.duties.Set(epoch, storeDuties)

	h.logger.Debug("fetched PTC duties", fields.Epoch(epoch), zap.Int("duties", len(ptcDuties)))
}

// scheduleExecution fires the duty at the payload-attestation cutoff, with a deadline at slot end.
func (h *PTCAttestationHandler) scheduleExecution(ctx context.Context, slot phase0.Slot, duties []*spectypes.ValidatorDuty) {
	executeAt := h.netCfg.PayloadAttestationCutoff(slot)
	deadline := h.netCfg.SlotStartTime(slot + 1)
	time.AfterFunc(time.Until(executeAt), func() {
		h.dutiesExecutor.ExecuteDuties(ctx, duties, deadline)
	})
}

func (h *PTCAttestationHandler) toSpecDuty(duty *gloas.PTCDuty) *spectypes.ValidatorDuty {
	return &spectypes.ValidatorDuty{
		Type:           spectypes.BNRolePTCAttester,
		PubKey:         duty.PubKey,
		ValidatorIndex: duty.ValidatorIndex,
		Slot:           duty.Slot,
	}
}
