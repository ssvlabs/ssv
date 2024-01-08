package roundthresholds

import (
	"context"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer"
)

const fatalRoundThreshold = 20

// Mapping contains thresholds when each round finishes for each role.
type Mapping struct {
	logger       *zap.Logger
	bn           beaconprotocol.BeaconNetwork
	thresholds   map[spectypes.BeaconRole][]time.Duration
	allowedSlots func(role spectypes.BeaconRole) phase0.Slot
}

func NewMapping(logger *zap.Logger, bn beaconprotocol.BeaconNetwork, allowedSlots func(role spectypes.BeaconRole) phase0.Slot) *Mapping {
	return &Mapping{
		logger:       logger,
		bn:           bn,
		thresholds:   make(map[spectypes.BeaconRole][]time.Duration),
		allowedSlots: allowedSlots,
	}
}

// InitThresholds fills threshold cache for given role.
func (c *Mapping) InitThresholds(role spectypes.BeaconRole) {
	unusedCtx := context.Background()
	rt := roundtimer.New(unusedCtx, c.bn, role, nil)

	round := specqbft.Round(1)
	c.thresholds[role] = []time.Duration{}

	for i := 0; i < fatalRoundThreshold; i++ {
		roundDuration := rt.DurationUntilEndOfRound(round)
		if roundDuration <= 0 {
			c.logger.Fatal("invalid round duration", fields.Round(round))
		}

		c.thresholds[role] = append(c.thresholds[role], roundDuration)

		if roundDuration >= c.maxPossibleDuration(role) {
			return
		}

		round++
	}

	c.logger.Fatal("too many rounds to initialize", fields.Count(fatalRoundThreshold))
}

func (c *Mapping) maxPossibleDuration(role spectypes.BeaconRole) time.Duration {
	return time.Duration(c.allowedSlots(role)) * c.bn.SlotDurationSec()
}

// MaxPossibleRound returns max possible round for given role.
func (c *Mapping) MaxPossibleRound(role spectypes.BeaconRole) specqbft.Round {
	return specqbft.Round(len(c.thresholds[role]))
}

// EstimatedRound returns estimated round for given role and duration.
// If it is out of bounds, it returns the next round after max possible one, which is considered invalid.
func (c *Mapping) EstimatedRound(role spectypes.BeaconRole, sinceSlotStart time.Duration) specqbft.Round {
	for i, threshold := range c.thresholds[role] {
		if sinceSlotStart < threshold {
			return specqbft.Round(i + 1)
		}
	}

	maxPossibleRound := c.MaxPossibleRound(role)
	if maxPossibleRound == ^specqbft.Round(0) {
		panic("max possible round causes overflow")
	}

	return maxPossibleRound + 1
}
