package exporter

import (
	"fmt"
	validatorstorage "github.com/bloxapp/ssv/validator/storage"
	"go.uber.org/zap"
	"math"
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
	exp.updateValidatorsMetadata(shares, 100)
	return err
}

func (exp *exporter) updateValidatorsMetadata(shares []*validatorstorage.Share, batchSize int) {
	batches := int(math.Ceil(float64(len(shares)) / float64(batchSize)))
	start := 0
	end := batchSize

	for i := 0; i <= batches; i++ {
		// collect pks
		batch := make([][]byte, 0)
		for j := start; j < end; j++ {
			share := shares[j]
			batch = append(batch, share.PublicKey.Serialize())
		}
		// run task
		exp.metaDataReadersQueue.QueueDistinct(exp.updateMetadataTask(batch), fmt.Sprintf("batch_%d", i))

		// reset start and end
		start = end
		end = int(math.Min(float64(len(shares)), float64(start + batchSize)))
	}
}