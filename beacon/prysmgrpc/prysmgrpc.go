package prysmgrpc

import (
	"context"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon"
	"github.com/ethereum/go-ethereum/event"
	ptypes "github.com/gogo/protobuf/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/shared/params"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/status"
	"time"

	"github.com/bloxapp/ssv/utils/grpcex"
)

// prysmGRPC implements Beacon interface using Prysm's beacon node via gRPC
type prysmGRPC struct {
	conn               *grpc.ClientConn
	validatorClient    ethpb.BeaconNodeValidatorClient
	beaconClient       ethpb.BeaconChainClient
	nodeClient         ethpb.NodeClient
	network            core.Network
	graffiti           []byte
	blockFeed          *event.Feed
	highestValidSlot   uint64
	logger             *zap.Logger
}

// reconnectPeriod is the frequency that we try to restart our
// streams when the beacon chain is node does not respond.
var reconnectPeriod = 5 * time.Second

// New is the constructor of prysmGRPC
func New(ctx context.Context, logger *zap.Logger, network core.Network, graffiti []byte, addr string) (beacon.Beacon, error) {
	conn, err := grpcex.DialConn(addr)
	if err != nil {
		return nil, err
	}

	b := &prysmGRPC{
		conn:               conn,
		validatorClient:    ethpb.NewBeaconNodeValidatorClient(conn),
		beaconClient:       ethpb.NewBeaconChainClient(conn),
		nodeClient:         ethpb.NewNodeClient(conn),
		network:            network,
		graffiti:           graffiti,
		blockFeed:          &event.Feed{},
		logger:             logger,
	}

	go func() {
		logger.Info("start receiving blocks")
		if err := b.receiveBlocks(ctx); err != nil {
			logger.Error("failed to receive blocks", zap.Error(err))
		}
	}()

	return b, nil
}

// StreamDuties implements Beacon interface
func (b *prysmGRPC) StreamDuties(ctx context.Context, pubKeys [][]byte) (<-chan *ethpb.DutiesResponse_Duty, error) {
	streamDuties, err := b.validatorClient.StreamDuties(ctx, &ethpb.DutiesRequest{
		PublicKeys: pubKeys,
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
				time.Sleep(reconnectPeriod)
				b.logger.Info("trying to reconnect duties stream")
				streamDuties, err = b.validatorClient.StreamDuties(ctx, &ethpb.DutiesRequest{
					PublicKeys: pubKeys,
				})
				if err != nil {
					b.logger.Error("failed to reconnect duties stream", zap.Error(err))
				}
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
func (b *prysmGRPC) RolesAt(ctx context.Context, slot uint64, duty *ethpb.DutiesResponse_Duty, pubkey *bls.PublicKey, shareKey *bls.SecretKey) ([]beacon.Role, error) {
	if duty == nil {
		return nil, nil
	}

	var roles []beacon.Role
	if len(duty.ProposerSlots) > 0 {
		for _, proposerSlot := range duty.ProposerSlots {
			if proposerSlot != 0 && proposerSlot == slot {
				roles = append(roles, beacon.RoleProposer)
				break
			}
		}
	}
	if duty.AttesterSlot == slot {
		roles = append(roles, beacon.RoleAttester)

		aggregator, err := b.isAggregator(ctx, slot, len(duty.Committee), shareKey)
		if err != nil {
			return nil, errors.Wrap(err, "could not check if a validator is an aggregator")
		}

		if aggregator {
			roles = append(roles, beacon.RoleAggregator)
		}
	}

	if len(roles) == 0 {
		roles = append(roles, beacon.RoleUnknown)
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
			if e, ok := status.FromError(err); ok {
				switch e.Code() {
				case codes.Canceled, codes.Internal, codes.Unavailable:
					b.logger.Info("Trying to restart connection.", zap.Any("rpc status", e.Code()))
					err = b.restartBeaconConnection(ctx)
					if err != nil {
						return errors.Wrap(err, "Could not restart beacon connection")
					}
					stream, err = b.beaconClient.StreamBlocks(ctx, &ethpb.StreamBlocksRequest{VerifiedOnly: true} /* Prefers unverified block to catch slashing */)
					if err != nil {
						return errors.Wrap(err, "Could not restart block stream")
					}
					b.logger.Info("Block stream restarted...")
					continue // force loop again to recv
				default:
					return errors.WithMessagef(err, "Could not receive block from beacon node. rpc status - %v", e.Code())
				}
			}
			return errors.Wrap(err, "failed to receive blocks from the node")
		}

		if res == nil || res.Block == nil {
			b.logger.Debug("empty block resp")
			continue
		}

		if res.Block.Slot > b.highestValidSlot {
			b.highestValidSlot = res.Block.Slot
		}

		//b.logger.Debug("Received block from beacon node", TODO need to set trace level
		//	zap.Any("slot", res.Block.Slot),
		//	zap.Any("proposer_index", res.Block.ProposerIndex))
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

func (b *prysmGRPC) signSlot(ctx context.Context, slot uint64, privateKey *bls.SecretKey) ([]byte, error) {
	domain, err := b.domainData(ctx, slot, params.BeaconConfig().DomainSelectionProof[:])
	if err != nil {
		return nil, err
	}

	root, err := helpers.ComputeSigningRoot(slot, domain.SignatureDomain)
	if err != nil {
		return nil, err
	}

	return privateKey.SignByte(root[:]).Serialize(), nil
}

func (b *prysmGRPC) restartBeaconConnection(ctx context.Context) error {
	ticker := time.NewTicker(reconnectPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if b.conn.GetState() == connectivity.TransientFailure || b.conn.GetState() == connectivity.Idle {
				b.logger.Debug("Connection status", zap.Any("status", b.conn.GetState()))
				b.logger.Info("Beacon node is still down")
				continue
			}
			s, err := b.nodeClient.GetSyncStatus(ctx, &ptypes.Empty{})
			if err != nil {
				b.logger.Error("Could not fetch sync status", zap.Error(err))
				continue
			}
			if s == nil || s.Syncing {
				b.logger.Info("Waiting for beacon node to be fully synced...")
				continue
			}
			b.logger.Info("Beacon node is fully synced")
			return nil
		case <-ctx.Done():
			b.logger.Debug("Context closed, exiting reconnect routine")
			return errors.New("context closed, no longer attempting to restart stream")
		}
	}
}
