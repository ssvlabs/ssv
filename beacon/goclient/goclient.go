package goclient

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	eth2client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/api"
	apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	eth2clienthttp "github.com/attestantio/go-eth2-client/http"
	eth2clientmulti "github.com/attestantio/go-eth2-client/multi"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"
	"tailscale.com/util/singleflight"

	"github.com/ssvlabs/ssv/logging/fields"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/slotticker"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/utils/casts"
)

const (
	// DataVersionNil is just a placeholder for a nil data version.
	// Don't check for it, check for errors or nil data instead.
	DataVersionNil spec.DataVersion = math.MaxUint64

	// Client timeouts.
	DefaultCommonTimeout = time.Second * 5  // For dialing and most requests.
	DefaultLongTimeout   = time.Second * 60 // For long requests.

	clResponseErrMsg        = "Consensus client returned an error"
	clNilResponseErrMsg     = "Consensus client returned a nil response"
	clNilResponseDataErrMsg = "Consensus client returned a nil response data"
)

// NodeClient is the type of the Beacon node.
type NodeClient string

const (
	NodeLighthouse NodeClient = "lighthouse"
	NodePrysm      NodeClient = "prysm"
	NodeNimbus     NodeClient = "nimbus"
	NodeUnknown    NodeClient = "unknown"
)

// ParseNodeClient derives the client from node's version string.
func ParseNodeClient(version string) NodeClient {
	version = strings.ToLower(version)
	switch {
	case strings.Contains(version, "lighthouse"):
		return NodeLighthouse
	case strings.Contains(version, "prysm"):
		return NodePrysm
	case strings.Contains(version, "nimbus"):
		return NodeNimbus
	default:
		return NodeUnknown
	}
}

// Client defines all go-eth2-client interfaces used in ssv
type Client interface {
	MultiClient

	eth2client.NodeVersionProvider
	eth2client.NodeClientProvider
	eth2client.BlindedProposalSubmitter
}

type MultiClient interface {
	eth2client.Service
	eth2client.SpecProvider
	eth2client.GenesisProvider

	eth2client.AttestationDataProvider
	eth2client.AttestationsSubmitter
	eth2client.AggregateAttestationProvider
	eth2client.AggregateAttestationsSubmitter
	eth2client.BeaconCommitteeSubscriptionsSubmitter
	eth2client.SyncCommitteeSubscriptionsSubmitter
	eth2client.AttesterDutiesProvider
	eth2client.ProposerDutiesProvider
	eth2client.SyncCommitteeDutiesProvider
	eth2client.NodeSyncingProvider
	eth2client.ProposalProvider
	eth2client.ProposalSubmitter
	eth2client.DomainProvider
	eth2client.SyncCommitteeMessagesSubmitter
	eth2client.BeaconBlockRootProvider
	eth2client.SyncCommitteeContributionProvider
	eth2client.SyncCommitteeContributionsSubmitter
	eth2client.ValidatorsProvider
	eth2client.ProposalPreparationsSubmitter
	eth2client.EventsProvider
	eth2client.ValidatorRegistrationsSubmitter
	eth2client.VoluntaryExitSubmitter
}

// GoClient implementing Beacon struct
type GoClient struct {
	log         *zap.Logger
	ctx         context.Context
	network     beaconprotocol.Network
	clients     []Client
	multiClient MultiClient

	genesisVersion atomic.Pointer[phase0.Version]

	syncDistanceTolerance phase0.Slot
	nodeSyncingFn         func(ctx context.Context, opts *api.NodeSyncingOpts) (*api.Response[*apiv1.SyncState], error)

	operatorDataStore operatordatastore.OperatorDataStore

	registrationMu       sync.Mutex
	registrationLastSlot phase0.Slot
	registrationCache    map[phase0.BLSPubKey]*api.VersionedSignedValidatorRegistration

	// attestationReqInflight helps prevent duplicate attestation data requests
	// from running in parallel.
	attestationReqInflight singleflight.Group[phase0.Slot, *phase0.AttestationData]

	// attestationDataCache helps reuse recently fetched attestation data.
	// AttestationData is cached by slot only, because Beacon nodes should return the same
	// data regardless of the requested committeeIndex.
	attestationDataCache *ttlcache.Cache[phase0.Slot, *phase0.AttestationData]

	commonTimeout time.Duration
	longTimeout   time.Duration
}

// New init new client and go-client instance
func New(
	logger *zap.Logger,
	opt beaconprotocol.Options,
	operatorDataStore operatordatastore.OperatorDataStore,
	slotTickerProvider slotticker.Provider,
) (*GoClient, error) {
	logger.Info("consensus client: connecting", fields.Address(opt.BeaconNodeAddr), fields.Network(string(opt.Network.BeaconNetwork)))

	commonTimeout := opt.CommonTimeout
	if commonTimeout == 0 {
		commonTimeout = DefaultCommonTimeout
	}
	longTimeout := opt.LongTimeout
	if longTimeout == 0 {
		longTimeout = DefaultLongTimeout
	}

	client := &GoClient{
		log:                   logger,
		ctx:                   opt.Context,
		network:               opt.Network,
		syncDistanceTolerance: phase0.Slot(opt.SyncDistanceTolerance),
		operatorDataStore:     operatorDataStore,
		registrationCache:     map[phase0.BLSPubKey]*api.VersionedSignedValidatorRegistration{},
		attestationDataCache: ttlcache.New(
			// we only fetch attestation data during the slot of the relevant duty (and never later),
			// hence caching it for 2 slots is sufficient
			ttlcache.WithTTL[phase0.Slot, *phase0.AttestationData](2 * opt.Network.SlotDurationSec()),
		),
		commonTimeout: commonTimeout,
		longTimeout:   longTimeout,
	}

	beaconAddrList := strings.Split(opt.BeaconNodeAddr, ";") // TODO: Decide what symbol to use as a separator. Bootnodes are currently separated by ";". Deployment bot currently uses ",".
	if len(beaconAddrList) == 0 {
		return nil, fmt.Errorf("no beacon node address provided")
	}

	for _, beaconAddr := range beaconAddrList {
		if err := client.addSingleClient(opt.Context, beaconAddr); err != nil {
			return nil, err
		}
	}

	err := client.initMultiClient(opt.Context)
	if err != nil {
		logger.Error("Consensus multi client initialization failed",
			zap.String("address", opt.BeaconNodeAddr),
			zap.Error(err),
		)

		return nil, err
	}

	client.nodeSyncingFn = client.nodeSyncing

	go client.registrationSubmitter(slotTickerProvider)
	// Start automatic expired item deletion for attestationDataCache.
	go client.attestationDataCache.Start()

	return client, nil
}

func (gc *GoClient) initMultiClient(ctx context.Context) error {
	var services []eth2client.Service
	for _, client := range gc.clients {
		services = append(services, client)
	}

	multiClient, err := eth2clientmulti.New(
		ctx,
		eth2clientmulti.WithClients(services),
		eth2clientmulti.WithLogLevel(zerolog.DebugLevel),
		eth2clientmulti.WithTimeout(gc.commonTimeout),
	)
	if err != nil {
		return fmt.Errorf("create multi client: %w", err)
	}

	gc.multiClient = multiClient.(*eth2clientmulti.Service)
	return nil
}

func (gc *GoClient) addSingleClient(ctx context.Context, addr string) error {
	httpClient, err := eth2clienthttp.New(
		ctx,
		// WithAddress supplies the address of the beacon node, in host:port format.
		eth2clienthttp.WithAddress(addr),
		// LogLevel supplies the level of logging to carry out.
		eth2clienthttp.WithLogLevel(zerolog.DebugLevel),
		eth2clienthttp.WithTimeout(gc.commonTimeout),
		eth2clienthttp.WithReducedMemoryUsage(true),
		eth2clienthttp.WithAllowDelayedStart(true),
		eth2clienthttp.WithHooks(gc.singleClientHooks()),
		eth2clienthttp.WithSyncDistanceTolerance(gc.syncDistanceTolerance),
	)
	if err != nil {
		gc.log.Error("Consensus http client initialization failed",
			zap.String("address", addr),
			zap.Error(err),
		)

		return fmt.Errorf("create http client: %w", err)
	}

	gc.clients = append(gc.clients, httpClient.(*eth2clienthttp.Service))

	return nil
}

func (gc *GoClient) singleClientHooks() *eth2clienthttp.Hooks {
	return &eth2clienthttp.Hooks{
		OnActive: func(ctx context.Context, s *eth2clienthttp.Service) {
			// If err is nil, nodeVersionResp is never nil.
			nodeVersionResp, err := s.NodeVersion(ctx, &api.NodeVersionOpts{})
			if err != nil {
				gc.log.Error(clResponseErrMsg,
					zap.String("address", s.Address()),
					zap.String("api", "NodeVersion"),
					zap.Error(err),
				)
				return
			}

			gc.log.Info("consensus client connected",
				fields.Name(s.Name()),
				fields.Address(s.Address()),
				zap.String("client", string(ParseNodeClient(nodeVersionResp.Data))),
				zap.String("version", nodeVersionResp.Data),
			)

			genesis, err := s.Genesis(ctx, &api.GenesisOpts{})
			if err != nil {
				gc.log.Error(clResponseErrMsg,
					zap.String("address", s.Address()),
					zap.String("api", "Genesis"),
					zap.Error(err),
				)
				return
			}

			if expected, err := gc.assertSameGenesisVersion(genesis.Data.GenesisForkVersion); err != nil {
				gc.log.Fatal("client returned unexpected genesis fork version, make sure all clients use the same Ethereum network",
					zap.String("address", s.Address()),
					zap.Any("client_genesis", genesis.Data.GenesisForkVersion),
					zap.Any("expected_genesis", expected),
					zap.Error(err),
				)
				return // Tests may override Fatal's behavior
			}
		},
		OnInactive: func(ctx context.Context, s *eth2clienthttp.Service) {
			gc.log.Warn("consensus client disconnected",
				fields.Name(s.Name()),
				fields.Address(s.Address()),
			)
		},
		OnSynced: func(ctx context.Context, s *eth2clienthttp.Service) {
			gc.log.Info("consensus client synced",
				fields.Name(s.Name()),
				fields.Address(s.Address()),
			)
		},
		OnDesynced: func(ctx context.Context, s *eth2clienthttp.Service) {
			gc.log.Warn("consensus client desynced",
				fields.Name(s.Name()),
				fields.Address(s.Address()),
			)
		},
	}
}

// assertSameGenesis checks if genesis is same.
// Clients may have different values returned by Spec call,
// so we decided that it's best to assert that GenesisForkVersion is the same.
// To add more assertions, we check the whole apiv1.Genesis (GenesisTime and GenesisValidatorsRoot)
// as they should be same too.
func (gc *GoClient) assertSameGenesisVersion(genesisVersion phase0.Version) (phase0.Version, error) {
	if gc.genesisVersion.CompareAndSwap(nil, &genesisVersion) {
		return genesisVersion, nil
	}

	expected := *gc.genesisVersion.Load()
	if expected != genesisVersion {
		return expected, fmt.Errorf("genesis fork version mismatch, expected %v, got %v", expected, genesisVersion)
	}

	return expected, nil
}

func (gc *GoClient) nodeSyncing(ctx context.Context, opts *api.NodeSyncingOpts) (*api.Response[*apiv1.SyncState], error) {
	return gc.multiClient.NodeSyncing(ctx, opts)
}

var errSyncing = errors.New("syncing")

// Healthy returns if beacon node is currently healthy: responds to requests, not in the syncing state, not optimistic
// (for optimistic see https://github.com/ethereum/consensus-specs/blob/dev/sync/optimistic.md#block-production).
func (gc *GoClient) Healthy(ctx context.Context) error {
	nodeSyncingResp, err := gc.nodeSyncingFn(ctx, &api.NodeSyncingOpts{})
	if err != nil {
		gc.log.Error(clResponseErrMsg,
			zap.String("api", "NodeSyncing"),
			zap.Error(err),
		)
		// TODO: get rid of global variable, pass metrics to goClient
		recordBeaconClientStatus(ctx, statusUnknown, gc.multiClient.Address())
		return fmt.Errorf("failed to obtain node syncing status: %w", err)
	}
	if nodeSyncingResp == nil {
		gc.log.Error(clNilResponseErrMsg,
			zap.String("api", "NodeSyncing"),
		)
		recordBeaconClientStatus(ctx, statusUnknown, gc.multiClient.Address())
		return fmt.Errorf("node syncing response is nil")
	}
	if nodeSyncingResp.Data == nil {
		gc.log.Error(clNilResponseDataErrMsg,
			zap.String("api", "NodeSyncing"),
		)
		recordBeaconClientStatus(ctx, statusUnknown, gc.multiClient.Address())
		return fmt.Errorf("node syncing data is nil")
	}
	syncState := nodeSyncingResp.Data
	recordBeaconClientStatus(ctx, statusSyncing, gc.multiClient.Address())
	recordSyncDistance(ctx, syncState.SyncDistance, gc.multiClient.Address())

	// TODO: also check if syncState.ElOffline when github.com/attestantio/go-eth2-client supports it
	if syncState.IsSyncing && syncState.SyncDistance > gc.syncDistanceTolerance {
		gc.log.Error("Consensus client is not synced")
		return errSyncing
	}
	if syncState.IsOptimistic {
		gc.log.Error("Consensus client is in optimistic mode")
		return fmt.Errorf("optimistic")
	}

	recordBeaconClientStatus(ctx, statusSynced, gc.multiClient.Address())

	return nil
}

// GetBeaconNetwork returns the beacon network the node is on
func (gc *GoClient) GetBeaconNetwork() spectypes.BeaconNetwork {
	return gc.network.BeaconNetwork
}

// SlotStartTime returns the start time in terms of its unix epoch
// value.
func (gc *GoClient) slotStartTime(slot phase0.Slot) time.Time {
	duration := time.Second * casts.DurationFromUint64(uint64(slot)*uint64(gc.network.SlotDurationSec().Seconds()))
	startTime := time.Unix(gc.network.MinGenesisTime(), 0).Add(duration)
	return startTime
}

func (gc *GoClient) Events(ctx context.Context, topics []string, handler eth2client.EventHandlerFunc) error {
	if err := gc.multiClient.Events(ctx, topics, handler); err != nil {
		gc.log.Error(clResponseErrMsg,
			zap.String("api", "Events"),
			zap.Error(err),
		)

		return err
	}

	return nil
}
