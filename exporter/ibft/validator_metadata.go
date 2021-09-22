package ibft

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/exporter/storage"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// MetaDataFetcherOptions defines the required parameters to create an instance
type MetaDataFetcherOptions struct {
	Logger  *zap.Logger
	Beacon  beacon.Beacon
	Storage storage.Storage
	PKs     []string
}

type metaDataFetcher struct {
	logger  *zap.Logger
	beacon  beacon.Beacon
	storage storage.Storage
	pks     []string
}

// NewMetaDataFetcher creates new instance
func NewMetaDataFetcher(opts MetaDataFetcherOptions) Reader {
	return &metaDataFetcher{
		logger:  opts.Logger,
		beacon:  opts.Beacon,
		storage: opts.Storage,
		pks:     opts.PKs,
	}
}

func (i *metaDataFetcher) Start() error {
	if len(i.pks) == 0 {
		return errors.New("invalid pubkeys. at least one validator is required")
	}

	// fetch data
	var pubkeys []phase0.BLSPubKey
	for _, pk := range i.pks {
		blsPubKey := phase0.BLSPubKey{}
		copy(blsPubKey[:], pk)
		pubkeys = append(pubkeys, blsPubKey)
	}
	i.logger.Debug("fetching indices for public keys", zap.Int("total", len(pubkeys)),
		zap.Any("pubkeys", pubkeys))
	validatorsIndexMap, err := i.beacon.GetValidatorData(pubkeys)
	if err != nil {
		return errors.Wrap(err, "failed to get validator data from Beacon")
	}

	// update validators one by one
	for _, val := range validatorsIndexMap {
		pubKey := &bls.PublicKey{}
		if err := pubKey.Deserialize(val.Validator.PublicKey[:]); err != nil {
			i.logger.Error("failed to deserialize validator public key", zap.Error(err))
			continue
		}

		valBeaconIndex := uint64(val.Index)

		toUpdate := &storage.ValidatorInformation{
			PublicKey:   pubKey.SerializeToHexStr(),
			Balance:     val.Balance,
			Status:      val.Status,
			BeaconIndex: &valBeaconIndex,
		}
		if err := i.storage.UpdateValidatorInformation(toUpdate); err != nil {
			i.logger.Error("failed to update validator info", zap.Error(err))
		}
	}
	return nil
}
