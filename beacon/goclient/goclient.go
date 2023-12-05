package goclient

import (
	"context"
	"math"
	"strings"
	"sync"
	"time"

	eth2client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/http"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/operator/slotticker"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

// DataVersionNil is just a placeholder for a nil data version.
// Don't check for it, check for errors or nil data instead.
const DataVersionNil spec.DataVersion = math.MaxUint64

type beaconNodeStatus int32

var (
	allMetrics = []prometheus.Collector{
		metricsBeaconNodeStatus,
		metricsBeaconDataRequest,
	}
	metricsBeaconNodeStatus = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv_beacon_status",
		Help: "Status of the connected beacon node",
	})

	// metricsBeaconDataRequest is located here to avoid including waiting for 1/3 or 2/3 of slot time into request duration.
	metricsBeaconDataRequest = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_beacon_data_request_duration_seconds",
		Help:    "Beacon data request duration (seconds)",
		Buckets: []float64{0.02, 0.05, 0.1, 0.2, 0.5, 1, 5},
	}, []string{"role"})

	metricsAttesterDataRequest                  = metricsBeaconDataRequest.WithLabelValues(spectypes.BNRoleAttester.String())
	metricsAggregatorDataRequest                = metricsBeaconDataRequest.WithLabelValues(spectypes.BNRoleAggregator.String())
	metricsProposerDataRequest                  = metricsBeaconDataRequest.WithLabelValues(spectypes.BNRoleProposer.String())
	metricsSyncCommitteeDataRequest             = metricsBeaconDataRequest.WithLabelValues(spectypes.BNRoleSyncCommittee.String())
	metricsSyncCommitteeContributionDataRequest = metricsBeaconDataRequest.WithLabelValues(spectypes.BNRoleSyncCommitteeContribution.String())

	statusUnknown beaconNodeStatus = 0
	statusSyncing beaconNodeStatus = 1
	statusOK      beaconNodeStatus = 2
)

func init() {
	logger := zap.L()
	for _, c := range allMetrics {
		if err := prometheus.Register(c); err != nil {
			logger.Debug("could not register prometheus collector")
		}
	}
}

// NodeClient is the type of the Beacon node.
type NodeClient string

const (
	NodeLighthouse NodeClient = "lighthouse"
	NodePrysm      NodeClient = "prysm"
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
	default:
		return NodeUnknown
	}
}

// Client defines all go-eth2-client interfaces used in ssv
type Client interface {
	eth2client.Service
	eth2client.NodeVersionProvider
	eth2client.NodeClientProvider

	eth2client.AttestationDataProvider
	eth2client.AggregateAttestationProvider
	eth2client.AggregateAttestationsSubmitter
	eth2client.AttestationDataProvider
	eth2client.AttestationsSubmitter
	eth2client.BeaconCommitteeSubscriptionsSubmitter
	eth2client.SyncCommitteeSubscriptionsSubmitter
	eth2client.AttesterDutiesProvider
	eth2client.ProposerDutiesProvider
	eth2client.SyncCommitteeDutiesProvider
	eth2client.NodeSyncingProvider
	eth2client.BeaconBlockProposalProvider
	eth2client.BeaconBlockSubmitter
	eth2client.BlindedBeaconBlockProposalProvider
	eth2client.BlindedBeaconBlockSubmitter
	eth2client.DomainProvider
	eth2client.BeaconBlockRootProvider
	eth2client.SyncCommitteeMessagesSubmitter
	eth2client.BeaconBlockRootProvider
	eth2client.SyncCommitteeContributionProvider
	eth2client.SyncCommitteeContributionsSubmitter
	eth2client.ValidatorsProvider
	eth2client.ProposalPreparationsSubmitter
	eth2client.EventsProvider
	eth2client.BlindedBeaconBlockProposalProvider
	eth2client.BlindedBeaconBlockSubmitter
	eth2client.ValidatorRegistrationsSubmitter
	eth2client.VoluntaryExitSubmitter
}

type NodeClientProvider interface {
	NodeClient() NodeClient
}

var _ NodeClientProvider = (*goClient)(nil)

// goClient implementing Beacon struct
type goClient struct {
	log                  *zap.Logger
	ctx                  context.Context
	network              beaconprotocol.Network
	client               Client
	nodeVersion          string
	nodeClient           NodeClient
	graffiti             []byte
	gasLimit             uint64
	operatorID           spectypes.OperatorID
	registrationMu       sync.Mutex
	registrationLastSlot phase0.Slot
	registrationCache    map[phase0.BLSPubKey]*api.VersionedSignedValidatorRegistration
}

// New init new client and go-client instance
func New(logger *zap.Logger, opt beaconprotocol.Options, operatorID spectypes.OperatorID, slotTickerProvider slotticker.Provider) (beaconprotocol.BeaconNode, error) {
	logger.Info("consensus client: connecting", fields.Address(opt.BeaconNodeAddr), fields.Network(string(opt.Network.BeaconNetwork)))

	httpClient, err := http.New(opt.Context,
		// WithAddress supplies the address of the beacon node, in host:port format.
		http.WithAddress(opt.BeaconNodeAddr),
		// LogLevel supplies the level of logging to carry out.
		http.WithLogLevel(zerolog.DebugLevel),
		http.WithTimeout(time.Second*5),
	)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to create http client")
	}

	client := &goClient{
		log:               logger,
		ctx:               opt.Context,
		network:           opt.Network,
		client:            httpClient.(*http.Service),
		graffiti:          opt.Graffiti,
		gasLimit:          opt.GasLimit,
		operatorID:        operatorID,
		registrationCache: map[phase0.BLSPubKey]*api.VersionedSignedValidatorRegistration{},
	}

	// Get the node's version and client.
	client.nodeVersion, err = client.client.NodeVersion(opt.Context)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get node version")
	}
	client.nodeClient = ParseNodeClient(client.nodeVersion)

	logger.Info("consensus client connected",
		fields.Name(httpClient.Name()),
		fields.Address(httpClient.Address()),
		zap.String("client", string(client.nodeClient)),
		zap.String("version", client.nodeVersion),
	)

	// Start registration submitter.
	go client.registrationSubmitter(slotTickerProvider)

	return client, nil
}

func (gc *goClient) NodeClient() NodeClient {
	return gc.nodeClient
}

// Healthy returns if beacon node is currently healthy: responds to requests, not in the syncing state, not optimistic
// (for optimistic see https://github.com/ethereum/consensus-specs/blob/dev/sync/optimistic.md#block-production).
func (gc *goClient) Healthy(ctx context.Context) error {
	syncState, err := gc.client.NodeSyncing(ctx)
	if err != nil {
		// TODO: get rid of global variable, pass metrics to goClient
		metricsBeaconNodeStatus.Set(float64(statusUnknown))
		return err
	}

	// TODO: also check if syncState.ElOffline when github.com/attestantio/go-eth2-client supports it
	metricsBeaconNodeStatus.Set(float64(statusSyncing))
	if syncState == nil {
		return errors.New("sync state is nil")
	}
	if syncState.IsSyncing {
		return errors.New("syncing")
	}
	if syncState.IsOptimistic {
		return errors.New("optimistic")
	}

	metricsBeaconNodeStatus.Set(float64(statusOK))
	return nil
}

// GetBeaconNetwork returns the beacon network the node is on
func (gc *goClient) GetBeaconNetwork() spectypes.BeaconNetwork {
	return gc.network.BeaconNetwork
}

// SlotStartTime returns the start time in terms of its unix epoch
// value.
func (gc *goClient) slotStartTime(slot phase0.Slot) time.Time {
	duration := time.Second * time.Duration(uint64(slot)*uint64(gc.network.SlotDurationSec().Seconds()))
	startTime := time.Unix(int64(gc.network.MinGenesisTime()), 0).Add(duration)
	return startTime
}

func (gc *goClient) Events(ctx context.Context, topics []string, handler eth2client.EventHandlerFunc) error {
	return gc.client.Events(ctx, topics, handler)
}
