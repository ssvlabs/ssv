package exporter

import (
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/exporter/storage"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// UpdateValidatorsMetadata updates validator information for the given public keys
func UpdateValidatorsMetadata(pubKeys [][]byte, collection storage.ValidatorsCollection, bc beacon.Beacon, logger *zap.Logger) error {
	results, err := beacon.FetchValidatorsData(bc, pubKeys)
	if err != nil {
		return errors.Wrap(err, "failed to get validator data from Beacon")
	}
	logger.Debug("got validators metadata", zap.Int("pks count", len(pubKeys)),
		zap.Int("results count", len(results)))
	var errs []error
	// update validators one by one
	for _, val := range results {
		pubKey := &bls.PublicKey{}
		if err := pubKey.Deserialize(val.Validator.PublicKey[:]); err != nil {
			logger.Error("failed to deserialize validator public key", zap.Error(err))
			errs = append(errs, err)
			continue
		}
		index := uint64(val.Index)
		toUpdate := &storage.ValidatorInformation{
			PublicKey:   pubKey.SerializeToHexStr(),
			Balance:     val.Balance,
			Status:      val.Status,
			BeaconIndex: &index,
		}
		if err := collection.UpdateValidatorInformation(toUpdate); err != nil {
			logger.Error("failed to update validator info",
				zap.String("pk", pubKey.SerializeToHexStr()), zap.Error(err))
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Errorf("could not process %d validators returned from beacon", len(errs))
	}
	return nil
}
