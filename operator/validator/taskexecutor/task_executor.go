package taskexecutor

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/multierr"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/monitoring/metricsreporter"
	"github.com/bloxapp/ssv/operator/duties"
	"github.com/bloxapp/ssv/operator/validator/validatormanager"
	"github.com/bloxapp/ssv/operator/validatorsmap"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

type Executor interface {
	StartValidator(share *ssvtypes.SSVShare) error
	StopValidator(pubKey spectypes.ValidatorPK) error
	LiquidateCluster(owner common.Address, operatorIDs []uint64, toLiquidate []*ssvtypes.SSVShare) error
	ReactivateCluster(owner common.Address, operatorIDs []uint64, toReactivate []*ssvtypes.SSVShare) error
	UpdateFeeRecipient(owner, recipient common.Address) error
	ExitValidator(pubKey phase0.BLSPubKey, blockNumber uint64, validatorIndex phase0.ValidatorIndex) error
}

type executor struct {
	logger           *zap.Logger
	metrics          metricsreporter.MetricsReporter // TODO: narrow
	validatorsMap    *validatorsmap.ValidatorsMap
	beacon           beaconprotocol.BeaconNode
	indicesChange    chan struct{}
	validatorExitCh  chan duties.ExitDescriptor
	validatorManager *validatormanager.ValidatorManager
}

func New(
	logger *zap.Logger,
	metrics metricsreporter.MetricsReporter,
	validatorsMap *validatorsmap.ValidatorsMap,
	beacon beaconprotocol.BeaconNode,
	indicesChange chan struct{},
	validatorExitCh chan duties.ExitDescriptor,
	validatorManager *validatormanager.ValidatorManager,
) Executor {
	return &executor{
		logger:           logger,
		metrics:          metrics,
		validatorsMap:    validatorsMap,
		beacon:           beacon,
		indicesChange:    indicesChange,
		validatorExitCh:  validatorExitCh,
		validatorManager: validatorManager,
	}
}

func (e *executor) taskLogger(taskName string, fields ...zap.Field) *zap.Logger {
	return e.logger.Named("TaskExecutor").
		With(zap.String("task", taskName)).
		With(fields...)
}

func (e *executor) StartValidator(share *ssvtypes.SSVShare) error {
	// logger := c.taskLogger("StartValidator", fields.PubKey(share.ValidatorPubKey))

	// Since we don't yet have the Beacon metadata for this validator,
	// we can't yet start it. Starting happens in `UpdateValidatorMetaDataIteration`,
	// so this task is currently a no-op.

	return nil
}

func (e *executor) StopValidator(pubKey spectypes.ValidatorPK) error {
	logger := e.taskLogger("StopValidator", fields.PubKey(pubKey))

	e.metrics.ValidatorRemoved(pubKey)
	e.validatorManager.RemoveValidator(pubKey)

	logger.Info("removed validator")

	return nil
}

func (e *executor) LiquidateCluster(owner common.Address, operatorIDs []spectypes.OperatorID, toLiquidate []*ssvtypes.SSVShare) error {
	logger := e.taskLogger("LiquidateCluster", fields.Owner(owner), fields.OperatorIDs(operatorIDs))

	for _, share := range toLiquidate {
		e.validatorManager.RemoveValidator(share.ValidatorPubKey)
		logger.With(fields.PubKey(share.ValidatorPubKey)).Debug("liquidated share")
	}

	return nil
}

func (e *executor) ReactivateCluster(owner common.Address, operatorIDs []spectypes.OperatorID, toReactivate []*ssvtypes.SSVShare) error {
	logger := e.taskLogger("ReactivateCluster", fields.Owner(owner), fields.OperatorIDs(operatorIDs))

	var startedValidators int
	var errs error
	for _, share := range toReactivate {
		started, err := e.validatorManager.CreateValidator(share)
		if err != nil {
			errs = multierr.Append(errs, err)
			continue
		}
		if started {
			startedValidators++
		}
	}
	if startedValidators > 0 {
		// Notify DutyScheduler about the changes in validator indices without blocking.
		go func() {
			select {
			case e.indicesChange <- struct{}{}:
			case <-time.After(12 * time.Second):
				logger.Error("failed to notify indices change")
			}
		}()
	}
	logger.Debug("reactivated cluster",
		zap.Int("cluster_validators", len(toReactivate)),
		zap.Int("started_validators", startedValidators))

	return errs
}

func (e *executor) UpdateFeeRecipient(owner, recipient common.Address) error {
	logger := e.taskLogger("UpdateFeeRecipient",
		zap.String("owner", owner.String()),
		zap.String("fee_recipient", recipient.String()))

	e.validatorsMap.ForEach(func(v *validator.Validator) bool {
		if v.Share.OwnerAddress == owner {
			v.Share.FeeRecipientAddress = recipient

			logger.Debug("updated recipient address")
		}
		return true
	})

	return nil
}

func (e *executor) ExitValidator(pubKey phase0.BLSPubKey, blockNumber uint64, validatorIndex phase0.ValidatorIndex) error {
	logger := e.taskLogger("ExitValidator",
		fields.PubKey(pubKey[:]),
		fields.BlockNumber(blockNumber),
		zap.Uint64("validator_index", uint64(validatorIndex)),
	)

	exitDesc := duties.ExitDescriptor{
		PubKey:         pubKey,
		ValidatorIndex: validatorIndex,
		BlockNumber:    blockNumber,
	}

	go func() {
		select {
		case e.validatorExitCh <- exitDesc:
			logger.Debug("added voluntary exit task to pipeline")
		case <-time.After(2 * e.beacon.GetBeaconNetwork().SlotDurationSec()):
			logger.Error("failed to schedule ExitValidator duty!")
		}
	}()

	return nil
}
