package goclient

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	eth2client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/http"
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
		metricsAttestationDataRequest,
	}
	metricsBeaconNodeStatus = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv_beacon_status",
		Help: "Status of the connected beacon node",
	})
	metricsAttestationDataRequest = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_beacon_attestation_data_request_duration_seconds",
		Help:    "Attestation data request duration (seconds)",
		Buckets: []float64{0.02, 0.05, 0.1, 0.2, 0.5, 1, 5},
	}, []string{})
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
}

// goClient implementing Beacon struct
type goClient struct {
	ctx            context.Context
	logger         *zap.Logger
	network        beaconprotocol.Network
	client         Client
	indicesMapLock sync.Mutex
	graffiti       []byte
}

// verifies that the client implements HealthCheckAgent
var _ metrics.HealthCheckAgent = &goClient{}

// New init new client and go-client instance
func New(opt beaconprotocol.Options) (beaconprotocol.Beacon, error) {
	logger := opt.Logger.With(zap.String("component", "goClient"), zap.String("network", opt.Network))
	logger.Info("connecting to beacon client...")

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

	logger = logger.With(zap.String("name", httpClient.Name()), zap.String("address", httpClient.Address()))
	logger.Info("successfully connected to beacon client")

	network := beaconprotocol.NewNetwork(core.NetworkFromString(opt.Network), opt.MinGenesisTime)
	_client := &goClient{
		ctx:            opt.Context,
		logger:         logger,
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
func (gc *goClient) slotStartTime(slot uint64) time.Time {
	duration := time.Second * time.Duration(slot*uint64(gc.network.SlotDurationSec().Seconds()))
	startTime := time.Unix(int64(gc.network.MinGenesisTime()), 0).Add(duration)
	return startTime
}
