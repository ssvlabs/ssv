package beacon

import (
	"context"

	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/ethereum/go-ethereum/event"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/shared/params"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/utils/grpcex"
)

// prysmGRPC implements Beacon interface using Prysm's beacon node via gRPC
type prysmGRPC struct {
	validatorClient    ethpb.BeaconNodeValidatorClient
	beaconClient       ethpb.BeaconChainClient
	privateKey         *bls.SecretKey
	network            core.Network
	validatorPublicKey []byte
	graffiti           []byte
	blockFeed          *event.Feed
	highestValidSlot   uint64
	logger             *zap.Logger
}

// NewPrysmGRPC is the constructor of prysmGRPC
func NewPrysmGRPC(ctx context.Context, logger *zap.Logger, privateKey *bls.SecretKey, network core.Network, validatorPublicKey, graffiti []byte, addr string) (Beacon, error) {
	conn, err := grpcex.DialConn(addr)
	if err != nil {
		return nil, err
	}

	b := &prysmGRPC{
		validatorClient:    ethpb.NewBeaconNodeValidatorClient(conn),
		beaconClient:       ethpb.NewBeaconChainClient(conn),
		privateKey:         privateKey,
		network:            network,
		validatorPublicKey: validatorPublicKey,
		graffiti:           graffiti,
		blockFeed:          &event.Feed{},
		logger:             logger,
	}

	go func() {
		if err := b.receiveBlocks(ctx); err != nil {
			logger.Fatal("failed to receive blocks")
		}
	}()

	return b, nil
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

// receiveBlocks receives blocks from the beacon node. Upon receiving a block, the service
// broadcasts it to a feed for other usages to subscribe to.
func (b *prysmGRPC) receiveBlocks(ctx context.Context) error {
	stream, err := b.beaconClient.StreamBlocks(ctx, &ethpb.StreamBlocksRequest{VerifiedOnly: true})
	if err != nil {
		return errors.Wrap(err, "failed to open stream blocks")
	}

	for {
		if ctx.Err() == context.Canceled {
			return errors.New("context canceled - shutting down blocks receiver")
		}

		res, err := stream.Recv()
		if err != nil {
			return errors.Wrap(err, "failed to receive blocks from the node")
		}

		if res == nil || res.Block == nil {
			continue
		}

		if res.Block.Slot > b.highestValidSlot {
			b.highestValidSlot = res.Block.Slot
		}

		b.blockFeed.Send(res)
	}
}

// domainData returns domain data for the given epoch and domain
func (b *prysmGRPC) domainData(ctx context.Context, slot uint64, domain []byte) (*ethpb.DomainResponse, error) {
	req := &ethpb.DomainRequest{
		Epoch:  b.network.EstimatedEpochAtSlot(slot),
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
	domain, err := b.domainData(ctx, slot, params.BeaconConfig().DomainSelectionProof[:])
	if err != nil {
		return nil, err
	}

	root, err := helpers.ComputeSigningRoot(slot, domain.SignatureDomain)
	if err != nil {
		return nil, err
	}

	return b.privateKey.SignByte(root[:]).Serialize(), nil
}
