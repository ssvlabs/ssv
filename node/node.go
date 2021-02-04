package node

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/herumi/bls-eth-go-binary/bls"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/val/validation"
	"github.com/bloxapp/ssv/ibft/val/weekday"
	"github.com/bloxapp/ssv/slotqueue"
)

// Options contains options to create the node
type Options struct {
	ValidatorPubKey []byte
	PrivateKey      *bls.SecretKey
	Network         core.Network
	Consensus       string
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
	consensus       string
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
		consensus:       opts.Consensus,
		slotQueue:       slotqueue.New(opts.Network),
		beacon:          opts.Beacon,
		iBFT:            opts.IBFT,
		logger:          opts.Logger,
	}
}

// Start implements Node interface
func (n *ssvNode) Start(ctx context.Context) error {
	go n.startSlotQueueListener(ctx)

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
func (n *ssvNode) startSlotQueueListener(ctx context.Context) {
	n.logger.Info("start listening slot queue")

	identfier := ibft.FirstInstanceIdentifier
	for {
		slot, duty, ok, err := n.slotQueue.Next(n.validatorPubKey)
		if err != nil {
			n.logger.Error("failed to get next slot data", zap.Error(err))
			continue
		}

		if !ok {
			n.logger.Debug("no duties for slot scheduled")
			continue
		}

		go func(slot uint64, duty *ethpb.DutiesResponse_Duty) {
			logger := n.logger.With(zap.Time("start_time", n.getSlotStartTime(slot)),
				zap.Uint64("committee_index", duty.GetCommitteeIndex()),
				zap.Uint64("slot", slot))

			roles, err := n.beacon.RolesAt(ctx, slot, duty)
			if err != nil {
				logger.Error("failed to get roles for duty", zap.Error(err))
				return
			}

			for _, role := range roles {
				go func(role beacon.Role) {
					logger := logger.With(zap.String("role", role.String()))
					logger.Info("starting IBFT instance...")

					inputValue := &validation.InputValue{
						Duty: duty,
					}

					switch role {
					case beacon.RoleAttester:
						attData, err := n.beacon.GetAttestationData(ctx, slot, duty.GetCommitteeIndex())
						if err != nil {
							logger.Error("failed to get attestation data", zap.Error(err))
							return
						}

						inputValue.Data = &validation.InputValue_AttestationData{
							AttestationData: attData,
						}
					case beacon.RoleAggregator:
						aggData, err := n.beacon.GetAggregationData(ctx, slot, duty.GetCommitteeIndex())
						if err != nil {
							logger.Error("failed to get aggregation data", zap.Error(err))
							return
						}

						inputValue.Data = &validation.InputValue_AggregationData{
							AggregationData: aggData,
						}
					case beacon.RoleProposer:
						block, err := n.beacon.GetProposalData(ctx, slot)
						if err != nil {
							logger.Error("failed to get proposal block", zap.Error(err))
							return
						}

						inputValue.Data = &validation.InputValue_BeaconBlock{
							BeaconBlock: block,
						}
					case beacon.RoleUnknown:
						logger.Warn("unknown role")
						return
					}

					valBytes, err := json.Marshal(&inputValue)
					if err != nil {
						logger.Error("failed to marshal input value", zap.Error(err))
						return
					}

					newId := strconv.Itoa(int(slot))

					// TODO: Refactor this out
					consensus := validation.New(logger, inputValue)
					if n.consensus == "weekday" {
						consensus = weekday.New()
						valBytes = []byte(time.Now().Weekday().String())
					}

					decided := n.iBFT.StartInstance(ibft.StartOptions{
						Logger:       logger,
						Consensus:    consensus,
						PrevInstance: []byte(identfier),
						Identifier:   []byte(newId),
						Value:        valBytes,
					})

					if !decided {
						logger.Warn("not decided")
						return
					}

					// identfier = newId // TODO: Fix race condition

					// Here we ensure at least 2/3 instances got a val so we can sign data and broadcast signatures
					logger.Info("------------------ GOT CONSENSUS -----------------")
				}(role)
			}
		}(slot, duty)
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
