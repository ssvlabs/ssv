package goclient

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	eth2client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	eth2clienthttp "github.com/attestantio/go-eth2-client/http"
	eth2clientmulti "github.com/attestantio/go-eth2-client/multi"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
	"github.com/rs/zerolog"
	"go.uber.org/zap"
	"tailscale.com/util/singleflight"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

const (
	// DataVersionNil is just a placeholder for a nil data version.
	// Don't check for it, check for errors or nil data instead.
	DataVersionNil spec.DataVersion = math.MaxUint64

	// Client timeouts.
	DefaultCommonTimeout = time.Second * 5  // For dialing and most requests.
	DefaultLongTimeout   = time.Second * 60 // For long requests.

	BlockRootToSlotCacheCapacityEpochs = 64

	// ProposalPreparationBatchSize is the maximum number of preparations to submit in a single request
	ProposalPreparationBatchSize = 500
)

// NodeClient is the type of the Beacon node.
type NodeClient string

const (
	NodeLighthouse NodeClient = "lighthouse"
	NodePrysm      NodeClient = "prysm"
	NodeNimbus     NodeClient = "nimbus"
	NodeUnknown    NodeClient = "unknown"
)

// parseNodeClient derives the Beacon client implementation from node version string (that we get from it).
func parseNodeClient(version string) NodeClient {
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
	eth2client.BeaconBlockHeadersProvider
	eth2client.ValidatorsProvider
	eth2client.BeaconCommitteesProvider
	eth2client.ProposalPreparationsSubmitter
	eth2client.EventsProvider
	eth2client.ValidatorRegistrationsSubmitter
	eth2client.VoluntaryExitSubmitter
	eth2client.ValidatorLivenessProvider
	eth2client.ForkScheduleProvider
}

// GoClient implementing Beacon struct
type GoClient struct {
	log *zap.Logger

	beaconConfigMu   sync.RWMutex
	beaconConfig     *networkconfig.Beacon
	beaconConfigInit chan struct{}

	clients     []Client
	multiClient MultiClient

	syncDistanceTolerance phase0.Slot

	// attestationReqInflight helps prevent duplicate attestation data requests
	// from running in parallel.
	attestationReqInflight singleflight.Group[phase0.Slot, *phase0.AttestationData]
	// attestationDataCache helps reuse recently fetched attestation data.
	// AttestationData is cached by slot only, because Beacon nodes should return the same
	// data regardless of the requested committeeIndex.
	attestationDataCache               *ttlcache.Cache[phase0.Slot, *phase0.AttestationData]
	weightedAttestationDataSoftTimeout time.Duration
	weightedAttestationDataHardTimeout time.Duration

	// blockRootToSlotCache is used for attestation data scoring. When multiple Consensus clients are used,
	// the cache helps reduce the number of Consensus Client calls by `n-1`, where `n` is the number of Consensus clients
	// that successfully fetched attestation data and proceeded to the scoring phase. Capacity is rather an arbitrary number,
	// intended for cases where some objects within the application may need to fetch attestation data for more than one slot.
	blockRootToSlotCache *ttlcache.Cache[phase0.Root, phase0.Slot]

	// committeesCache caches Beacon committees by epoch to avoid repeated fetching
	committeesCache *ttlcache.Cache[phase0.Epoch, []*eth2apiv1.BeaconCommittee]

	commonTimeout time.Duration
	longTimeout   time.Duration

	withWeightedAttestationData bool
	withParallelSubmissions     bool

	subscribersLock      sync.RWMutex
	headEventSubscribers []subscriber[*eth2apiv1.HeadEvent]
	supportedTopics      []eventTopic

	// voluntaryExitDomainCached is voluntary exit domain value calculated lazily and re-used
	// since it doesn't change over time
	voluntaryExitDomainCached atomic.Pointer[phase0.Domain]

	// Provider for proposal preparations from fee recipient controller
	proposalPreparationsProviderMu sync.RWMutex
	proposalPreparationsProvider   func() ([]*eth2apiv1.ProposalPreparation, error)

	// activatedClients tracks which clients have been activated before (for reconnection detection)
	activatedClients *hashmap.Map[string, struct{}]
}

func New(ctx context.Context, logger *zap.Logger, opt Options) (*GoClient, error) {
	if opt.BeaconNodeAddr == "" {
		return nil, fmt.Errorf("no beacon node address provided")
	}

	beaconAddrList := strings.Split(opt.BeaconNodeAddr, ";")

	commonTimeout := opt.CommonTimeout
	if commonTimeout == 0 {
		commonTimeout = DefaultCommonTimeout
	}
	longTimeout := opt.LongTimeout
	if longTimeout == 0 {
		longTimeout = DefaultLongTimeout
	}

	client := &GoClient{
		log:                                logger.Named(log.NameConsensusClient),
		beaconConfigInit:                   make(chan struct{}),
		syncDistanceTolerance:              phase0.Slot(opt.SyncDistanceTolerance),
		commonTimeout:                      commonTimeout,
		longTimeout:                        longTimeout,
		withWeightedAttestationData:        opt.WithWeightedAttestationData,
		withParallelSubmissions:            opt.WithParallelSubmissions,
		weightedAttestationDataSoftTimeout: time.Duration(float64(commonTimeout) / 2.5),
		weightedAttestationDataHardTimeout: commonTimeout,
		supportedTopics:                    []eventTopic{eventTopicHead, eventTopicBlock},
		activatedClients:                   hashmap.New[string, struct{}](),
	}

	for _, beaconAddr := range beaconAddrList {
		if err := client.addSingleClient(ctx, beaconAddr); err != nil {
			return nil, err
		}
	}

	client.log.Debug("connecting")

	err := client.initMultiClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("initialize consensus client(s), address(es)=%s: %w", opt.BeaconNodeAddr, err)
	}

	client.log.Debug("connected successfully, fetching beacon config")

	initCtx, initCtxCancel := context.WithTimeout(ctx, client.longTimeout)
	defer initCtxCancel()
	select {
	case <-initCtx.Done():
		return nil, fmt.Errorf("timed out awaiting Beacon config initialization, timeout=%s: %w", client.longTimeout, initCtx.Err())
	case <-client.beaconConfigInit:
	}

	config := client.getBeaconConfig()
	if config == nil {
		return nil, fmt.Errorf("no beacon config set")
	}

	client.log.Debug("fetched beacon config successfully")

	client.blockRootToSlotCache = ttlcache.New(
		ttlcache.WithCapacity[phase0.Root, phase0.Slot](config.SlotsPerEpoch * BlockRootToSlotCacheCapacityEpochs),
	)

	client.attestationDataCache = ttlcache.New(
		// we only fetch attestation data during the slot of the relevant duty (and never later),
		// hence caching it for 2 slots is sufficient
		ttlcache.WithTTL[phase0.Slot, *phase0.AttestationData](2 * config.SlotDuration),
	)

	// Start automatic expired item deletion for attestationDataCache.
	go client.attestationDataCache.Start()

	// Initialize committees cache with TTL of ~2 epochs
	committeeTTL := config.SlotDuration * time.Duration(config.SlotsPerEpoch) * 2 //nolint:gosec
	client.committeesCache = ttlcache.New(ttlcache.WithTTL[phase0.Epoch, []*eth2apiv1.BeaconCommittee](committeeTTL))
	go client.committeesCache.Start()

	client.log.Debug("starting event listener")

	if err := client.startEventListener(ctx); err != nil {
		return nil, fmt.Errorf("failed to launch event listener: %w", err)
	}

	return client, nil
}

// SetProposalPreparationsProvider sets the callback to get current proposal preparations
func (gc *GoClient) SetProposalPreparationsProvider(provider func() ([]*eth2apiv1.ProposalPreparation, error)) {
	gc.proposalPreparationsProviderMu.Lock()
	defer gc.proposalPreparationsProviderMu.Unlock()
	gc.proposalPreparationsProvider = provider
}

// getBeaconConfig provides thread-safe access to the beacon configuration
func (gc *GoClient) getBeaconConfig() *networkconfig.Beacon {
	gc.beaconConfigMu.RLock()
	defer gc.beaconConfigMu.RUnlock()
	return gc.beaconConfig
}

func (gc *GoClient) initMultiClient(ctx context.Context) error {
	services := make([]eth2client.Service, 0, len(gc.clients))
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
			logger := gc.log.With(fields.Name(s.Name()), fields.Address(s.Address()))

			nodeVersion, err := gc.fetchNodeVersion(ctx, s)
			if err != nil {
				// Since connection can become active/inactive multiple times, we don't need to terminate
				// the SSV node here. Unlike with the "unexpected beacon config" case below, here we might
				// be able to recover automatically, so - we log an error and return for now.
				logger.Error("couldn't fetch node version", zap.Error(err))
				return
			}

			beaconConfig, err := gc.fetchBeaconConfig(ctx, s)
			if err != nil {
				// Since connection can become active/inactive multiple times, we don't need to terminate
				// the SSV node here. Unlike with the "unexpected beacon config" case below, here we might
				// be able to recover automatically, so - we log an error and return for now.
				logger.Error("couldn't fetch beacon config", zap.Error(err))
				return
			}

			currentConfig, err := gc.applyBeaconConfig(s.Address(), beaconConfig)
			if err != nil {
				logger.Fatal("client returned unexpected beacon config, make sure all clients use the same Ethereum network",
					zap.Error(err),
					zap.Any("current_forks", currentConfig.Forks),
					zap.Any("got_forks", beaconConfig.Forks),
					zap.Stringer("client_config", beaconConfig),
					zap.Stringer("expected_config", currentConfig),
				)
				return // tests may override Fatal's behavior, hence gotta return in addition to the Fatal call
			}

			dataVersion, _ := currentConfig.ForkAtEpoch(currentConfig.EstimatedCurrentEpoch())
			logger.Info("consensus client connected and retrieved beacon config",
				zap.String("client", string(parseNodeClient(nodeVersion))),
				zap.String("version", nodeVersion),
				zap.Uint64("data_version", uint64(dataVersion)),
				zap.Stringer("config", currentConfig),
			)

			// Note, this also records that client in gc.activatedClients map.
			_, isReconnection := gc.activatedClients.GetOrSet(s.Address(), struct{}{})
			if isReconnection {
				// Re-submit proposal preparations on reconnection
				go gc.handleProposalPreparationsOnReconnect(ctx, s, logger)
			}
		},
		OnInactive: func(ctx context.Context, s *eth2clienthttp.Service) {
			logger := gc.log.With(fields.Name(s.Name()), fields.Address(s.Address()))

			logger.Warn("consensus client disconnected")
		},
		OnSynced: func(ctx context.Context, s *eth2clienthttp.Service) {
			logger := gc.log.With(fields.Name(s.Name()), fields.Address(s.Address()))

			logger.Info("consensus client synced")
		},
		OnDesynced: func(ctx context.Context, s *eth2clienthttp.Service) {
			logger := gc.log.With(fields.Name(s.Name()), fields.Address(s.Address()))

			logger.Warn("consensus client de-synced")
		},
	}
}

func (gc *GoClient) applyBeaconConfig(nodeAddress string, beaconConfig *networkconfig.Beacon) (*networkconfig.Beacon, error) {
	gc.beaconConfigMu.Lock()
	defer gc.beaconConfigMu.Unlock()

	if gc.beaconConfig == nil {
		gc.beaconConfig = beaconConfig
		close(gc.beaconConfigInit)

		gc.log.Info("beacon config has been initialized",
			zap.Stringer("beacon_config", beaconConfig),
			fields.Address(nodeAddress),
		)
		return beaconConfig, nil
	}

	if err := gc.beaconConfig.AssertSame(beaconConfig); err != nil {
		return gc.beaconConfig, fmt.Errorf("beacon config misalign: %w", err)
	}

	return gc.beaconConfig, nil
}

var (
	errSyncing    = fmt.Errorf("syncing")
	errOptimistic = fmt.Errorf("optimistic")
)

// Healthy returns if the consensus client (for single-client) or at least one of consensus clients (for multi-client)
// is currently healthy.
// It's healthy if it:
// - responds to API requests
// - is already synced (including sync-distance check)
// - is not optimistic (see https://github.com/ethereum/consensus-specs/blob/dev/sync/optimistic.md#block-production)
// Note, for multi-client case this function is checking every client separately instead of simply relying on the
// corresponding multi-client `NodeSyncing` endpoint - this is because multi-client implementation does not allow
// for sync-distance to be > 1 while we do want to allow for that (in case we have multiple clients and none of them
// has sync-distance of <= 1 we still want Healthy to report success so that SSV node can survive such occasional CL
// hiccups without having to restart).
func (gc *GoClient) Healthy(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var hasHealthy atomic.Bool
	errCh := make(chan error, len(gc.clients))

	for _, client := range gc.clients {
		go func() {
			if err := gc.checkNodeHealth(ctx, client); err != nil {
				errCh <- fmt.Errorf("check node health: %w", err)
				return
			}

			if hasHealthy.CompareAndSwap(false, true) {
				cancel() // one healthy node is enough
			}
		}()
	}

	// Wait only for the result: either one success or all errors
	var errs error
	for i := 0; i < len(gc.clients); i++ {
		select {
		case <-ctx.Done():
			// If we have a healthy client, return no error
			if hasHealthy.Load() {
				return nil
			}
			// Otherwise, collect errors and keep going
		case err := <-errCh:
			errs = errors.Join(errs, err)
		}
	}

	if !hasHealthy.Load() {
		return errs
	}

	return nil
}

// checkNodeHealth checks client's healthiness by checking if it's synced.
func (gc *GoClient) checkNodeHealth(ctx context.Context, client Client) error {
	reqStart := time.Now()
	nodeSyncingResp, err := client.NodeSyncing(ctx, &api.NodeSyncingOpts{})
	recordRequest(ctx, gc.log, "NodeSyncing", client, http.MethodGet, false, time.Since(reqStart), err)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil // already found healthy nodes
		}
		recordBeaconClientStatus(ctx, statusUnknown, client.Address())
		return errSingleClient(fmt.Errorf("fetch node syncing status: %w", err), client.Address(), "NodeSyncing")
	}
	if nodeSyncingResp == nil {
		recordBeaconClientStatus(ctx, statusUnknown, client.Address())
		return errSingleClient(fmt.Errorf("node syncing response is nil"), client.Address(), "NodeSyncing")
	}
	if nodeSyncingResp.Data == nil {
		recordBeaconClientStatus(ctx, statusUnknown, client.Address())
		return errSingleClient(fmt.Errorf("node syncing response data is nil"), client.Address(), "NodeSyncing")
	}

	syncState := nodeSyncingResp.Data

	recordBeaconClientStatus(ctx, statusSyncing, client.Address())
	recordSyncDistance(ctx, syncState.SyncDistance, client.Address())

	if syncState.IsSyncing && syncState.SyncDistance > gc.syncDistanceTolerance {
		return errSyncing
	}
	if syncState.IsOptimistic {
		return errOptimistic
	}

	recordBeaconClientStatus(ctx, statusSynced, client.Address())

	return nil
}
