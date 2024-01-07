package roundthresholds

import (
	"context"
	"sync"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer"
)

// Cache contains thresholds when each round finishes for each role.
type Cache struct {
	logger     *zap.Logger
	bn         beaconprotocol.BeaconNetwork
	thresholds map[spectypes.BeaconRole][]time.Duration
	mu         sync.Mutex
}

func New(logger *zap.Logger, bn beaconprotocol.BeaconNetwork) *Cache {
	return &Cache{
		logger:     logger,
		bn:         bn,
		thresholds: make(map[spectypes.BeaconRole][]time.Duration),
		mu:         sync.Mutex{},
	}
}

// InitThresholds fills threshold cache for given role.
func (c *Cache) InitThresholds(role spectypes.BeaconRole) {
	c.mu.Lock()
	defer c.mu.Unlock()

	unusedCtx := context.Background()
	rt := roundtimer.New(unusedCtx, c.bn, role, nil)

	var cumulativeDuration time.Duration
	round := specqbft.Round(1)
	c.thresholds[role] = []time.Duration{}

	for {
		roundDuration := rt.RoundDuration(round)
		if roundDuration <= 0 {
			c.logger.Fatal("invalid round duration", fields.Round(round))
		}

		cumulativeDuration += roundDuration
		c.thresholds[role] = append(c.thresholds[role], cumulativeDuration)

		if cumulativeDuration > c.bn.SlotDurationSec()*time.Duration(c.bn.SlotsPerEpoch()) {
			break
		}

		round++
	}
}

// MaxPossibleRound returns max possible round for given role.
func (c *Cache) MaxPossibleRound(role spectypes.BeaconRole) specqbft.Round {
	c.mu.Lock()
	defer c.mu.Unlock()

	return specqbft.Round(len(c.thresholds[role]))
}

// EstimatedRound returns estimated round for given role and duration.
// If it is out of bounds, it returns the next round after max possible one, which is considered invalid.
func (c *Cache) EstimatedRound(role spectypes.BeaconRole, sinceSlotStart time.Duration) specqbft.Round {
	c.mu.Lock()
	thresholds := c.thresholds[role]
	c.mu.Unlock()

	for i, threshold := range thresholds {
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
