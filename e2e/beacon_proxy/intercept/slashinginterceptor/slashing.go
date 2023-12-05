package slashinginterceptor

import (
	"context"
	"crypto/rand"
	"fmt"
	"reflect"
	"sync"
	"time"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"

	beaconproxy "github.com/bloxapp/ssv/e2e/beacon_proxy"
)

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
	firstProposerDuty   *v1.ProposerDuty
	firstBlock          *spec.VersionedBeaconBlock
	firstSubmittedBlock *spec.VersionedSignedBeaconBlock

	// EndEpoch
	secondProposerDuty   *v1.ProposerDuty
	secondBlock          *spec.VersionedBeaconBlock
	secondSubmittedBlock *spec.VersionedSignedBeaconBlock
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
		logger:             logger,
		network:            network,
		startEpoch:         startEpoch,
		sleepEpoch:         startEpoch + 1,
		endEpoch:           startEpoch + 2,
		fakeProposerDuties: fakeProposerDuties,
		validators:         make(map[phase0.ValidatorIndex]*validatorState),
	}
	logger.Debug("creating slashing interceptor",
		zap.Any("start_epoch", s.startEpoch),
		zap.Any("end_epoch", s.endEpoch),
		zap.Any("sleep_epoch", s.sleepEpoch),
	)
	for _, validator := range validators {
		s.validators[validator.Index] = &validatorState{
			validator:                  validator,
			attesterTest:               AttesterSlashingTests[0], // TODO: extract from validators.json
			firstAttesterDuty:          make(map[beaconproxy.Gateway]*v1.AttesterDuty),
			firstAttestationData:       make(map[beaconproxy.Gateway]*phase0.AttestationData),
			firstSubmittedAttestation:  make(map[beaconproxy.Gateway]*phase0.Attestation),
			secondAttesterDuty:         make(map[beaconproxy.Gateway]*v1.AttesterDuty),
			secondAttestationData:      make(map[beaconproxy.Gateway]*phase0.AttestationData),
			secondSubmittedAttestation: make(map[beaconproxy.Gateway]*phase0.Attestation),
		}
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
				zap.Any("submitters", maps.Keys(state.firstSubmittedAttestation)),
			)
		} else {
			submittedCount++
			s.logger.Debug("validator submitted in start epoch",
				zap.Any("validator_index", state.validator.Index),
				zap.Any("validator_pk", state.validator.Validator.PublicKey.String()),
				zap.Any("submitters", maps.Keys(state.firstSubmittedAttestation)),
			)
		}
	}

	if submittedCount == len(s.validators) {
		s.logger.Info("all attestations submitted in start epoch", zap.Any("count", submittedCount))
	} else {
		s.logger.Info("not all attestations submitted in start epoch", zap.Any("submitted", submittedCount), zap.Any("expected", len(s.validators)))
	}
}

func (s *SlashingInterceptor) checkEndEpochAttestationSubmission() {
	submittedCount := 0
	for _, state := range s.validators {
		// TODO: support values other than 4
		if len(state.secondSubmittedAttestation) != 4 {
			s.logger.Debug("validator did not submit in end epoch",
				zap.Any("validator_index", state.validator.Index),
				zap.Any("validator_pk", state.validator.Validator.PublicKey.String()),
				zap.Any("submitters", maps.Keys(state.secondSubmittedAttestation)),
			)
		} else {
			submittedCount++
			s.logger.Debug("validator submitted in end epoch",
				zap.Any("validator_index", state.validator.Index),
				zap.Any("validator_pk", state.validator.Validator.PublicKey.String()),
				zap.Any("submitters", maps.Keys(state.secondSubmittedAttestation)),
			)
		}
	}

	if submittedCount == len(s.validators) {
		s.logger.Info("all attestations submitted in end epoch", zap.Any("count", submittedCount))
	} else {
		s.logger.Info("not all attestations submitted in end epoch", zap.Any("submitted", submittedCount), zap.Any("expected", len(s.validators)))
	}
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
		s.logger.Debug("epoch blocked, returning empty attester duties", zap.Any("epoch", epoch))
		return []*v1.AttesterDuty{}, nil
	} else {
		s.logger.Debug("epoch not blocked, returning real attester duties", zap.Any("epoch", epoch))
	}

	for _, duty := range duties {
		state, ok := s.validators[duty.ValidatorIndex]
		if !ok {
			continue
		}
		if _, ok = state.firstAttesterDuty[gateway]; !ok {
			if epoch != s.startEpoch {
				return nil, fmt.Errorf("misbehavior: first attester duty wasn't requested during the start epoch")
			}
			state.firstAttesterDuty[gateway] = duty
			continue
		}
		if reflect.DeepEqual(duty, state.firstAttesterDuty[gateway]) {
			s.logger.Debug("new duty is same as old duty, skipping")
			continue
		}
		if _, ok = state.secondAttesterDuty[gateway]; !ok {
			if epoch != s.endEpoch {
				return nil, fmt.Errorf("misbehavior: second attester duty wasn't requested during the end epoch")
			}
			state.secondAttesterDuty[gateway] = duty
			continue
		}
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
		s.logger.Debug("epoch blocked, returning empty attestation data", zap.Any("epoch", epoch))
		return nil, fmt.Errorf("attestation data requested for blocked epoch %d", epoch)
	} else {
		s.logger.Debug("epoch not blocked, returning real attestation data", zap.Any("epoch", epoch))
	}

	for _, state := range s.validators {
		// TODO: fix to track both first and second attester duties.

		// Skip validators that are not in the requested committee.
		if firstDuty, ok := state.firstAttesterDuty[gateway]; !ok || firstDuty.Slot != slot ||
			firstDuty.CommitteeIndex != committeeIndex {
			continue
		}

		// Record the first attestation data.
		if _, ok := state.firstAttestationData[gateway]; !ok {
			if epoch != s.startEpoch {
				return nil, fmt.Errorf("misbehavior: first attester data wasn't requested during the start epoch")
			}
			state.firstAttestationData[gateway] = data
			continue
		}

		if reflect.DeepEqual(data, state.firstAttestationData[gateway]) {
			s.logger.Debug("new duty is same as old duty, skipping")
			continue
		}

		// Apply the test on the second attestation data.
		if err := state.attesterTest.Apply(data); err != nil {
			return nil, fmt.Errorf("failed to apply attester slashing test: %w", err)
		}

		// Record the second attestation data.
		if _, ok := state.secondAttestationData[gateway]; !ok {
			if epoch != s.endEpoch {
				return nil, fmt.Errorf("misbehavior: second attester data wasn't requested during the end epoch")
			}
			state.secondAttestationData[gateway] = data
			continue
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

	_, gateway := s.requestContext(ctx)

	for _, attestation := range attestations {
		slot := attestation.Data.Slot

		epoch := s.network.EstimatedEpochAtSlot(slot)

		if s.blockedEpoch(epoch) {
			return nil, fmt.Errorf("attestation submitted for blocked epoch %d", epoch)
		}

		//if s.epochExpired(epoch) {
		//	return nil, fmt.Errorf("misbehavior: epoch expired")
		//}

		currentEpoch := s.network.EstimatedCurrentEpoch()
		if epoch == s.startEpoch && currentEpoch > s.startEpoch && currentEpoch-s.startEpoch >= 2 {
			return nil, fmt.Errorf("misbehavior: start epoch expired")
		}

		if epoch == s.endEpoch && currentEpoch > s.endEpoch && currentEpoch-s.endEpoch >= 2 {
			return nil, fmt.Errorf("misbehavior: end epoch expired")
		}

		s.logger.Debug("submitting attestation", zap.Any("epoch", epoch), zap.Any("slot", slot))

		for validatorIndex, state := range s.validators {
			// Skip validators that are not in the attestation's committee.
			if firstDuty, ok := state.firstAttesterDuty[gateway]; !ok ||
				firstDuty.CommitteeIndex != attestation.Data.Index {
				continue
			}

			// Record the submitted attestation.
			if _, ok := state.firstSubmittedAttestation[gateway]; !ok {
				s.logger.Debug("got first attestation", zap.Any("epoch", epoch), zap.Any("slot", slot), zap.Any("validator", validatorIndex))

				if epoch != s.startEpoch {
					return nil, fmt.Errorf("misbehavior: attestation wasn't submitted during the start epoch")
				}
				state.firstSubmittedAttestation[gateway] = attestation
				s.logger.Debug("submitted first attestation", zap.Any("epoch", epoch), zap.Any("slot", slot), zap.Any("validator", validatorIndex))
				if state.attesterTest.Slashable {
					return nil, fmt.Errorf("misbehavior: attestation was submitted during the start epoch")
				}
				continue
			}
			s.logger.Debug("got new attestation, checking if it's same as first", zap.Any("epoch", epoch), zap.Any("slot", slot), zap.Any("validator", validatorIndex))

			if reflect.DeepEqual(attestation, state.firstSubmittedAttestation[gateway]) {
				s.logger.Debug("new attestation is same as old attestation, skipping")
				continue
			}

			s.logger.Debug("got second attestation", zap.Any("epoch", epoch), zap.Any("slot", slot), zap.Any("validator", validatorIndex))

			// Record the second submitted attestation.
			if _, ok := state.secondSubmittedAttestation[gateway]; !ok {
				if epoch != s.endEpoch {
					return nil, fmt.Errorf("misbehavior: attestation wasn't submitted during the end epoch")
				}
				state.secondSubmittedAttestation[gateway] = attestation
				s.logger.Debug("submitted second attestation", zap.Any("epoch", epoch), zap.Any("slot", slot), zap.Any("validator", validatorIndex))
				if state.attesterTest.Slashable {
					return nil, fmt.Errorf("misbehavior: attestation was submitted during the end epoch")
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
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.blockedEpoch(epoch) {
		return []*v1.ProposerDuty{}, nil
	}

	if !s.fakeProposerDuties {
		return duties, nil
	}

	_, gateway := s.requestContext(ctx)

	// Fake 2 proposer duties for each validator.
	duties = make([]*v1.ProposerDuty, 0, len(indices)*2)
	for _, index := range indices {
		state, ok := s.validators[index]
		if !ok {
			continue
		}
		for i := 0; i < 2; i++ {
			duties = append(duties, &v1.ProposerDuty{
				ValidatorIndex: index,
				Slot:           state.firstAttesterDuty[gateway].Slot + phase0.Slot(i),
			})
		}
	}
	return duties, nil
}

func (s *SlashingInterceptor) InterceptBlockProposal(
	ctx context.Context,
	slot phase0.Slot,
	randaoReveal phase0.BLSSignature,
	graffiti []byte,
	block *spec.VersionedBeaconBlock,
) (*spec.VersionedBeaconBlock, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	epoch := s.network.EstimatedEpochAtSlot(slot)
	if s.blockedEpoch(epoch) {
		return nil, fmt.Errorf("block proposal requested for blocked epoch %d", epoch)
	}

	_, gateway := s.requestContext(ctx)

	for _, state := range s.validators {
		// Skip forward to the proposer.
		if _, ok := state.firstAttesterDuty[gateway]; !ok || state.firstAttesterDuty[gateway].Slot != slot {
			continue
		}

		// Record the first block.
		if state.firstBlock == nil {
			state.firstBlock = block
			continue
		}

		// Apply the test on the second block.
		if err := state.proposerTest.Apply(block); err != nil {
			return nil, fmt.Errorf("failed to apply proposer slashing test: %w", err)
		}
	}
	return block, nil
}

func (s *SlashingInterceptor) InterceptSubmitBlockProposal(ctx context.Context, block *spec.VersionedSignedBeaconBlock) (*spec.VersionedSignedBeaconBlock, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	slot := block.Capella.Message.Slot
	epoch := s.network.EstimatedEpochAtSlot(slot)
	if s.blockedEpoch(epoch) {
		return nil, fmt.Errorf("block proposal submitted for blocked epoch %d", epoch)
	}

	for _, state := range s.validators {
		if state.firstProposerDuty == nil || state.firstProposerDuty.Slot != slot ||
			state.firstProposerDuty.ValidatorIndex != block.Capella.Message.ProposerIndex {
			continue
		}

		if state.firstSubmittedBlock == nil {
			if epoch != s.endEpoch {
				return nil, fmt.Errorf("misbehavior: proposal wasn't submitted during the end epoch")
			}
			state.firstSubmittedBlock = block
			if state.proposerTest.Slashable {
				return nil, fmt.Errorf("misbehavior: proposal was submitted during the end epoch")
			}
		}
	}

	//if s.epochExpired(epoch) {
	//	return nil, fmt.Errorf("misbehavior: start epoch expired")
	//}

	return block, nil
}

func (s *SlashingInterceptor) blockedEpoch(epoch phase0.Epoch) bool {
	return epoch < s.startEpoch || epoch == s.sleepEpoch || epoch > s.endEpoch
}

func (s *SlashingInterceptor) expiredEpoch(epoch phase0.Epoch) bool {
	currentEpoch := s.network.EstimatedCurrentEpoch()
	if epoch == s.startEpoch && currentEpoch > s.startEpoch && currentEpoch-s.startEpoch >= 2 {
		return true
	}

	if epoch == s.endEpoch && currentEpoch > s.endEpoch && currentEpoch-s.endEpoch >= 2 {
		return true
	}

	return false
}

func (s *SlashingInterceptor) epochExpired(epoch phase0.Epoch) bool {
	return epoch > s.startEpoch && epoch-s.startEpoch >= 2
}

func (s *SlashingInterceptor) requestContext(ctx context.Context) (*zap.Logger, beaconproxy.Gateway) {
	return ctx.Value(beaconproxy.LoggerKey{}).(*zap.Logger),
		ctx.Value(beaconproxy.GatewayKey{}).(beaconproxy.Gateway)
}

// TEST CASES

var ProposerSlashingTests = []ProposerSlashingTest{
	{
		Name:      "HigherSlot_DifferentRoot",
		Slashable: false,
		Apply: func(block *spec.VersionedBeaconBlock) error {
			switch block.Version {
			case spec.DataVersionCapella:
				block.Capella.Slot++
			default:
				return fmt.Errorf("unsupported version: %s", block.Version)
			}
			_, err := rand.Read(block.Capella.ParentRoot[:])
			return err
		},
	},
	{
		Name:      "SameSlot_DifferentRoot",
		Slashable: true,
		Apply: func(block *spec.VersionedBeaconBlock) error {
			_, err := rand.Read(block.Capella.ParentRoot[:])
			return err
		},
	},
	{
		Name:      "LowerSlot_SameRoot",
		Slashable: true,
		Apply: func(block *spec.VersionedBeaconBlock) error {
			switch block.Version {
			case spec.DataVersionCapella:
				block.Capella.Slot--
			default:
				return fmt.Errorf("unsupported version: %s", block.Version)
			}
			return nil
		},
	},
}

var AttesterSlashingTests = []AttesterSlashingTest{
	{
		Name:      "SameSource_HigherTarget_DifferentRoot",
		Slashable: false,
		Apply: func(data *phase0.AttestationData) error {
			data.Target.Epoch++
			_, err := rand.Read(data.BeaconBlockRoot[:])
			return err
		},
	},
	{
		Name:      "SameSource_SameTarget_SameRoot",
		Slashable: true,
		Apply: func(data *phase0.AttestationData) error {
			return nil
		},
	},
	{
		Name:      "SameSource_SameTarget_DifferentRoot",
		Slashable: true,
		Apply: func(data *phase0.AttestationData) error {
			_, err := rand.Read(data.BeaconBlockRoot[:])
			return err
		},
	},
	{
		Name:      "LowerSource_HigherTarget_SameRoot",
		Slashable: true,
		Apply: func(data *phase0.AttestationData) error {
			data.Source.Epoch--
			return nil
		},
	},
	{
		Name:      "HigherSource_SameTarget_SameRoot",
		Slashable: true,
		Apply: func(data *phase0.AttestationData) error {
			data.Source.Epoch++
			return nil
		},
	},
	{
		Name:      "LowerSource_HigherTarget_SameRoot",
		Slashable: true,
		Apply: func(data *phase0.AttestationData) error {
			data.Source.Epoch--
			return nil
		},
	},
}
