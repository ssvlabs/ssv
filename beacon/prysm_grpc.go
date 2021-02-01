package beacon

import (
	"context"

	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/shared/params"

	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/utils/grpcex"
)

// prysmGRPC implements Beacon interface using Prysm's beacon node via gRPC
type prysmGRPC struct {
	validatorClient    ethpb.BeaconNodeValidatorClient
	privateKey         *bls.SecretKey
	network            core.Network
	validatorPublicKey []byte
	logger             *zap.Logger
}

// NewPrysmGRPC is the constructor of prysmGRPC
func NewPrysmGRPC(logger *zap.Logger, privateKey *bls.SecretKey, network core.Network, validatorPublicKey []byte, addr string) (Beacon, error) {
	conn, err := grpcex.DialConn(addr)
	if err != nil {
		return nil, err
	}

	return &prysmGRPC{
		validatorClient:    ethpb.NewBeaconNodeValidatorClient(conn),
		privateKey:         privateKey,
		network:            network,
		validatorPublicKey: validatorPublicKey,
		logger:             logger,
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

// RolesAt slot returns the validator roles at the given slot. Returns nil if the
// validator is known to not have a roles at the at slot. Returns UNKNOWN if the
// validator assignments are unknown. Otherwise returns a valid validatorRole map.
func (b *prysmGRPC) RolesAt(ctx context.Context, slot uint64, duty *ethpb.DutiesResponse_Duty) ([]Role, error) {
	if duty == nil {
		return nil, nil
	}

	var roles []Role

	if len(duty.ProposerSlots) > 0 {
		for _, proposerSlot := range duty.ProposerSlots {
			if proposerSlot != 0 && proposerSlot == slot {
				roles = append(roles, RoleProposer)
				break
			}
		}
	}
	if duty.AttesterSlot == slot {
		roles = append(roles, RoleAttester)

		aggregator, err := b.isAggregator(ctx, slot, len(duty.Committee))
		if err != nil {
			return nil, errors.Wrap(err, "could not check if a validator is an aggregator")
		}

		if aggregator {
			roles = append(roles, RoleAggregator)
		}
	}

	if len(roles) == 0 {
		roles = append(roles, RoleUnknown)
	}

	return roles, nil
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

func (b *prysmGRPC) signSlot(ctx context.Context, slot uint64) ([]byte, error) {
	domain, err := b.domainData(ctx, b.network.EstimatedEpochAtSlot(slot), params.BeaconConfig().DomainSelectionProof[:])
	if err != nil {
		return nil, err
	}

	root, err := helpers.ComputeSigningRoot(slot, domain.SignatureDomain)
	if err != nil {
		return nil, err
	}

	return b.privateKey.SignByte(root[:]).Serialize(), nil
}
