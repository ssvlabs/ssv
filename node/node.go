package node

import (
	"context"
	"strconv"
	"time"

	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/herumi/bls-eth-go-binary/bls"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/slotqueue"
)

// Options contains options to create the node
type Options struct {
	ValidatorPubKey []byte
	PrivateKey      *bls.SecretKey
	Network         core.Network
	Beacon          beacon.Beacon
	IBFT            *ibft.IBFT
	Logger          *zap.Logger
}

// Node represents the behavior of SSV node
type Node interface {
	// Start starts the SSV node
	Start(ctx context.Context) error
}

// ssvNode implements Node interface
type ssvNode struct {
	validatorPubKey []byte
	privateKey      *bls.SecretKey
	network         core.Network
	slotQueue       slotqueue.Queue
	beacon          beacon.Beacon
	iBFT            *ibft.IBFT
	logger          *zap.Logger
}

// New is the constructor of ssvNode
func New(opts Options) Node {
	return &ssvNode{
		validatorPubKey: opts.ValidatorPubKey,
		privateKey:      opts.PrivateKey,
		network:         opts.Network,
		slotQueue:       slotqueue.New(opts.Network),
		beacon:          opts.Beacon,
		iBFT:            opts.IBFT,
		logger:          opts.Logger,
	}
}

// Start implements Node interface
func (n *ssvNode) Start(ctx context.Context) error {
	go n.startSlotQueueListener()

	streamDuties, err := n.beacon.StreamDuties(ctx, n.validatorPubKey)
	if err != nil {
		n.logger.Fatal("failed to open duties stream", zap.Error(err))
	}

	n.logger.Info("start streaming duties")
	for duty := range streamDuties {
		go func(duty *ethpb.DutiesResponse_Duty) {
			slots := collectSlots(duty)
			if len(slots) == 0 {
				n.logger.Debug("no slots found for the given duty")
				return
			}

			for _, slot := range slots {
				go func(slot uint64) {
					n.logger.Info("scheduling IBFT instance start for slot",
						zap.Time("start_time", n.getSlotStartTime(slot)),
						zap.Uint64("committee_index", duty.GetCommitteeIndex()),
						zap.Uint64("slot", slot))

					if err := n.slotQueue.Schedule(n.validatorPubKey, slot, duty); err != nil {
						n.logger.Error("failed to schedule slot")
					}
				}(slot)
			}
		}(duty)
	}

	return nil
}

// startSlotQueueListener starts slot queue listener
func (n *ssvNode) startSlotQueueListener() {
	n.logger.Info("start listening slot queue")

	for {
		slot, duty, ok, err := n.slotQueue.Next(n.validatorPubKey)
		if err != nil {
			n.logger.Error("failed to get next slot data", zap.Error(err))
			continue
		}

		if !ok {
			n.logger.Debug("no duties slot scheduled")
			continue
		}

		logger := n.logger.With(zap.Time("start_time", n.getSlotStartTime(slot)),
			zap.Uint64("committee_index", duty.GetCommitteeIndex()),
			zap.Uint64("slot", slot))

		logger.Info("starting IBFT instance for slot...")

		lambda, val, err := prepareInstanceParams(slot, duty)
		if err != nil {
			logger.Error("failed to prepare params to start instance")
			continue
		}

		// Resolve role and pass a specific object
		/*inputValue := &validation.InputValue{
			Data: &validation.InputValue_AttestationData{
				AttestationData: &ethpb.AttestationData{},
			},
		}
		*/
		// TODO: Pass prev lambda
		if err := n.iBFT.StartInstance(logger, []byte{}, lambda, val); err != nil {
			logger.Error("failed to start IBFT instance", zap.Error(err))
		}
	}
}

// getSlotStartTime returns the start time for the given slot
func (n *ssvNode) getSlotStartTime(slot uint64) time.Time {
	timeSinceGenesisStart := slot * uint64(n.network.SlotDurationSec().Seconds())
	start := time.Unix(int64(n.network.MinGenesisTime()+timeSinceGenesisStart), 0)
	return start
}

// collectSlots collects slots from the given duty
func collectSlots(duty *ethpb.DutiesResponse_Duty) []uint64 {
	var slots []uint64
	slots = append(slots, duty.GetAttesterSlot())
	slots = append(slots, duty.GetProposerSlots()...)
	return slots
}

func prepareInstanceParams(slot uint64, duty *ethpb.DutiesResponse_Duty) ([]byte, []byte, error) {
	val, err := duty.Marshal()
	if err != nil {
		return nil, nil, err
	}

	return []byte(strconv.Itoa(int(slot))), val, nil
}
