package goclient

import (
	"context"
	client "github.com/attestantio/go-eth2-client"
	eth2client "github.com/attestantio/go-eth2-client"
	api "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/auto"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/shared/slotutil"
	"github.com/prysmaticlabs/prysm/shared/timeutils"
	"github.com/rs/zerolog"
	"go.uber.org/zap"
	"time"
)

// goClient implementing Beacon struct
type goClient struct {
	ctx      context.Context
	logger   *zap.Logger
	network  core.Network
	client   client.Service
	graffiti []byte

	blockChannel     chan spec.SignedBeaconBlock
	highestValidSlot uint64
}

// New init new client and go-client instance
func New(opt beacon.Options) (beacon.Beacon, error) {
	logger := opt.Logger.With(zap.String("component", "go-client"), zap.String("network", opt.Network))
	logger.Info("connecting to client...")
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
	logger.Info("successfully connected to client")

	_client := &goClient{
		ctx:      opt.Context,
		logger:   logger,
		network:  core.NetworkFromString(opt.Network),
		client:   autoClient,
		graffiti: []byte("BloxStaking"),
	}

	return _client, nil
}

func (gc *goClient) ExtendIndexMap(index spec.ValidatorIndex, pubKey spec.BLSPubKey) {
	gc.client.ExtendIndexMap(map[spec.ValidatorIndex]spec.BLSPubKey{index: pubKey})
}

// StartReceivingBlocks receives blocks from the beacon node. Upon receiving a block, the service
// broadcasts it to a observers.
func (gc *goClient) StartReceivingBlocks() {
	defer func() {
		close(gc.blockChannel)
	}()
	if provider, isProvider := gc.client.(eth2client.SignedBeaconBlockProvider); isProvider {
		for {
			signedBeaconBlock, err := provider.SignedBeaconBlock(gc.ctx, "head")
			if err != nil {
				gc.logger.Error("failed to fetch head block", zap.Error(err))
				time.Sleep(time.Second * 2)
				continue
			}
			gc.highestValidSlot = uint64(signedBeaconBlock.Message.Slot)
			gc.blockChannel <- *signedBeaconBlock
			time.Sleep(time.Second * 1)
		}
	}
	gc.logger.Error("client does not support SignedBeaconBlockProvider")
}

func (gc *goClient) GetDuties(epoch spec.Epoch, validatorIndices []spec.ValidatorIndex) ([]*api.AttesterDuty, error) {
	if provider, isProvider := gc.client.(eth2client.AttesterDutiesProvider); isProvider {
		duties, err := provider.AttesterDuties(gc.ctx, epoch, validatorIndices)
		if err != nil {
			return nil, err
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

// waitOneThirdOrValidBlock waits until (a) or (b) whichever comes first:
//   (a) the validator has received a valid block that is the same slot as input slot
//   (b) one-third of the slot has transpired (SECONDS_PER_SLOT / 3 seconds after the start of slot)
func (gc *goClient) waitOneThirdOrValidBlock(slot uint64) {
	// Don't need to wait if requested slot is the same as highest valid slot.
	if slot <= gc.highestValidSlot {
		return
	}

	delay := slotutil.DivideSlotBy(3 /* a third of the slot duration */)
	startTime := gc.slotStartTime(slot)
	finalTime := startTime.Add(delay)
	wait := timeutils.Until(finalTime)
	if wait <= 0 {
		return
	}

	t := time.NewTimer(wait)
	defer t.Stop()

	for {
		select {
		case b := <-gc.blockChannel:
			if slot <= uint64(b.Message.Slot) { // TODO need to make sure its not impacting attr.rate
				return
			}
		case <-t.C:
			return
		}
	}
}

// SlotStartTime returns the start time in terms of its unix epoch
// value.
func (gc *goClient) slotStartTime(slot uint64) time.Time {
	duration := time.Second * time.Duration(slot*uint64(gc.network.SlotDurationSec().Seconds()))
	startTime := time.Unix(int64(gc.network.MinGenesisTime()), 0).Add(duration)
	return startTime
}
