package ibft

import (
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/exporter/storage"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// MetaDataFetcherOptions defines the required parameters to create an instance
type MetaDataFetcherOptions struct {
	Logger              *zap.Logger
	Beacon              beacon.Beacon
	Storage             storage.Storage
	ReportUpdatedStatus func(pk string, meta *beacon.ValidatorMetadata)
	PKs                 [][]byte
}

type metaDataFetcher struct {
	logger              *zap.Logger
	beacon              beacon.Beacon
	storage             storage.Storage
	reportUpdatedStatus func(pk string, meta *beacon.ValidatorMetadata)
	pks                 [][]byte
}

// NewMetaDataFetcher creates new instance
func NewMetaDataFetcher(opts MetaDataFetcherOptions) Reader {
	return &metaDataFetcher{
		logger:              opts.Logger,
		beacon:              opts.Beacon,
		storage:             opts.Storage,
		reportUpdatedStatus: opts.ReportUpdatedStatus,
		pks:                 opts.PKs,
	}
}

func (i *metaDataFetcher) Start() error {
	if len(i.pks) == 0 {
		return errors.New("invalid pubkeys. at least one validator is required")
	}

	i.logger.Debug("fetching validator metadata for public keys", zap.Int("total", len(i.pks)))
	validatorsIndexMap, err := beacon.FetchValidatorsMetadata(i.beacon, i.pks)
	if err != nil {
		return errors.Wrap(err, "failed to get validator data from Beacon")
	}

	// update validators one by one
	for pk, meta := range validatorsIndexMap {
		if err := i.storage.UpdateValidatorMetadata(pk, meta); err != nil {
			i.logger.Error("failed to update validator info", zap.Error(err))
		}
		i.reportUpdatedStatus(pk, meta)
	}
	return nil
}
