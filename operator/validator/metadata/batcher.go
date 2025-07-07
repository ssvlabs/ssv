package metadata

import (
	"context"
	"math/big"
	"sync/atomic"
	"time"

	commons "github.com/ssvlabs/ssv/network/commons"
	types "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/registry/storage"
	"go.uber.org/zap"
)

type batcher struct {
	shareStorage    shareStorage
	validatorStore  selfValidatorStore
	fixedSubnets    commons.Subnets
	refreshInterval time.Duration
	logger          *zap.Logger

	batchSize atomic.Uint32
}

func newBatcher(
	shareStorage shareStorage,
	validatorStore selfValidatorStore,
	fixedSubnets commons.Subnets,
	refreshInterval time.Duration,
	logger *zap.Logger) *batcher {
	return &batcher{
		shareStorage:    shareStorage,
		validatorStore:  validatorStore,
		fixedSubnets:    fixedSubnets,
		refreshInterval: refreshInterval,
		logger:          logger,
	}
}

func (b *batcher) size() uint32 {
	return b.batchSize.Load()
}

func (b *batcher) launch(ctx context.Context) {
	b.logger.Info("launching batcher")
	for {
		select {
		case <-ctx.Done():
			b.logger.Debug("context is Done. Returning...")
			return
		case <-time.After(time.Minute * 15):
			totalValidators := b.totalValidators()
			itemsPerSecond := float64(totalValidators) * 0.05 //5%

			intervalSeconds := b.refreshInterval.Seconds()

			batchSize := int(itemsPerSecond * intervalSeconds)

			if batchSize == 0 && totalValidators > 0 {
				batchSize = 1
			}

			b.batchSize.Store(1)
		}
	}
}

func (b *batcher) totalValidators() uint32 {
	totalSubnets := b.totalSubnets()

	nonLiquidatedShares := big.NewInt(0)
	totalValidators := b.shareStorage.List(nil, storage.ByNotLiquidated(), func(share *types.SSVShare) bool {
		commons.SetCommitteeSubnet(nonLiquidatedShares, share.CommitteeID())
		subnet := nonLiquidatedShares.Uint64()
		return totalSubnets.IsSet(subnet)
	})

	return uint32(len(totalValidators))
}

func (s *batcher) totalSubnets() commons.Subnets {
	var (
		committeeSubnets = big.NewInt(0)
		totalSubnets     = s.fixedSubnets
	)

	myValidators := s.validatorStore.SelfValidators()
	for _, v := range myValidators {
		commons.SetCommitteeSubnet(committeeSubnets, v.CommitteeID())
		totalSubnets.Set(committeeSubnets.Uint64())
	}

	return totalSubnets
}
