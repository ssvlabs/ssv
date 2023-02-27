package goclient

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	eth2client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/http"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/eth2-key-manager/core"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/monitoring/metrics"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

const (
	healthCheckTimeout = 10 * time.Second
)

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
	for _, c := range allMetrics {
		if err := prometheus.Register(c); err != nil {
			log.Println("could not register prometheus collector")
		}
	}
}

// Client defines all go-eth2-client interfaces used in ssv
type Client interface {
	eth2client.Service

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
}

// goClient implementing Beacon struct
type goClient struct {
	ctx            context.Context
	network        beaconprotocol.Network
	client         Client
	indicesMapLock sync.Mutex
	graffiti       []byte
}

// verifies that the client implements HealthCheckAgent
var _ metrics.HealthCheckAgent = &goClient{}

// New init new client and go-client instance
func New(logger *zap.Logger, opt beaconprotocol.Options) (beaconprotocol.Beacon, error) {
	logger.Info("connecting to consensus client...", zap.String("address", opt.BeaconNodeAddr), zap.String("network", opt.Network))

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

	logger.Info("successfully connected to consensus client", zap.String("name", httpClient.Name()), zap.String("address", httpClient.Address()))

	network := beaconprotocol.NewNetwork(core.NetworkFromString(opt.Network), opt.MinGenesisTime)
	_client := &goClient{
		ctx:            opt.Context,
		network:        network,
		client:         httpClient.(*http.Service),
		indicesMapLock: sync.Mutex{},
		graffiti:       opt.Graffiti,
	}

	return _client, nil
}

// HealthCheck provides health status of beacon node
func (gc *goClient) HealthCheck() []string {
	if gc.client == nil {
		return []string{"not connected to beacon node"}
	}
	ctx, cancel := context.WithTimeout(gc.ctx, healthCheckTimeout)
	defer cancel()
	syncState, err := gc.client.NodeSyncing(ctx)
	if err != nil {
		metricsBeaconNodeStatus.Set(float64(statusUnknown))
		return []string{"could not get beacon node sync state"}
	}
	if syncState != nil && syncState.IsSyncing {
		metricsBeaconNodeStatus.Set(float64(statusSyncing))
		return []string{fmt.Sprintf("beacon node is currently syncing: head=%d, distance=%d",
			syncState.HeadSlot, syncState.SyncDistance)}
	}
	metricsBeaconNodeStatus.Set(float64(statusOK))
	return []string{}
}

// GetBeaconNetwork returns the beacon network the node is on
func (gc *goClient) GetBeaconNetwork() spectypes.BeaconNetwork {
	return spectypes.BeaconNetwork(gc.network.Network)
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
