package validator

import (
	"slices"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	model "github.com/ssvlabs/ssv/exporter/v2"
	"github.com/ssvlabs/ssv/logging/fields"
	"go.uber.org/zap"
)

func (c *Collector) evictLateTraces(currentSlot phase0.Slot) {
	c.lateMu.Lock()
	defer c.lateMu.Unlock()

	threshold := currentSlot - c.lateArrivalThreshold

	var vindices []int

	for i, trace := range c.lateValidatorTraces {
		for _, role := range trace.Roles {
			if role.Slot > threshold {
				continue
			}

			if c.updateValidatorTrace(role) {
				vindices = append(vindices, i)
			}
		}
	}
	// compact validator traces
	keepIdx := 0
	for i, trace := range c.lateValidatorTraces {
		if !slices.Contains(vindices, i) {
			c.lateValidatorTraces[keepIdx] = trace
			keepIdx++
		}
	}

	c.lateValidatorTraces = c.lateValidatorTraces[:keepIdx]
	c.logger.Info("evicted late validator duty traces to disk", zap.Int("count", len(vindices)))

	var cindices []int

	for i, trace := range c.lateCommitteeTraces {
		if trace.Slot > threshold {
			continue
		}

		if c.updateCommitteeTrace(&trace.CommitteeDutyTrace) {
			cindices = append(cindices, i)
		}
	}

	// compact committee traces
	keepIdx = 0
	for i, trace := range c.lateCommitteeTraces {
		if !slices.Contains(cindices, i) {
			c.lateCommitteeTraces[keepIdx] = trace
			keepIdx++
		}
	}

	c.lateCommitteeTraces = c.lateCommitteeTraces[:keepIdx]
	c.logger.Info("evicted late committee duty traces to disk", zap.Int("count", len(cindices)))

	var lindices []int
	for i, link := range c.lateLinks {
		if link.slot > threshold {
			continue
		}

		err := c.store.SaveCommitteeDutyLink(link.slot, link.validatorIndex, link.committeeID)
		if err != nil {
			c.logger.Error("failed to save committee duty link", zap.Error(err))
		}

		c.logger.Debug("#tmplog saved late validator to committee link", fields.Slot(link.slot), fields.ValidatorIndex(link.validatorIndex), fields.CommitteeID(link.committeeID))

		lindices = append(lindices, i)
	}

	// compact links
	keepIdx = 0
	for i, link := range c.lateLinks {
		if !slices.Contains(lindices, i) {
			c.lateLinks[keepIdx] = link
			keepIdx++
		}
	}
	c.lateLinks = c.lateLinks[:keepIdx]
	c.logger.Info("evicted late validator to committee links to disk", zap.Int("count", len(lindices)))
}

func (c *Collector) updateCommitteeTrace(lateTrace *model.CommitteeDutyTrace) bool {
	existing, err := c.store.GetCommitteeDuty(lateTrace.Slot, lateTrace.CommitteeID)
	if err != nil {
		c.logger.Error("failed to get committee duty for update", zap.Error(err))
		return false
	}

	mergeCommittee(existing, lateTrace)

	err = c.store.SaveCommitteeDuty(existing)
	if err != nil {
		c.logger.Error("failed to save committee duty for update", zap.Error(err))
		return false
	}

	return true
}

func (c *Collector) updateValidatorTrace(trace *model.ValidatorDutyTrace) bool {
	existing, err := c.store.GetValidatorDuty(trace.Slot, trace.Role, trace.Validator)
	if err != nil {
		c.logger.Error("failed to get validator duty for update", zap.Error(err))
		return false
	}

	mergeValidator(existing, trace)

	err = c.store.SaveValidatorDuty(existing)
	if err != nil {
		c.logger.Error("failed to save validator duty for update", zap.Error(err))
		return false
	}

	return true
}

func mergeValidator(dst *model.ValidatorDutyTrace, src *model.ValidatorDutyTrace) {
	// Merge proposal data if not set
	if len(dst.ProposalData) == 0 && len(src.ProposalData) > 0 {
		dst.ProposalData = src.ProposalData
	}

	// consensus trace
	dst.Rounds = append(dst.Rounds, src.Rounds...)
	dst.Decideds = append(dst.Decideds, src.Decideds...)

	// pre
	dst.Pre = append(dst.Pre, src.Pre...)

	// post
	dst.Post = append(dst.Post, src.Post...)
}

func mergeCommittee(dst *model.CommitteeDutyTrace, src *model.CommitteeDutyTrace) {
	// consensus trace
	dst.Rounds = append(dst.Rounds, src.Rounds...)
	dst.Decideds = append(dst.Decideds, src.Decideds...)

	// operator ids
	dst.OperatorIDs = append(dst.OperatorIDs, src.OperatorIDs...)

	// proposal data
	if len(dst.ProposalData) == 0 && len(src.ProposalData) > 0 {
		dst.ProposalData = src.ProposalData
	}

	// sync committee
	dst.SyncCommittee = append(dst.SyncCommittee, src.SyncCommittee...)

	// attester
	dst.Attester = append(dst.Attester, src.Attester...)
}
