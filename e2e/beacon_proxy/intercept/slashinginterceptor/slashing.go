package slashinginterceptor

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	beaconproxy "github.com/ssvlabs/ssv/e2e/beacon_proxy"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

const startEndEpochsDiff = 2

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
	a := 0
	p := 0
	for _, validator := range validators {
		if a == len(AttesterSlashingTests) {
			a = 0
		}
		if p == len(ProposerSlashingTests) {
			p = 0
		}
		attesterTest := AttesterSlashingTests[a]
		proposerTest := ProposerSlashingTests[p]
		a++
		p++
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

func (s *SlashingInterceptor) checkStartEpochProposalSubmission() {
	submittedCount := 0
	for _, state := range s.validators {
		// TODO: support values other than 4
		if len(state.firstSubmittedBlock) != 4 {
			s.logger.Debug("validator did not submit proposal in start epoch",
				zap.Any("validator_index", state.validator.Index),
				zap.Any("validator_pk", state.validator.Validator.PublicKey.String()),
				zap.Any("submitters", gatewayNames(slices.Collect(maps.Keys(state.firstSubmittedBlock)))),
			)
		} else {
			submittedCount++
			s.logger.Debug("validator submitted proposal in start epoch",
				zap.Any("validator_index", state.validator.Index),
				zap.Any("validator_pk", state.validator.Validator.PublicKey.String()),
				zap.Any("submitters", gatewayNames(slices.Collect(maps.Keys(state.firstSubmittedBlock)))),
			)
		}
	}

	if submittedCount == len(s.validators) {
		s.logger.Info("all proposals submitted in start epoch", zap.Any("count", submittedCount))
	} else {
		s.logger.Warn("not all proposals submitted in start epoch", zap.Any("submitted", submittedCount), zap.Any("expected", len(s.validators)))
	}
}

func (s *SlashingInterceptor) checkEndEpochProposalSubmission() {
	// TODO: currently we're not able to preform proposer duties in end epoch
}

func (s *SlashingInterceptor) checkStartEpochAttestationSubmission() {
	submittedCount := 0
	for _, state := range s.validators {
		// TODO: support values other than 4
		if len(state.firstSubmittedAttestation) != 4 {
			s.logger.Debug("validator did not submit in start epoch",
				zap.Any("validator_index", state.validator.Index),
				zap.Any("validator_pk", state.validator.Validator.PublicKey.String()),
				zap.Any("submitters", gatewayNames(slices.Collect(maps.Keys(state.firstSubmittedAttestation)))),
			)
		} else {
			submittedCount++
			s.logger.Debug("validator submitted in start epoch",
				zap.Any("validator_index", state.validator.Index),
				zap.Any("validator_pk", state.validator.Validator.PublicKey.String()),
				zap.Any("submitters", gatewayNames(slices.Collect(maps.Keys(state.firstSubmittedAttestation)))),
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
					zap.Any("submitters", gatewayNames(slices.Collect(maps.Keys(state.secondSubmittedAttestation)))),
					zap.Any("first_attestation", state.firstSubmittedAttestation),
					zap.Any("second_attestation", state.secondSubmittedAttestation),
					zap.Any("test", state.attesterTest.Name),
				)
			}
		} else if len(state.secondSubmittedAttestation) != 4 { // TODO: support values other than 4
			s.logger.Debug("validator did not submit in end epoch",
				zap.Any("validator_index", state.validator.Index),
				zap.Any("validator_pk", state.validator.Validator.PublicKey.String()),
				zap.Any("submitters", gatewayNames(slices.Collect(maps.Keys(state.secondSubmittedAttestation)))),
				zap.Any("test", state.attesterTest.Name),
			)
		} else {
			submittedCount++
			s.logger.Debug("validator submitted in end epoch",
				zap.Any("validator_index", state.validator.Index),
				zap.Any("validator_pk", state.validator.Validator.PublicKey.String()),
				zap.Any("submitters", gatewayNames(slices.Collect(maps.Keys(state.secondSubmittedAttestation)))),
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
