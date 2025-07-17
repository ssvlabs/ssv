package signing

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/sourcegraph/conc/pool"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/suite"

	"github.com/ssvlabs/ssv/ssvsigner/e2e"
	"github.com/ssvlabs/ssv/ssvsigner/e2e/common"
)

// AttestationSlashingTestSuite tests attestation slashing protection functionality
type AttestationSlashingTestSuite struct {
	e2e.E2ETestSuite
}

// TestAttestationDoubleVote validates slashing protection against double votes.
// Ensures both SSV and Web3Signer prevent signing different attestations for the same target epoch.
func (s *AttestationSlashingTestSuite) TestAttestationDoubleVote() {
	s.T().Logf("🧪 Testing double vote slashing protection")
	testCurrentEpoch := phase0.Epoch(4200)
	s.GetEnv().SetTestCurrentEpoch(testCurrentEpoch)

	ctx, cancel := context.WithTimeout(s.GetContext(), 60*time.Second)
	defer cancel()

	validatorKeyPair := s.AddValidator(ctx)

	s.T().Logf("✍️ Phase 1: Signing first valid attestation")
	testEpoch := testCurrentEpoch + 1                                      // One epoch ahead of mocked current
	testSlot := s.GetEnv().GetBeaconConfig().EpochFirstSlot(testEpoch) + 1 // First slot + 1 of the epoch
	initialAttestationData := common.NewTestAttestationData(testEpoch-1, testEpoch, testSlot)

	s.RequireValidSigning(
		ctx,
		initialAttestationData,
		validatorKeyPair.BLSPubKey,
		testSlot,
		spectypes.DomainAttester,
	)

	s.T().Logf("🚫 Phase 2: Attempting double vote (should be rejected)")
	doubleVoteAttestation := common.NewTestAttestationData(testEpoch-1, testEpoch, testSlot)
	doubleVoteAttestation.Source.Root = phase0.Root{0x06}     // Different source root for clarity
	doubleVoteAttestation.Target.Root = phase0.Root{0x04}     // Conflicting target root
	doubleVoteAttestation.BeaconBlockRoot = phase0.Root{0x05} // Different head block

	s.RequireFailedSigning(
		ctx,
		doubleVoteAttestation,
		validatorKeyPair.BLSPubKey,
		testSlot,
		spectypes.DomainAttester,
	)

	s.T().Logf("✅ Phase 3: Testing valid progression to higher epoch")
	validNextEpoch := testEpoch + 1
	validNextSlot := s.GetEnv().GetBeaconConfig().EpochFirstSlot(validNextEpoch) + 1
	validProgressionAttData := common.NewTestAttestationData(testEpoch, validNextEpoch, validNextSlot)

	s.RequireValidSigning(
		ctx,
		validProgressionAttData,
		validatorKeyPair.BLSPubKey,
		validNextSlot,
		spectypes.DomainAttester,
	)
}

// TestAttestationSurroundingVote validates slashing protection against surrounding votes.
// Ensures both layers prevent signing attestations that surround previous ones (wider epoch range).
func (s *AttestationSlashingTestSuite) TestAttestationSurroundingVote() {
	s.T().Logf("🧪 Testing surrounding vote slashing protection")
	testCurrentEpoch := phase0.Epoch(4200)
	s.GetEnv().SetTestCurrentEpoch(testCurrentEpoch)

	ctx, cancel := context.WithTimeout(s.GetContext(), 60*time.Second)
	defer cancel()

	validatorKeyPair := s.AddValidator(ctx)

	s.T().Logf("✍️ Phase 1: Signing initial narrow attestation")
	testEpoch := testCurrentEpoch + 10
	testSlot := s.GetEnv().GetBeaconConfig().EpochFirstSlot(testEpoch) + 1
	initialAttestationData := common.NewTestAttestationData(testEpoch-1, testEpoch, testSlot)

	s.RequireValidSigning(
		ctx,
		initialAttestationData,
		validatorKeyPair.BLSPubKey,
		testSlot,
		spectypes.DomainAttester,
	)

	s.T().Logf("🚫 Phase 2: Attempting surrounding vote (should be rejected)")
	surroundingAttestationData := common.NewTestAttestationData(testEpoch-2, testEpoch+1, testSlot+32)
	surroundingAttestationData.Target.Root = phase0.Root{0x06}     // Different target root for clarity
	surroundingAttestationData.BeaconBlockRoot = phase0.Root{0x04} // Different block root
	surroundingAttestationData.Source.Root = phase0.Root{0x05}     // Different source root

	s.RequireFailedSigning(
		ctx,
		surroundingAttestationData,
		validatorKeyPair.BLSPubKey,
		surroundingAttestationData.Slot,
		spectypes.DomainAttester,
	)

	s.T().Logf("✅ Phase 3: Testing valid progression after rejection")
	validNextEpoch := testEpoch + 2
	validNextSlot := s.GetEnv().GetBeaconConfig().EpochFirstSlot(validNextEpoch) + 1
	validProgressionAttData := common.NewTestAttestationData(testEpoch, validNextEpoch, validNextSlot)

	s.RequireValidSigning(
		ctx,
		validProgressionAttData,
		validatorKeyPair.BLSPubKey,
		validNextSlot,
		spectypes.DomainAttester,
	)
}

// TestAttestationSurroundedVote validates slashing protection against surrounded votes.
// Ensures both layers prevent signing attestations within a previous attestation's epoch range.
func (s *AttestationSlashingTestSuite) TestAttestationSurroundedVote() {
	s.T().Logf("🧪 Testing surrounded vote slashing protection")
	testCurrentEpoch := phase0.Epoch(4200)
	s.GetEnv().SetTestCurrentEpoch(testCurrentEpoch)

	ctx, cancel := context.WithTimeout(s.GetContext(), 60*time.Second)
	defer cancel()

	validatorKeyPair := s.AddValidator(ctx)

	s.T().Logf("✍️ Phase 1: Signing wide attestation first")
	testEpoch := testCurrentEpoch + 10
	testSlot := s.GetEnv().GetBeaconConfig().EpochFirstSlot(testEpoch) + 1
	initialAttestationData := common.NewTestAttestationData(testEpoch-2, testEpoch+1, testSlot+32)

	s.RequireValidSigning(
		ctx,
		initialAttestationData,
		validatorKeyPair.BLSPubKey,
		initialAttestationData.Slot,
		spectypes.DomainAttester,
	)

	s.T().Logf("🚫 Phase 2: Attempting surrounded vote (should be rejected)")
	surroundedAttestationData := common.NewTestAttestationData(testEpoch-1, testEpoch, testSlot)
	surroundedAttestationData.Target.Root = phase0.Root{0x06}     // Different target root for clarity
	surroundedAttestationData.BeaconBlockRoot = phase0.Root{0x04} // Different block root
	surroundedAttestationData.Source.Root = phase0.Root{0x05}     // Different source root

	s.RequireFailedSigning(
		ctx,
		surroundedAttestationData,
		validatorKeyPair.BLSPubKey,
		surroundedAttestationData.Slot,
		spectypes.DomainAttester,
	)

	s.T().Logf("✅ Phase 3: Testing valid progression after rejection")
	validNextEpoch := testEpoch + 10
	validNextSlot := s.GetEnv().GetBeaconConfig().EpochFirstSlot(validNextEpoch) + 1
	validProgressionAttData := common.NewTestAttestationData(testEpoch+5, validNextEpoch, validNextSlot)

	s.RequireValidSigning(
		ctx,
		validProgressionAttData,
		validatorKeyPair.BLSPubKey,
		validNextSlot,
		spectypes.DomainAttester,
	)
}

// TestAttestationValidProgression validates proper attestation progression.
// Ensures valid sequential attestations with increasing epochs are signed successfully.
func (s *AttestationSlashingTestSuite) TestAttestationValidProgression() {
	s.T().Logf("🧪 Testing valid attestation progression")
	testCurrentEpoch := phase0.Epoch(4200)
	s.GetEnv().SetTestCurrentEpoch(testCurrentEpoch)

	ctx, cancel := context.WithTimeout(s.GetContext(), 60*time.Second)
	defer cancel()

	validatorKeyPair := s.AddValidator(ctx)

	s.T().Logf("✍️ Phase 1: Creating sequence of valid progressive attestations")
	startEpoch := testCurrentEpoch + 10
	startSlot := s.GetEnv().GetBeaconConfig().EpochFirstSlot(startEpoch) + 1

	// Define a series of valid sequential attestations
	attestations := make([]*phase0.AttestationData, 5)
	for i := 0; i < 5; i++ {
		sourceEpoch := startEpoch - 1 + phase0.Epoch(i)
		targetEpoch := sourceEpoch + 1
		slot := startSlot + phase0.Slot(i*32)
		attestations[i] = common.NewTestAttestationData(sourceEpoch, targetEpoch, slot)
		attestations[i].BeaconBlockRoot = phase0.Root{byte(i + 1)}
		attestations[i].Source.Root = phase0.Root{byte(i + 2)}
		attestations[i].Target.Root = phase0.Root{byte(i + 3)}
	}

	s.T().Logf("✍️ Phase 2: Signing attestations sequentially")
	var allSigs []spectypes.Signature

	for _, attestationData := range attestations {
		sig := s.RequireValidSigning(
			ctx,
			attestationData,
			validatorKeyPair.BLSPubKey,
			attestationData.Slot,
			spectypes.DomainAttester,
		)
		allSigs = append(allSigs, sig)
	}

	s.T().Logf("🔍 Phase 3: Verifying all signatures are unique")
	for i := 0; i < len(allSigs); i++ {
		for j := i + 1; j < len(allSigs); j++ {
			s.Require().NotEqual(allSigs[i], allSigs[j], "Signatures %d and %d should be different", i, j)
		}
	}
}

// TestConcurrentAttestationSigning validates concurrent signing safety.
// Ensures SSV allows only one concurrent signature while Web3Signer allows all.
func (s *AttestationSlashingTestSuite) TestConcurrentAttestationSigning() {
	s.T().Logf("🔄 Testing concurrent attestation signing")
	testCurrentEpoch := phase0.Epoch(4200)
	s.GetEnv().SetTestCurrentEpoch(testCurrentEpoch)

	ctx, cancel := context.WithTimeout(s.GetContext(), 60*time.Second)
	defer cancel()

	validatorKeyPair := s.AddValidator(ctx)

	testEpoch := testCurrentEpoch + 10
	testSlot := s.GetEnv().GetBeaconConfig().EpochFirstSlot(testEpoch) + 5
	attestationData := common.NewTestAttestationData(testEpoch-1, testEpoch, testSlot)
	attestationData.BeaconBlockRoot = phase0.Root{0x42} // Distinguish this concurrent test attestation

	domain, err := s.CalculateDomain(spectypes.DomainAttester, testEpoch)
	s.Require().NoError(err)

	numGoroutines := 12

	s.T().Logf("🔀 Phase 1: Testing LocalKeyManager concurrent protection")
	localSuccessCount := s.RunConcurrentSigning(ctx, numGoroutines, "LocalKeyManager", func() (spectypes.Signature, phase0.Root, error) {
		return s.GetEnv().GetLocalKeyManager().SignBeaconObject(
			ctx,
			attestationData,
			domain,
			validatorKeyPair.BLSPubKey,
			testSlot,
			spectypes.DomainAttester,
		)
	})

	// LocalKeyManager allows only 1 successful signature by design (slashing protection)
	s.Require().Equal(1, localSuccessCount)

	s.T().Logf("🔀 Phase 2: Testing RemoteKeyManager concurrent protection")
	remoteSuccessCount := s.RunConcurrentSigning(ctx, numGoroutines, "RemoteKeyManager", func() (spectypes.Signature, phase0.Root, error) {
		return s.GetEnv().GetRemoteKeyManager().SignBeaconObject(
			ctx,
			attestationData,
			domain,
			validatorKeyPair.BLSPubKey,
			testSlot,
			spectypes.DomainAttester,
		)
	})

	// RemoteKeyManager allows only 1 successful signature by design (slashing protection)
	s.Require().Equal(1, remoteSuccessCount)

	s.T().Logf("🔀 Phase 3: Testing Web3Signer concurrent protection")
	web3SignerSuccessCount := s.RunConcurrentSigning(ctx, numGoroutines, "Web3Signer", func() (spectypes.Signature, phase0.Root, error) {
		return s.SignWeb3Signer(
			ctx,
			attestationData,
			domain,
			validatorKeyPair.BLSPubKey,
			testSlot,
			spectypes.DomainAttester,
		)
	})

	// Web3Signer allows same source/target/block root (no slashing protection violation)
	s.Require().Equal(numGoroutines, web3SignerSuccessCount)
}

// TestWeb3SignerRestart validates slashing protection persistence across restarts.
func (s *AttestationSlashingTestSuite) TestWeb3SignerRestart() {
	s.T().Logf("🔄 Testing attestation restart persistence")
	testCurrentEpoch := phase0.Epoch(4200)
	s.GetEnv().SetTestCurrentEpoch(testCurrentEpoch)

	ctx, cancel := context.WithTimeout(s.GetContext(), 60*time.Second)
	defer cancel()

	validatorKeyPair := s.AddValidator(ctx)

	s.T().Logf("✍️ Phase 1: Signing initial attestation before restart")
	testEpoch := testCurrentEpoch + 10
	testSlot := s.GetEnv().GetBeaconConfig().EpochFirstSlot(testEpoch) + 5
	initialAttestationData := common.NewTestAttestationData(testEpoch-1, testEpoch, testSlot)

	s.RequireValidSigning(
		ctx,
		initialAttestationData,
		validatorKeyPair.BLSPubKey,
		testSlot,
		spectypes.DomainAttester,
	)

	s.T().Logf("🔄 Phase 2: Restarting Web3Signer container")
	err := s.GetEnv().RestartWeb3Signer()
	s.Require().NoError(err, "Failed to restart Web3Signer")

	s.T().Logf("🔍 Phase 3: Testing slashing protection persistence (should be rejected)")
	conflictingAttestationData := common.NewTestAttestationData(testEpoch-2, testEpoch, testSlot)

	s.RequireFailedSigning(
		ctx,
		conflictingAttestationData,
		validatorKeyPair.BLSPubKey,
		testSlot,
		spectypes.DomainAttester,
	)

	s.T().Logf("✅ Phase 4: Testing valid operations after restart")
	higherTestEpoch := testEpoch + 5
	higherTestSlot := s.GetEnv().GetBeaconConfig().EpochFirstSlot(higherTestEpoch) + 1
	validAttestationData := common.NewTestAttestationData(testEpoch+2, higherTestEpoch, higherTestSlot)

	s.RequireValidSigning(
		ctx,
		validAttestationData,
		validatorKeyPair.BLSPubKey,
		higherTestSlot,
		spectypes.DomainAttester,
	)
}

// TestWeb3SignerPostgreSQLRestart validates slashing protection persistence across PostgreSQL restarts.
func (s *AttestationSlashingTestSuite) TestWeb3SignerPostgreSQLRestart() {
	s.T().Logf("💾 Testing Web3Signer PostgreSQL restart with attestation persistence")
	testCurrentEpoch := phase0.Epoch(4200)
	s.GetEnv().SetTestCurrentEpoch(testCurrentEpoch)

	ctx, cancel := context.WithTimeout(s.GetContext(), 60*time.Second)
	defer cancel()

	validatorKeyPair := s.AddValidator(ctx)

	s.T().Logf("✍️ Phase 1: Signing initial attestation to populate database")
	testEpoch := testCurrentEpoch + 10
	testSlot := s.GetEnv().GetBeaconConfig().EpochFirstSlot(testEpoch) + 5
	initialAttestationData := common.NewTestAttestationData(testEpoch-1, testEpoch, testSlot)

	s.RequireValidSigning(
		ctx,
		initialAttestationData,
		validatorKeyPair.BLSPubKey,
		testSlot,
		spectypes.DomainAttester,
	)

	s.T().Logf("🔄 Phase 2: Restarting PostgreSQL database container with persistent volume")
	err := s.GetEnv().RestartPostgreSQL()
	s.Require().NoError(err, "Failed to restart PostgreSQL")

	s.T().Logf("🔍 Phase 3: Verifying data persistence - attempting conflicting attestation (should be rejected)")
	conflictingAttestationData := common.NewTestAttestationData(testEpoch-2, testEpoch, testSlot)

	s.RequireFailedSigning(
		ctx,
		conflictingAttestationData,
		validatorKeyPair.BLSPubKey,
		testSlot,
		spectypes.DomainAttester,
	)

	s.T().Logf("✅ Phase 4: Signing new valid attestation with different epoch")
	freshTestEpoch := testEpoch + 10
	freshTestSlot := s.GetEnv().GetBeaconConfig().EpochFirstSlot(freshTestEpoch) + 1
	freshAttestationData := common.NewTestAttestationData(freshTestEpoch-1, freshTestEpoch, freshTestSlot)

	s.RequireValidSigning(
		ctx,
		freshAttestationData,
		validatorKeyPair.BLSPubKey,
		freshTestSlot,
		spectypes.DomainAttester,
	)

	s.T().Logf("🛡️ Phase 5: Testing slashing protection with double vote (should be rejected)")
	conflictingFreshData := common.NewTestAttestationData(freshTestEpoch-2, freshTestEpoch, freshTestSlot)
	conflictingFreshData.BeaconBlockRoot = phase0.Root{0xAA} // Different root = double vote

	s.RequireFailedSigning(
		ctx,
		conflictingFreshData,
		validatorKeyPair.BLSPubKey,
		freshTestSlot,
		spectypes.DomainAttester,
	)
}

// TestSSVSignerRestart validates SSV-Signer restart functionality with attestations.
func (s *AttestationSlashingTestSuite) TestSSVSignerRestart() {
	s.T().Logf("🔄 Testing SSV-Signer restart with attestation functionality")
	testCurrentEpoch := phase0.Epoch(4200)
	s.GetEnv().SetTestCurrentEpoch(testCurrentEpoch)

	ctx, cancel := context.WithTimeout(s.GetContext(), 60*time.Second)
	defer cancel()

	validatorKeyPair := s.AddValidator(ctx)

	s.T().Logf("✍️ Phase 1: Signing initial attestation before restart")
	testEpoch := testCurrentEpoch + 10
	testSlot := s.GetEnv().GetBeaconConfig().EpochFirstSlot(testEpoch) + 5
	initialAttestationData := common.NewTestAttestationData(testEpoch-1, testEpoch, testSlot)

	s.RequireValidSigning(
		ctx,
		initialAttestationData,
		validatorKeyPair.BLSPubKey,
		testSlot,
		spectypes.DomainAttester,
	)

	s.T().Logf("🔄 Phase 2: Restarting SSV-Signer container")
	err := s.GetEnv().RestartSSVSigner()
	s.Require().NoError(err, "Failed to restart SSV-Signer")

	s.T().Logf("🔍 Phase 3: Verifying SSV-Signer operational status")
	ssvSignerClient := s.GetEnv().GetSSVSignerClient()
	validators, err := ssvSignerClient.ListValidators(ctx)
	s.Require().NoError(err, "Failed to list validators")
	s.Require().True(slices.Contains(validators, validatorKeyPair.BLSPubKey))

	s.T().Logf("✅ Phase 4: Testing signing operations after restart")
	higherTestSlot := testSlot + 10
	newAttestationData := common.NewTestAttestationData(testEpoch, testEpoch+1, higherTestSlot)

	s.RequireValidSigning(
		ctx,
		newAttestationData,
		validatorKeyPair.BLSPubKey,
		higherTestSlot,
		spectypes.DomainAttester,
	)

	s.T().Logf("🛡️ Phase 5: Testing slashing protection after SSV-Signer restart (should be rejected)")
	conflictingAttestationData := common.NewTestAttestationData(testEpoch-2, testEpoch, testSlot)
	conflictingAttestationData.BeaconBlockRoot = phase0.Root{0x99} // Different root = double vote

	s.RequireFailedSigning(
		ctx,
		conflictingAttestationData,
		validatorKeyPair.BLSPubKey,
		testSlot,
		spectypes.DomainAttester,
	)
}

// TestConcurrentSigningStress validates concurrent signing and signature consistency.
// Tests multiple validators signing attestations in parallel and validates that all key managers
// produce identical signatures for the same validator+attestation combination.
func (s *AttestationSlashingTestSuite) TestConcurrentSigningStress() {
	testCurrentEpoch := phase0.Epoch(4200)
	s.GetEnv().SetTestCurrentEpoch(testCurrentEpoch)

	ctx, cancel := context.WithTimeout(s.GetContext(), 120*time.Second)
	defer cancel()

	numValidators := 50
	s.T().Logf("✍️ Setting up %d validators for stress test", numValidators)

	validators := make([]*common.ValidatorKeyPair, numValidators)
	for i := 0; i < numValidators; i++ {
		validators[i] = s.AddValidator(ctx)
		if (i+1)%10 == 0 {
			s.T().Logf("✅ Added %d/%d validators", i+1, numValidators)
		}
	}

	testEpoch := testCurrentEpoch + 10
	testSlot := s.GetEnv().GetBeaconConfig().EpochFirstSlot(testEpoch) + 5
	domain, err := s.CalculateDomain(spectypes.DomainAttester, testEpoch)
	s.Require().NoError(err)

	attestations := make([]*phase0.AttestationData, numValidators)
	for i := 0; i < numValidators; i++ {
		attestations[i] = common.NewTestAttestationData(testEpoch-1, testEpoch, testSlot)
		attestations[i].Index = phase0.CommitteeIndex(i % 64)
		attestations[i].BeaconBlockRoot = phase0.Root{byte(i % 256)}
	}

	// Store signatures from each key manager for comparison
	type SignResult struct {
		Signature spectypes.Signature
		Root      phase0.Root
		Error     error
	}

	allResults := make(map[string]map[phase0.BLSPubKey]SignResult) // keyManager -> map[pubKey]SignResult

	keyManagers := []struct {
		name     string
		signFunc func(ctx context.Context, obj ssz.HashRoot, domain phase0.Domain, pubKey phase0.BLSPubKey, slot phase0.Slot, domainType phase0.DomainType) (spectypes.Signature, phase0.Root, error)
	}{
		{"LocalKeyManager", s.GetEnv().GetLocalKeyManager().SignBeaconObject},
		{"RemoteKeyManager", s.GetEnv().GetRemoteKeyManager().SignBeaconObject},
		{"Web3Signer", s.SignWeb3Signer},
	}

	for _, km := range keyManagers {
		s.T().Logf("🧪 Testing %s with %d concurrent signings", km.name, numValidators)
		startTime := time.Now()

		p := pool.NewWithResults[*e2e.ConcurrentSignResult]().WithContext(ctx)

		for i, validator := range validators {
			idx := i
			val := validator
			p.Go(func(ctx context.Context) (*e2e.ConcurrentSignResult, error) {
				sig, root, err := km.signFunc(ctx, attestations[idx], domain, val.BLSPubKey, testSlot, spectypes.DomainAttester)
				return &e2e.ConcurrentSignResult{Sig: sig, Root: root, Err: err, ID: idx}, nil
			})
		}

		results, err := p.Wait()
		s.Require().NoError(err, "%s: Pool execution failed", km.name)

		// Process results and map by validator public key
		successCount := 0
		kmResults := make(map[phase0.BLSPubKey]SignResult)

		for _, result := range results {
			validator := validators[result.ID]
			kmResults[validator.BLSPubKey] = SignResult{
				Signature: result.Sig,
				Root:      result.Root,
				Error:     result.Err,
			}
			if result.Err == nil {
				successCount++
			}
		}

		allResults[km.name] = kmResults

		duration := time.Since(startTime)
		avgDivisor := successCount
		if avgDivisor == 0 {
			avgDivisor = 1
		}
		s.T().Logf("⏱️ %s: %d/%d successful in %v (avg: %v/sig)",
			km.name, successCount, numValidators, duration, duration/time.Duration(avgDivisor))

		// All should succeed since different validators with different attestations
		s.Require().Equal(numValidators, successCount, "%s: All should succeed with different validators", km.name)
	}

	// Compare signatures across key managers for each validator
	s.T().Logf("🔄 Validating signature consistency across key managers")
	localResults := allResults["LocalKeyManager"]
	remoteResults := allResults["RemoteKeyManager"]
	web3Results := allResults["Web3Signer"]

	consistentSignatures := 0
	for _, validator := range validators {
		pubKey := validator.BLSPubKey

		s.Require().NoError(localResults[pubKey].Error, "LocalKeyManager should succeed")
		s.Require().NoError(remoteResults[pubKey].Error, "RemoteKeyManager should succeed")
		s.Require().NoError(web3Results[pubKey].Error, "Web3Signer should succeed")

		// Signatures should be identical for same validator+attestation
		s.Require().Equal(localResults[pubKey].Signature, remoteResults[pubKey].Signature,
			"LocalKeyManager and RemoteKeyManager signatures should match")
		s.Require().Equal(localResults[pubKey].Signature, web3Results[pubKey].Signature,
			"LocalKeyManager and Web3Signer signatures should match")
		s.Require().Equal(localResults[pubKey].Root, remoteResults[pubKey].Root,
			"Signing roots should match")
		s.Require().Equal(localResults[pubKey].Root, web3Results[pubKey].Root,
			"Signing roots should match")

		consistentSignatures++
	}

	s.Require().Equal(numValidators, consistentSignatures,
		"Expected all %d validators to have consistent signatures across key managers, but only %d were consistent",
		numValidators, consistentSignatures)

	s.T().Logf("✅ Signature consistency validated: %d/%d validators have identical signatures across all key managers",
		consistentSignatures, numValidators)
}

// TestAttestationSlashing runs the complete attestation slashing test suite
func TestAttestationSlashing(t *testing.T) {
	suite.Run(t, new(AttestationSlashingTestSuite))
}
