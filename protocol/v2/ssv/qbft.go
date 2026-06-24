package ssv

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	"go.uber.org/zap"
)

type QBFTRoundTimer interface {
	// TimeoutForRound will reset running round timer if exists and will start a new timer for a specific round.
	TimeoutForRound(round specqbft.Round)
	// Stop will terminate running round timer, releasing resources.
	Stop()
}

// QBFTRoundTimerF builds a fresh QBFTRoundTimer for a given QBFT instance. The caller (usually the runner) binds
// the beacon config, role, identifier, and timeout callback factory into the closure, so the QBFT layer itself
// doesn't need to know about any of those.
type QBFTRoundTimerF func(ctx context.Context, logger *zap.Logger, slot phase0.Slot) QBFTRoundTimer
