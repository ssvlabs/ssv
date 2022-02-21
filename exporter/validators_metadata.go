package exporter

import (
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/exporter/ibft"
	"github.com/bloxapp/ssv/validator"
	validatorstorage "github.com/bloxapp/ssv/validator/storage"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
)

func (exp *exporter) continuouslyUpdateValidatorMetaData() {
	for {
		time.Sleep(exp.validatorMetaDataUpdateInterval)

		shares, err := exp.validatorStorage.GetAllValidatorsShare()
		if err != nil {
			exp.logger.Error("could not get validators shares for metadata update", zap.Error(err))
			continue
		}

		exp.updateValidatorsMetadata(shares, metaDataBatchSize)
	}
}

func (exp *exporter) warmupValidatorsMetaData() error {
	shares, err := exp.validatorStorage.GetAllValidatorsShare()
	if err != nil {
		exp.logger.Error("could not get validators shares for metadata update", zap.Error(err))
		return err
	}
	//// reporting on warmup to fill statuses of validators w/o metadata
	for _, share := range shares {
		validator.ReportValidatorStatus(share.PublicKey.SerializeToHexStr(), share.Metadata, exp.logger)
	}
	exp.updateValidatorsMetadata(shares, metaDataBatchSize)
	return err
}

func (exp *exporter) updateValidatorsMetadata(shares []*validatorstorage.Share, batchSize int) {
	var pks [][]byte
	for _, share := range shares {
		pks = append(pks, share.PublicKey.Serialize())
	}
	onUpdated := func(pk string, meta *beacon.ValidatorMetadata) {
		logger := exp.logger.With(zap.String("pk", pk))
		validator.ReportValidatorStatus(pk, meta, exp.logger)
		pubKey := bls.PublicKey{}
		if err := pubKey.DeserializeHexStr(pk); err != nil {
			logger.Error("could not desrialize public key", zap.Error(err))
			return
		}
		if started, err := exp.triggerValidator(&pubKey); err != nil {
			logger.Error("could not setup validator share")
		} else if !started {
			logger.Debug("validator didn't started")
		}
	}
	beacon.UpdateValidatorsMetadataBatch(pks, exp.metaDataReadersQueue, exp, exp.beacon, onUpdated, batchSize)
}

// UpdateValidatorMetadata updates all relevant components with the updated metadata
func (exp *exporter) UpdateValidatorMetadata(pk string, metadata *beacon.ValidatorMetadata) error {
	if metadata == nil {
		return errors.New("could not update empty metadata")
	}
	if err := exp.validatorStorage.(beacon.ValidatorMetadataStorage).UpdateValidatorMetadata(pk, metadata); err != nil {
		return errors.Wrap(err, "failed to update share")
	}
	if decidedReader := exp.getDecidedReader(pk); decidedReader != nil {
		share := decidedReader.(ibft.ShareHolder).Share()
		share.HasMetadata()
		if !share.HasMetadata() {
			share.Metadata = metadata
			exp.logger.Debug("metadata was created", zap.String("pk", pk), zap.Any("metadata", *metadata))
		} else if !share.Metadata.Equals(metadata) {
			share.Metadata.Status = metadata.Status
			share.Metadata.Balance = metadata.Balance
			exp.logger.Debug("metadata was updated", zap.String("pk", pk), zap.Any("metadata", *metadata))
		}
	}
	return nil
}
