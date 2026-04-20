package queue

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const inboxPressureNumerator = 3
const inboxPressureDenominator = 4

func IsUnderPressure(buffered, capacity int) bool {
	if capacity <= 0 {
		return false
	}
	threshold := (capacity*inboxPressureNumerator + inboxPressureDenominator - 1) / inboxPressureDenominator
	if threshold >= capacity && capacity > 1 {
		threshold = capacity - 1
	}
	return buffered >= threshold
}

// ShouldAcceptUnderPressure keeps current or newer work admissible when inbox pressure is high.
// It rejects only obviously stale messages, leaving strict-full behavior unchanged.
func ShouldAcceptUnderPressure(state *State, msg *SSVMessage, buffered, capacity int) (bool, string) {
	if !IsUnderPressure(buffered, capacity) || state == nil || msg == nil {
		return true, ""
	}

	switch body := msg.Body.(type) {
	case *specqbft.Message:
		if body == nil {
			return false, DropReasonMalformed
		}
		if body.Height < state.Height {
			return false, DropReasonStaleHeight
		}
		if state.HasRunningInstance && body.Height == state.Height && body.Round < state.Round {
			return false, DropReasonStaleRound
		}
	case *spectypes.PartialSignatureMessages:
		if body == nil {
			return false, DropReasonMalformed
		}
		currentSlot := state.Slot
		if currentSlot == 0 {
			currentSlot = phase0.Slot(state.Height)
		}
		// Match runner's [currentDutySlot-1, currentDutySlot] acceptance window.
		minAcceptableSlot := currentSlot
		if currentSlot > 0 {
			minAcceptableSlot = currentSlot - 1
		}
		if body.Slot < minAcceptableSlot {
			return false, DropReasonStaleSlot
		}
	}

	return true, ""
}
