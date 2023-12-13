package slashinginterceptor

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"

	beaconproxy "github.com/bloxapp/ssv/e2e/beacon_proxy"
)

const startEndEpochsDiff = 2

type ProposerSlashingTest struct {
	Name      string
	Slashable bool
	Apply     func(*spec.VersionedBeaconBlock) error
}

type AttesterSlashingTest struct {
	Name      string
	Slashable bool
	Apply     func(*phase0.AttestationData) error
}

type validatorState struct {
	validator *v1.Validator

	proposerTest ProposerSlashingTest
	attesterTest AttesterSlashingTest

	// StartEpoch
	firstAttesterDuty         map[beaconproxy.Gateway]*v1.AttesterDuty
	firstAttestationData      map[beaconproxy.Gateway]*phase0.AttestationData
	firstSubmittedAttestation map[beaconproxy.Gateway]*phase0.Attestation

	// EndEpoch
	secondAttesterDuty         map[beaconproxy.Gateway]*v1.AttesterDuty
	secondAttestationData      map[beaconproxy.Gateway]*phase0.AttestationData
	secondSubmittedAttestation map[beaconproxy.Gateway]*phase0.Attestation

	// StartEpoch
	firstProposerDuty   map[beaconproxy.Gateway]*v1.ProposerDuty
	firstBlock          map[beaconproxy.Gateway]*spec.VersionedBeaconBlock
	firstSubmittedBlock map[beaconproxy.Gateway]*spec.VersionedSignedBeaconBlock

	// EndEpoch
	secondProposerDuty   map[beaconproxy.Gateway]*v1.ProposerDuty
	secondBlock          map[beaconproxy.Gateway]*spec.VersionedBeaconBlock
	secondSubmittedBlock map[beaconproxy.Gateway]*spec.VersionedSignedBeaconBlock
}

type SlashingInterceptor struct {
	logger             *zap.Logger
	network            beacon.Network
	startEpoch         phase0.Epoch
	sleepEpoch         phase0.Epoch
	endEpoch           phase0.Epoch
	validators         map[phase0.ValidatorIndex]*validatorState
	fakeProposerDuties bool
	mu                 sync.RWMutex
}

func New(
	logger *zap.Logger,
	network beacon.Network,
	startEpoch phase0.Epoch,
	fakeProposerDuties bool,
	validators []*v1.Validator,
) *SlashingInterceptor {
	s := &SlashingInterceptor{
		logger:     logger,
		network:    network,
		startEpoch: startEpoch,
		sleepEpoch: startEpoch + 1,
		// sleepEpoch:         math.MaxUint64, // TODO: replace with startEpoch + 1 after debugging is done
		endEpoch:           startEpoch + 2,
		fakeProposerDuties: fakeProposerDuties,
		validators:         make(map[phase0.ValidatorIndex]*validatorState),
	}

	if len(s.validators) > int(network.SlotsPerEpoch()) {
		panic(">32 validators not supported yet")
	}

	logger.Debug("creating slashing interceptor",
		zap.Any("start_epoch", s.startEpoch),
		zap.Any("end_epoch", s.endEpoch),
		zap.Any("sleep_epoch", s.sleepEpoch),
	)
	first := true
	for _, validator := range validators {
		proposerTest := ProposerSlashingTests[1]
		attesterTest := AttesterSlashingTests[3]
		if first {
			proposerTest = ProposerSlashingTests[0]
			attesterTest = AttesterSlashingTests[0]
			first = false
		}
		s.validators[validator.Index] = &validatorState{
			validator:                  validator,
			proposerTest:               proposerTest, // TODO: extract from validators.json
			attesterTest:               attesterTest, // TODO: extract from validators.json
			firstAttesterDuty:          make(map[beaconproxy.Gateway]*v1.AttesterDuty),
			firstAttestationData:       make(map[beaconproxy.Gateway]*phase0.AttestationData),
			firstSubmittedAttestation:  make(map[beaconproxy.Gateway]*phase0.Attestation),
			secondAttesterDuty:         make(map[beaconproxy.Gateway]*v1.AttesterDuty),
			secondAttestationData:      make(map[beaconproxy.Gateway]*phase0.AttestationData),
			secondSubmittedAttestation: make(map[beaconproxy.Gateway]*phase0.Attestation),
			firstProposerDuty:          make(map[beaconproxy.Gateway]*v1.ProposerDuty),
			firstBlock:                 make(map[beaconproxy.Gateway]*spec.VersionedBeaconBlock),
			firstSubmittedBlock:        make(map[beaconproxy.Gateway]*spec.VersionedSignedBeaconBlock),
			secondProposerDuty:         make(map[beaconproxy.Gateway]*v1.ProposerDuty),
			secondBlock:                make(map[beaconproxy.Gateway]*spec.VersionedBeaconBlock),
			secondSubmittedBlock:       make(map[beaconproxy.Gateway]*spec.VersionedSignedBeaconBlock),
		}
		logger.Debug("set up validator",
			zap.Uint64("validator_index", uint64(validator.Index)),
			zap.String("pubkey", validator.Validator.PublicKey.String()),
			zap.String("attester_test", attesterTest.Name),
			zap.Bool("attester_slashable", attesterTest.Slashable),
			zap.String("proposer_test", proposerTest.Name),
			zap.Bool("proposer_slashable", proposerTest.Slashable),
		)
	}
	return s
}

func (s *SlashingInterceptor) WatchSubmissions() {
	endOfStartEpoch := s.network.EpochStartTime(s.startEpoch + 1)

	time.AfterFunc(time.Until(endOfStartEpoch), func() {
		s.checkStartEpochAttestationSubmission()
	})

	s.logger.Info("scheduled start epoch submission check", zap.Any("at", endOfStartEpoch))

	endOfEndEpoch := s.network.EpochStartTime(s.endEpoch + 1)

	time.AfterFunc(time.Until(endOfEndEpoch), func() {
		s.checkEndEpochAttestationSubmission()
	})

	s.logger.Info("scheduled end epoch submission check", zap.Any("at", endOfEndEpoch))
}

func (s *SlashingInterceptor) checkStartEpochAttestationSubmission() {
	submittedCount := 0
	for _, state := range s.validators {
		// TODO: support values other than 4
		if len(state.firstSubmittedAttestation) != 4 {
			s.logger.Debug("validator did not submit in start epoch",
				zap.Any("validator_index", state.validator.Index),
				zap.Any("validator_pk", state.validator.Validator.PublicKey.String()),
				zap.Any("submitters", gatewayNames(maps.Keys(state.firstSubmittedAttestation))),
			)
		} else {
			submittedCount++
			s.logger.Debug("validator submitted in start epoch",
				zap.Any("validator_index", state.validator.Index),
				zap.Any("validator_pk", state.validator.Validator.PublicKey.String()),
				zap.Any("submitters", gatewayNames(maps.Keys(state.firstSubmittedAttestation))),
			)
		}
	}

	if submittedCount == len(s.validators) {
		s.logger.Info("all attestations submitted in start epoch", zap.Any("count", submittedCount))
	} else {
		s.logger.Warn("not all attestations submitted in start epoch", zap.Any("submitted", submittedCount), zap.Any("expected", len(s.validators)))
	}
}

func (s *SlashingInterceptor) checkEndEpochAttestationSubmission() {
	submittedCount := 0
	hasSlashable := false
	for _, state := range s.validators {
		if state.attesterTest.Slashable {
			if len(state.secondSubmittedAttestation) != 0 {
				hasSlashable = true
				s.logger.Error("found slashable validator",
					zap.Any("validator_index", state.validator.Index),
					zap.Any("validator_pk", state.validator.Validator.PublicKey.String()),
					zap.Any("submitters", gatewayNames(maps.Keys(state.secondSubmittedAttestation))),
					zap.Any("first_attestation", state.firstSubmittedAttestation),
					zap.Any("second_attestation", state.secondSubmittedAttestation),
					zap.Any("test", state.attesterTest.Name),
				)
			}
		} else if len(state.secondSubmittedAttestation) != 4 { // TODO: support values other than 4
			s.logger.Debug("validator did not submit in end epoch",
				zap.Any("validator_index", state.validator.Index),
				zap.Any("validator_pk", state.validator.Validator.PublicKey.String()),
				zap.Any("submitters", gatewayNames(maps.Keys(state.secondSubmittedAttestation))),
				zap.Any("test", state.attesterTest.Name),
			)
		} else {
			submittedCount++
			s.logger.Debug("validator submitted in end epoch",
				zap.Any("validator_index", state.validator.Index),
				zap.Any("validator_pk", state.validator.Validator.PublicKey.String()),
				zap.Any("submitters", gatewayNames(maps.Keys(state.secondSubmittedAttestation))),
				zap.Any("test", state.attesterTest.Name),
			)
		}
	}

	if hasSlashable {
		s.logger.Error("found slashable validators")
	} else if submittedCount == len(s.validators) {
		s.logger.Info("all attestations submitted in end epoch", zap.Any("count", submittedCount))
	} else {
		s.logger.Info("not all attestations submitted in end epoch", zap.Any("submitted", submittedCount), zap.Any("expected", len(s.validators)))
	}

	s.logger.Info("End epoch finished")
	// TODO: rewrite logs above so that we check two conditions:
	// 1. All non-slashable validators submitted in end epoch
	// 2. All slashable validators did not submit in end epoch
	// Then have a summary log if test passes or not. Two cases above may be logged but don't have to (e.g. debugging)
}

// ATTESTER

func (s *SlashingInterceptor) InterceptAttesterDuties(
	ctx context.Context,
	epoch phase0.Epoch,
	indices []phase0.ValidatorIndex,
	duties []*v1.AttesterDuty,
) ([]*v1.AttesterDuty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, gateway := s.requestContext(ctx)

	if s.blockedEpoch(epoch) {
		return []*v1.AttesterDuty{}, nil
	}

	if len(duties) != len(s.validators) {
		return nil, fmt.Errorf("unexpected duty count")
	}

	// Sort duties for deterministic response.
	sort.Slice(duties, func(i, j int) bool {
		return duties[i].ValidatorIndex < duties[j].ValidatorIndex
	})

	for i, duty := range duties {
		state, ok := s.validators[duty.ValidatorIndex]
		if !ok {
			return nil, fmt.Errorf("validator not found")
		}

		duty.Slot = s.network.FirstSlotAtEpoch(epoch) + phase0.Slot(i)
		duty.CommitteeIndex = phase0.CommitteeIndex(i)

		s.logger.Debug("validator got attester duty",
			zap.Any("epoch", epoch), zap.Any("slot", duty.Slot), zap.Any("gateway", gateway.Name), zap.Any("committee_index", duty.CommitteeIndex), zap.Any("validator", duty.ValidatorIndex))

		if _, ok = state.firstAttesterDuty[gateway]; !ok {
			if epoch != s.startEpoch {
				return nil, fmt.Errorf("misbehavior: first attester duty wasn't requested during the start epoch")
			}
			state.firstAttesterDuty[gateway] = duty

			continue
		}

		if _, ok = state.secondAttesterDuty[gateway]; ok {
			return nil, fmt.Errorf("second attester duties already requested")
		}
		if epoch != s.endEpoch {
			return nil, fmt.Errorf("misbehavior: second attester duty wasn't requested during the end epoch")
		}
		state.secondAttesterDuty[gateway] = duty
	}

	return duties, nil
}

func (s *SlashingInterceptor) InterceptAttestationData(
	ctx context.Context,
	slot phase0.Slot,
	committeeIndex phase0.CommitteeIndex,
	data *phase0.AttestationData,
) (*phase0.AttestationData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, gateway := s.requestContext(ctx)

	epoch := s.network.EstimatedEpochAtSlot(slot)
	if s.blockedEpoch(epoch) {
		return nil, fmt.Errorf("attestation data requested for blocked epoch %d", epoch)
	}

	for validatorIndex, state := range s.validators {
		// Skip validators that are not in the requested committee.
		if firstDuty, ok := state.firstAttesterDuty[gateway]; ok && firstDuty.Slot == slot && firstDuty.CommitteeIndex == committeeIndex {
			s.logger.Debug("got first attestation data request", zap.Any("epoch", epoch), zap.Any("gateway", gateway.Name), zap.Any("slot", slot), zap.Any("validator", validatorIndex))

			if epoch != s.startEpoch {
				return nil, fmt.Errorf("misbehavior: first attester data doesn't belong in the start epoch")
			}

			// Have we created the first attestation data for another gateway already?
			// If so, record the same attestation data for this gateway and return it.
			for gw, existingData := range state.firstAttestationData {
				if gw == gateway {
					s.logger.Warn("first attestation data already requested") //NOTE: aggregator might request attestation data again
					return existingData, nil
				}
				state.firstAttestationData[gateway] = existingData
				return existingData, nil
			}

			// Record the first attestation data on this gateway.
			state.firstAttestationData[gateway] = data
			return data, nil
		}

		if secondDuty, ok := state.secondAttesterDuty[gateway]; ok && secondDuty.Slot == slot && secondDuty.CommitteeIndex == committeeIndex {
			s.logger.Debug("got second attestation data request", zap.Any("epoch", epoch), zap.Any("gateway", gateway.Name), zap.Any("slot", slot), zap.Any("validator", validatorIndex))

			if epoch != s.endEpoch {
				return nil, fmt.Errorf("misbehavior: second attester data doesn't belong in the end epoch")
			}

			// Have we created the second attestation data for another gateway already?
			// If so, record the same attestation data for this gateway and return it.
			for gw, existingData := range state.secondAttestationData {
				if gw == gateway {
					s.logger.Warn("second attestation data already requested") // NOTE: aggregator might request attestation data again
					return existingData, nil
				}
				state.secondAttestationData[gateway] = existingData
				return existingData, nil
			}

			if _, ok := state.firstAttestationData[gateway]; !ok {
				return nil, fmt.Errorf("unexpected state: received second attestation but first attestation doesn't exist")
			}

			modifiedData := &phase0.AttestationData{
				Slot:            slot,
				Index:           committeeIndex,
				BeaconBlockRoot: state.firstAttestationData[gateway].BeaconBlockRoot,
				Source: &phase0.Checkpoint{
					Epoch: state.firstAttestationData[gateway].Source.Epoch,
					Root:  state.firstAttestationData[gateway].Source.Root,
				},
				Target: &phase0.Checkpoint{
					Epoch: state.firstAttestationData[gateway].Target.Epoch,
					Root:  state.firstAttestationData[gateway].Target.Root,
				},
			}

			// Apply the test on the first attestation data.
			s.logger.Debug("Applying test for validator", zap.String("test", state.attesterTest.Name), zap.Uint64("validator", uint64(state.validator.Index)))
			if err := state.attesterTest.Apply(modifiedData); err != nil {
				return nil, fmt.Errorf("failed to apply attester slashing test: %w", err)
			}

			// Record the second attestation data on this gateway.
			state.secondAttestationData[gateway] = modifiedData
			return modifiedData, nil
		}

	}
	return data, nil
}

func (s *SlashingInterceptor) InterceptSubmitAttestations(
	ctx context.Context,
	attestations []*phase0.Attestation,
) ([]*phase0.Attestation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	logger, gateway := s.requestContext(ctx)

	for _, attestation := range attestations {
		slot := attestation.Data.Slot
		epoch := s.network.EstimatedEpochAtSlot(slot)
		logger := logger.With(
			zap.Any("epoch", epoch),
			zap.Any("slot", slot),
			zap.Any("source", attestation.Data.Source.Epoch),
			zap.Any("target", attestation.Data.Target.Epoch),
		)
		logger.Debug("submit attestation request")

		if s.blockedEpoch(epoch) {
			return nil, fmt.Errorf("attestation submitted for blocked epoch %d", epoch)
		}

		for validatorIndex, state := range s.validators {
			logger := logger.With(zap.Any("validator", validatorIndex))

			// Skip validators that are not in the requested committee.
			if firstDuty, ok := state.firstAttesterDuty[gateway]; ok && firstDuty.Slot == slot && firstDuty.CommitteeIndex == attestation.Data.Index {
				// Record the submitted attestation.
				if _, ok := state.firstSubmittedAttestation[gateway]; ok {
					return nil, fmt.Errorf("first attestation already submitted")
				}

				logger.Debug("got first attestation submission")

				if epoch != s.startEpoch {
					return nil, fmt.Errorf("misbehavior: attestation wasn't submitted during the start epoch")
				}
				state.firstSubmittedAttestation[gateway] = attestation

				logger.Debug("submitted first attestation")

				continue
			}

			if secondDuty, ok := state.secondAttesterDuty[gateway]; ok && secondDuty.Slot == slot && secondDuty.CommitteeIndex == attestation.Data.Index {
				logger.Debug("got second attestation submission")

				// Record the second submitted attestation.
				if _, ok := state.secondSubmittedAttestation[gateway]; ok {
					return nil, fmt.Errorf("second attestation already submitted")
				}

				if epoch != s.endEpoch {
					return nil, fmt.Errorf("misbehavior: attestation wasn't submitted during the end epoch")
				}
				state.secondSubmittedAttestation[gateway] = attestation

				logger.Debug("submitted second attestation")

				if state.attesterTest.Slashable {
					return nil, fmt.Errorf("misbehavior: slashable attestation was submitted during the end epoch")
				}

				continue
			}
		}
	}

	return attestations, nil
}

// PROPOSER

func (s *SlashingInterceptor) InterceptProposerDuties(
	ctx context.Context,
	epoch phase0.Epoch,
	indices []phase0.ValidatorIndex,
	duties []*v1.ProposerDuty,
) ([]*v1.ProposerDuty, error) {
	return []*v1.ProposerDuty{}, nil

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.blockedEpoch(epoch) {
		return []*v1.ProposerDuty{}, nil
	}

	if !s.fakeProposerDuties {
		return duties, nil
	}

	logger, gateway := s.requestContext(ctx)

	// If no indices are provided, it means that the client wants duties for all validators.
	if len(indices) == 0 {
		for _, state := range s.validators {
			indices = append(indices, state.validator.Index)
		}
	}

	// Sort indices for deterministic response.
	sort.Slice(indices, func(i, j int) bool {
		return indices[i] < indices[j]
	})

	// Fake a proposer duty for each validator.
	duties = make([]*v1.ProposerDuty, 0, len(indices))
	for i, index := range indices {
		state, ok := s.validators[index]
		if !ok {
			return nil, fmt.Errorf("validator not found: %d", index)
		}
		duty := &v1.ProposerDuty{
			ValidatorIndex: index,
			PubKey:         state.validator.Validator.PublicKey,
			Slot:           s.network.FirstSlotAtEpoch(epoch) + phase0.Slot(i),
		}
		logger := logger.With(
			zap.Any("epoch", epoch),
			zap.Any("slot", duty.Slot),
			zap.Any("validator", duty.ValidatorIndex),
		)
		if _, ok := state.firstProposerDuty[gateway]; !ok {
			if epoch != s.startEpoch {
				return nil, fmt.Errorf("misbehavior: first proposer duty wasn't requested during the start epoch")
			}
			state.firstProposerDuty[gateway] = duty
			logger.Info("validator got first proposer duty")
		} else if _, ok := state.secondProposerDuty[gateway]; !ok {
			if epoch != s.endEpoch {
				return nil, fmt.Errorf("misbehavior: second proposer duty wasn't requested during the end epoch")
			}
			state.secondProposerDuty[gateway] = duty
			logger.Info("validator got second proposer duty")
		} else {
			return nil, fmt.Errorf("second proposer duties already requested")
		}
		duties = append(duties, duty)
	}

	return duties, nil
}

func (s *SlashingInterceptor) InterceptBlockProposal(
	ctx context.Context,
	slot phase0.Slot,
	randaoReveal phase0.BLSSignature,
	graffiti [32]byte,
	block *spec.VersionedBeaconBlock,
) (*spec.VersionedBeaconBlock, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	epoch := s.network.EstimatedEpochAtSlot(slot)
	if s.blockedEpoch(epoch) {
		return nil, fmt.Errorf("block proposal requested for blocked epoch %d", epoch)
	}

	logger, gateway := s.requestContext(ctx)
	logger = logger.With(
		zap.Any("epoch", epoch),
		zap.Any("slot", slot),
	)

	for _, state := range s.validators {
		logger := logger.With(zap.Any("validator", state.validator.Index))

		if duty, ok := state.firstProposerDuty[gateway]; ok && duty.Slot == slot {
			// Fake a block at the slot of the duty.
			var err error
			block, err := fakeBeaconBlock(slot, randaoReveal, graffiti)
			if err != nil {
				return nil, fmt.Errorf("failed to create base block: %w", err)
			}
			state.firstBlock[gateway] = block
			logger.Info("produced first block proposal")
			return block, nil
		}

		if duty, ok := state.secondProposerDuty[gateway]; ok && duty.Slot == slot {
			if _, ok := state.firstBlock[gateway]; !ok {
				return nil, fmt.Errorf("unexpected state: requested second block before first block")
			}

			// Copy the first block to avoid mutating it.
			var secondBlock *spec.VersionedBeaconBlock
			b, err := json.Marshal(state.firstBlock[gateway])
			if err != nil {
				return nil, fmt.Errorf("failed to marshal first block: %w", err)
			}
			if err := json.Unmarshal(b, &secondBlock); err != nil {
				return nil, fmt.Errorf("failed to unmarshal first block: %w", err)
			}

			// Apply the test on the first block.
			if err := state.proposerTest.Apply(secondBlock); err != nil {
				return nil, fmt.Errorf("failed to apply proposer slashing test: %w", err)
			}

			state.secondBlock[gateway] = secondBlock
			logger.Info("produced second block proposal")

			return secondBlock, nil
		}
	}
	return nil, fmt.Errorf("block proposal requested for unknown duty")
}

func (s *SlashingInterceptor) InterceptSubmitBlockProposal(ctx context.Context, block *spec.VersionedSignedBeaconBlock) (*spec.VersionedSignedBeaconBlock, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	slot := block.Capella.Message.Slot
	epoch := s.network.EstimatedEpochAtSlot(slot)
	if s.blockedEpoch(epoch) {
		return nil, fmt.Errorf("block proposal submitted for blocked epoch %d", epoch)
	}

	logger, gateway := s.requestContext(ctx)
	logger = logger.With(zap.Any("epoch", epoch), zap.Any("slot", slot))

	for _, state := range s.validators {
		logger := logger.With(zap.Any("validator", state.validator.Index))

		if duty, ok := state.firstProposerDuty[gateway]; ok && duty.Slot == slot {
			if _, ok := state.firstSubmittedBlock[gateway]; ok {
				return nil, fmt.Errorf("first block already submitted")
			}
			state.firstSubmittedBlock[gateway] = block
			logger.Info("submitted first block proposal")
			return nil, nil
		}
		if duty, ok := state.secondProposerDuty[gateway]; ok && duty.Slot == slot {
			if _, ok := state.secondSubmittedBlock[gateway]; ok {
				return nil, fmt.Errorf("second block already submitted")
			}
			state.secondSubmittedBlock[gateway] = block
			if state.proposerTest.Slashable {
				return nil, fmt.Errorf("misbehavior: slashable block was submitted during the end epoch")
			}
			logger.Info("submitted second block proposal")
			return nil, nil
		}
	}

	return nil, fmt.Errorf("block proposal submitted for unknown duty at slot %d", slot)
}

func (s *SlashingInterceptor) blockedEpoch(epoch phase0.Epoch) bool {
	return epoch < s.startEpoch || epoch == s.sleepEpoch || epoch > s.endEpoch
}

func (s *SlashingInterceptor) requestContext(ctx context.Context) (*zap.Logger, beaconproxy.Gateway) {
	return ctx.Value(beaconproxy.LoggerKey{}).(*zap.Logger),
		ctx.Value(beaconproxy.GatewayKey{}).(beaconproxy.Gateway)
}

func gatewayNames(gateways []beaconproxy.Gateway) []string {
	names := make([]string, 0, len(gateways))
	for _, gateway := range gateways {
		names = append(names, gateway.Name)
	}
	return names
}

func fakeBeaconBlock(slot phase0.Slot, randaoReveal phase0.BLSSignature, graffiti [32]byte) (*spec.VersionedBeaconBlock, error) {
	var block *capella.BeaconBlock
	blockJSON := []byte(`{"slot":"1861337","proposer_index":"1610","parent_root":"0x353f35e506b07b6a20dd5d24c92b6dfda1b9e221848963d7a40cb7fe4e23c35a","state_root":"0x8e9cfedb909c4c9bbc52ca4174b279d31ffe40a667077c7b31a66bbd1a66d0be","body":{"randao_reveal":"0xb019a2ef4b7763c7bd5a9b3dbe00ba38cdfb6ee0fcecfe4a7301687973717f7c193072a3ebd70de3aed153f960b11fc40037b8164542e1364a31489ff7a0a1119ef46bc2ea099886446cba3acc4721a2534c292f6eeb650719f32477707aab3e","eth1_data":{"deposit_root":"0x9df92d765b5aa041fd4bbe8d5878eb89290efa78e444c1a603eecfae2ea05fa4","deposit_count":"403","block_hash":"0x1ee29c95ea816db342e04fbd1f00cb14940a45d025d2d14bf0d33187d8f4d5ea"},"graffiti":"0x626c6f787374616b696e672e636f6d0000000000000000000000000000000000","proposer_slashings":[],"attester_slashings":[],"attestations":[{"aggregation_bits":"0xffffffffffffff37","data":{"slot":"1861336","index":"0","beacon_block_root":"0x353f35e506b07b6a20dd5d24c92b6dfda1b9e221848963d7a40cb7fe4e23c35a","source":{"epoch":"58165","root":"0x5c0a59daecb2f509e0d5a038da44c55ef5827b0b979e6e397cc7f5846f9f3f4c"},"target":{"epoch":"58166","root":"0x835469d3b4348c2f5c2964afaac731acc0117eb0f5cec64bf166ab26847f77a8"}},"signature":"0x991b97e124a8dc672d135c167f030ceff9ab500dfd9a8bb79e199a303bfa54cebc205e396a16466e51d54cb7e0d981eb0eb03295bdb39b69d9559421aeea22838d22732d66ea79d3bebb8a8a335679491b60ad6d815318cbfd92a1a867c70255"}],"deposits":[],"voluntary_exits":[],"sync_aggregate":{"sync_committee_bits":"0xfffffbfffffffffffffffcffffdfffffffffffffffefffffffffffff7ffdffffbfffffffffffefffffebfffffffffffffffffffdff7fffffffffffffffffffff","sync_committee_signature":"0x91713624034f3c1e37baa3de1d082883c6e44cc830f4ecf0ca58cc06153aba97ca4539edfdb0f49a0d18b354f585422e114feb07b689db0a47ee72231d8e6fb2970663f518ca0a3fda0e701cd4662405c4f2fa9fdac0533c790f92404240432e"},"execution_payload":{"parent_hash":"0x4b37c3a4f001b91cfcac61b7f6bdfd2218525e4f421a4ab52fa513b00e76ab6f","fee_recipient":"0x0f35b0753e261375c9a6cb44316b4bdc7e765509","state_root":"0xc9d0b31cc38141d01998220dcc0e7830994e6a1d11fa3a26d7d113df82c1a53a","receipts_root":"0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421","logs_bloom":"0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000","prev_randao":"0x251fdfd6ebef77085f24016900debbd334e67cf51ee48d959f01ef12698a49a5","block_number":"3031542","gas_limit":"30000000","gas_used":"0","timestamp":"1678069644","extra_data":"0xd883010b02846765746888676f312e32302e31856c696e7578","base_fee_per_gas":"7","block_hash":"0x4cb1ca5a2947972cb018416fe4489b9daf674d5be97fd2ebb6de1a1608226733","transactions":[],"withdrawals":[{"index":"645759","validator_index":"425","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"2886"},{"index":"645760","validator_index":"428","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"2886"},{"index":"645761","validator_index":"429","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"2886"},{"index":"645762","validator_index":"431","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"2886"},{"index":"645763","validator_index":"434","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"2886"},{"index":"645764","validator_index":"437","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"2886"},{"index":"645765","validator_index":"440","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"1924"},{"index":"645766","validator_index":"441","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"1924"},{"index":"645767","validator_index":"448","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"1924"},{"index":"645768","validator_index":"450","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"1924"},{"index":"645769","validator_index":"451","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"1924"},{"index":"645770","validator_index":"456","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"1924"},{"index":"645771","validator_index":"458","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"1924"},{"index":"645772","validator_index":"465","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"1924"},{"index":"645773","validator_index":"467","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"1924"},{"index":"645774","validator_index":"468","address":"0x25c4a76e7d118705e7ea2e9b7d8c59930d8acd3b","amount":"1924"}]},"bls_to_execution_changes":[]}}`)
	if err := json.Unmarshal(blockJSON, &block); err != nil {
		return nil, err
	}
	block.Slot = slot
	block.Body.RANDAOReveal = randaoReveal
	block.Body.Graffiti = graffiti
	return &spec.VersionedBeaconBlock{
		Version: spec.DataVersionCapella,
		Capella: block,
	}, nil
}
