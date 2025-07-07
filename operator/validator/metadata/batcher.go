package metadata

import (
	"context"
	"math"
	"math/big"
	"sync/atomic"
	"time"

	"github.com/ssvlabs/ssv/logging"
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
	streamInterval  time.Duration
	logger          *zap.Logger

	batchSize atomic.Uint32
}

func newBatcher(
	shareStorage shareStorage,
	validatorStore selfValidatorStore,
	fixedSubnets commons.Subnets,
	refreshInterval time.Duration,
	streamInterval time.Duration,
	logger *zap.Logger) *batcher {
	return &batcher{
		shareStorage:    shareStorage,
		validatorStore:  validatorStore,
		fixedSubnets:    fixedSubnets,
		refreshInterval: refreshInterval,
		streamInterval:  streamInterval,

		logger: logger.Named(logging.NameShareMetadataBatcher),
	}
}

func (b *batcher) size() uint32 {
	return b.batchSize.Load()
}

func (b *batcher) launch(ctx context.Context) {
	const evaluationInterval = time.Minute
	b.logger.Info("launching batcher", zap.Duration("evaluation_interval", evaluationInterval))

	b.updateBatchSize()

	b.logger.Info("initial batch size was set", zap.Uint32("size", b.batchSize.Load()))

	for {
		select {
		case <-ctx.Done():
			b.logger.Debug("context is Done. Returning...")
			return
		case <-time.After(evaluationInterval):
			b.updateBatchSize()
		}
	}
}

func (b *batcher) updateBatchSize() {
	b.logger.Info("batch size evaluation started. Fetching total number of validators")
	totalValidators := b.totalValidators()

	b.logger.Info("total number of validators was fetched. Calculating the batch size", zap.Uint32("count", totalValidators))
	ticks := int(b.refreshInterval / b.streamInterval)
	if ticks == 0 {
		ticks = 1
	}

	batchSize := uint32(math.Ceil(float64(totalValidators) / float64(ticks)))

	b.logger.Info("new batch size was calculated.", zap.Uint32("size", batchSize))
	b.batchSize.Store(batchSize)
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
