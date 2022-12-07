package goclient

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	client "github.com/attestantio/go-eth2-client"
	eth2client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/http"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv-spec/ssv"
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
	metricsBeaconNodeStatus = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:beacon:node_status",
		Help: "Status of the connected beacon node",
	})
	statusUnknown beaconNodeStatus = 0
	statusSyncing beaconNodeStatus = 1
	statusOK      beaconNodeStatus = 2
)

func init() {
	if err := prometheus.Register(metricsBeaconNodeStatus); err != nil {
		log.Println("could not register prometheus collector")
	}
}

// goClient implementing Beacon struct
type goClient struct {
	ctx            context.Context
	logger         *zap.Logger
	network        beaconprotocol.Network
	client         client.Service
	indicesMapLock sync.Mutex
	graffiti       []byte
}

// verifies that the client implements HealthCheckAgent
var _ metrics.HealthCheckAgent = &goClient{}

// New init new client and go-client instance
func New(opt beaconprotocol.Options) (ssv.BeaconNode, error) { //TODo change to spec interface
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

	network := beaconprotocol.NewNetwork(core.NetworkFromString(opt.Network))
	_client := &goClient{
		ctx:            opt.Context,
		logger:         logger,
		network:        network,
		client:         httpClient,
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
	if provider, isProvider := gc.client.(eth2client.NodeSyncingProvider); isProvider {
		ctx, cancel := context.WithTimeout(gc.ctx, healthCheckTimeout)
		defer cancel()
		syncState, err := provider.NodeSyncing(ctx)
		if err != nil {
			metricsBeaconNodeStatus.Set(float64(statusUnknown))
			return []string{"could not get beacon node sync state"}
		}
		if syncState != nil && syncState.IsSyncing {
			metricsBeaconNodeStatus.Set(float64(statusSyncing))
			return []string{fmt.Sprintf("beacon node is currently syncing: head=%d, distance=%d",
				syncState.HeadSlot, syncState.SyncDistance)}
		}
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
