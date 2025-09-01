package duties

import (
	"context"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// ctxWithDeadlineOnNextSlot returns the derived context with deadline set to next slot (+ some safety margin
// to account for clock skews).
func (h *baseHandler) ctxWithDeadlineOnNextSlot(ctx context.Context, slot phase0.Slot) (context.Context, context.CancelFunc) {
	return context.WithDeadline(ctx, h.beaconConfig.SlotStartTime(slot+1).Add(100*time.Millisecond))
}
