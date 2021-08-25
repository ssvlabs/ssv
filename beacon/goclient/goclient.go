package goclient

import (
	"context"
	"fmt"
	client "github.com/attestantio/go-eth2-client"
	eth2client "github.com/attestantio/go-eth2-client"
	api "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/auto"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/metrics"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prysmaticlabs/prysm/shared/slotutil"
	"github.com/prysmaticlabs/prysm/shared/timeutils"
	"github.com/rs/zerolog"
	"go.uber.org/zap"
	"log"
	"time"
)

const (
	healthCheckTimeout = 10 * time.Second
)

type beaconNodeStatus int32

var (
	metricsBeaconNodeStatus = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:validator:beacon_node_status",
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
	ctx      context.Context
	logger   *zap.Logger
	network  core.Network
	client   client.Service
	graffiti []byte
}

// verifies that the client implements HealthCheckAgent
var _ metrics.HealthCheckAgent = &goClient{}

// New init new client and go-client instance
func New(opt beacon.Options) (beacon.Beacon, error) {
	logger := opt.Logger.With(zap.String("component", "goClient"), zap.String("network", opt.Network))
	logger.Info("connecting to beacon client...")
	autoClient, err := auto.New(opt.Context,
		// WithAddress supplies the address of the beacon node, in host:port format.
		auto.WithAddress(opt.BeaconNodeAddr),
		// LogLevel supplies the level of logging to carry out.
		auto.WithLogLevel(zerolog.DebugLevel),
		auto.WithTimeout(time.Second*5),
	)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to create auto client")
	}

	logger = logger.With(zap.String("name", autoClient.Name()), zap.String("address", autoClient.Address()))
	logger.Info("successfully connected to beacon client")

	_client := &goClient{
		ctx:      opt.Context,
		logger:   logger,
		network:  core.NetworkFromString(opt.Network),
		client:   autoClient,
		graffiti: []byte("BloxStaking"),
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
			return []string{fmt.Sprintf("eth1 node is currently syncing: head=%d, distance=%d",
				syncState.HeadSlot, syncState.SyncDistance)}
		}
	}
	metricsBeaconNodeStatus.Set(float64(statusOK))
	return []string{}
}

func (gc *goClient) ExtendIndexMap(index spec.ValidatorIndex, pubKey spec.BLSPubKey) {
	gc.client.ExtendIndexMap(map[spec.ValidatorIndex]spec.BLSPubKey{index: pubKey})
}

func (gc *goClient) GetDuties(epoch spec.Epoch, validatorIndices []spec.ValidatorIndex) ([]*beacon.Duty, error) {
	if provider, isProvider := gc.client.(eth2client.AttesterDutiesProvider); isProvider {
		attesterDuties, err := provider.AttesterDuties(gc.ctx, epoch, validatorIndices)
		if err != nil {
			return nil, err
		}
		var duties []*beacon.Duty
		for _, attesterDuty := range attesterDuties {
			duties = append(duties, &beacon.Duty{
				Type:                    beacon.RoleTypeAttester,
				PubKey:                  attesterDuty.PubKey,
				Slot:                    attesterDuty.Slot,
				ValidatorIndex:          attesterDuty.ValidatorIndex,
				CommitteeIndex:          attesterDuty.CommitteeIndex,
				CommitteeLength:         attesterDuty.CommitteeLength,
				CommitteesAtSlot:        attesterDuty.CommitteesAtSlot,
				ValidatorCommitteeIndex: attesterDuty.ValidatorCommitteeIndex,
			})
		}
		return duties, nil
	}
	return nil, errors.New("client does not support AttesterDutiesProvider")
}

func (gc *goClient) GetIndices(validatorPubKeys []spec.BLSPubKey) (map[spec.ValidatorIndex]*api.Validator, error) {
	if provider, isProvider := gc.client.(eth2client.ValidatorsProvider); isProvider {
		validatorsMap, err := provider.ValidatorsByPubKey(gc.ctx, "head", validatorPubKeys) // TODO maybe need to get the chainId (head) as var
		if err != nil {
			return nil, err
		}
		return validatorsMap, nil
	}
	return nil, errors.New("client does not support ValidatorsProvider")
}

// waitOneThirdOrValidBlock waits until one-third of the slot has transpired (SECONDS_PER_SLOT / 3 seconds after the start of slot)
func (gc *goClient) waitOneThirdOrValidBlock(slot uint64) {
	delay := slotutil.DivideSlotBy(3 /* a third of the slot duration */)
	startTime := gc.slotStartTime(slot)
	finalTime := startTime.Add(delay)
	wait := timeutils.Until(finalTime)
	if wait <= 0 {
		return
	}

	t := time.NewTimer(wait)
	defer t.Stop()
	for range t.C {
		return
	}
}

// SlotStartTime returns the start time in terms of its unix epoch
// value.
func (gc *goClient) slotStartTime(slot uint64) time.Time {
	duration := time.Second * time.Duration(slot*uint64(gc.network.SlotDurationSec().Seconds()))
	startTime := time.Unix(int64(gc.network.MinGenesisTime()), 0).Add(duration)
	return startTime
}
