package beacon

import (
	"context"

	"github.com/pkg/errors"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/utils/grpcex"
)

// prysmGRPC implements Beacon interface using Prysm's beacon node via gRPC
type prysmGRPC struct {
	validatorClient ethpb.BeaconNodeValidatorClient
	logger          *zap.Logger
}

// NewPrysmGRPC is the constructor of prysmGRPC
func NewPrysmGRPC(logger *zap.Logger, addr string) (Beacon, error) {
	conn, err := grpcex.DialConn(addr)
	if err != nil {
		return nil, err
	}

	return &prysmGRPC{
		validatorClient: ethpb.NewBeaconNodeValidatorClient(conn),
		logger:          logger,
	}, nil
}

// StreamDuties implements Beacon interface
func (b *prysmGRPC) StreamDuties(ctx context.Context, pubKey []byte) (<-chan *ethpb.DutiesResponse_Duty, error) {
	streamDuties, err := b.validatorClient.StreamDuties(ctx, &ethpb.DutiesRequest{
		PublicKeys: [][]byte{pubKey},
	})
	if err != nil {
		return nil, err
	}

	dutiesChan := make(chan *ethpb.DutiesResponse_Duty)

	go func() {
		defer close(dutiesChan)

		for {
			resp, err := streamDuties.Recv()
			if err != nil {
				b.logger.Error("failed to receive duties from stream", zap.Error(err))
				continue
			}

			duties := resp.GetCurrentEpochDuties()
			if len(duties) == 0 {
				b.logger.Debug("no duties in the response")
				continue
			}

			for _, duty := range duties {
				dutiesChan <- duty
			}
		}
	}()

	return dutiesChan, nil
}

// domainData returns domain data for the given epoch and domain
func (b *prysmGRPC) domainData(ctx context.Context, epoch uint64, domain []byte) (*ethpb.DomainResponse, error) {
	req := &ethpb.DomainRequest{
		Epoch:  epoch,
		Domain: domain,
	}

	// TODO: Try to get data from cache

	res, err := b.validatorClient.DomainData(ctx, req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get domain data")
	}

	// TODO: Cache data

	return res, nil
}
