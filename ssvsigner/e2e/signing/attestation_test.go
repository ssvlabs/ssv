package signing

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
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
	testEpoch := testCurrentEpoch + 1                                       // One epoch ahead of mocked current
	testSlot := s.GetEnv().GetMockBeacon().GetEpochFirstSlot(testEpoch) + 1 // First slot + 1 of the epoch
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
	validNextSlot := s.GetEnv().GetMockBeacon().GetEpochFirstSlot(validNextEpoch) + 1
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
	testSlot := s.GetEnv().GetMockBeacon().GetEpochFirstSlot(testEpoch) + 1
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
	validNextSlot := s.GetEnv().GetMockBeacon().GetEpochFirstSlot(validNextEpoch) + 1
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
	testSlot := s.GetEnv().GetMockBeacon().GetEpochFirstSlot(testEpoch) + 1
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
	validNextSlot := s.GetEnv().GetMockBeacon().GetEpochFirstSlot(validNextEpoch) + 1
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
	startSlot := s.GetEnv().GetMockBeacon().GetEpochFirstSlot(startEpoch) + 1

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
	testSlot := s.GetEnv().GetMockBeacon().GetEpochFirstSlot(testEpoch) + 5
	attestationData := common.NewTestAttestationData(testEpoch-1, testEpoch, testSlot)
	attestationData.BeaconBlockRoot = phase0.Root{0x42} // Distinguish this concurrent test attestation

	s.T().Logf("🔀 Phase 1: Testing LocalKeyManager concurrent protection")
	numGoroutines := 12
	localResults := make(chan struct {
		sig  spectypes.Signature
		root phase0.Root
		err  error
	}, numGoroutines)

	domain, err := s.CalculateDomain(spectypes.DomainAttester, testEpoch)
	s.Require().NoError(err)

	// Launch multiple goroutines trying to sign the same attestation concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			sig, root, err := s.GetEnv().GetLocalKeyManager().SignBeaconObject(
				ctx,
				attestationData,
				domain,
				validatorKeyPair.BLSPubKey,
				testSlot,
				spectypes.DomainAttester,
			)
			localResults <- struct {
				sig  spectypes.Signature
				root phase0.Root
				err  error
			}{sig, root, err}
		}(i)
	}

	// Collect results from all goroutines
	var localSignatures []spectypes.Signature
	var localRoots []phase0.Root
	var localErrors []error

	for i := 0; i < numGoroutines; i++ {
		result := <-localResults
		localSignatures = append(localSignatures, result.sig)
		localRoots = append(localRoots, result.root)
		localErrors = append(localErrors, result.err)
	}

	// Verify that only 1 operation was successful
	var validLocalSig spectypes.Signature
	var validLocalRoot phase0.Root
	successCount := 0

	for i, err := range localErrors {
		if err == nil {
			successCount++
			validLocalSig = localSignatures[i]
			validLocalRoot = localRoots[i]
		}
	}
	// LocalKeyManager allows only 1 successful signature by design (slashing protection)
	s.Require().Equal(1, successCount)

	s.T().Logf("🔀 Phase 2: Testing RemoteKeyManager concurrent protection")
	remoteResults := make(chan struct {
		sig  spectypes.Signature
		root phase0.Root
		err  error
	}, numGoroutines)

	// Launch multiple goroutines trying to sign the same attestation concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			sig, root, err := s.GetEnv().GetRemoteKeyManager().SignBeaconObject(
				ctx,
				attestationData,
				domain,
				validatorKeyPair.BLSPubKey,
				testSlot,
				spectypes.DomainAttester,
			)
			remoteResults <- struct {
				sig  spectypes.Signature
				root phase0.Root
				err  error
			}{sig, root, err}
		}(i)
	}

	// Collect results from all goroutines
	var remoteSignatures []spectypes.Signature
	var remoteRoots []phase0.Root
	var remoteErrors []error

	for i := 0; i < numGoroutines; i++ {
		result := <-remoteResults
		remoteSignatures = append(remoteSignatures, result.sig)
		remoteRoots = append(remoteRoots, result.root)
		remoteErrors = append(remoteErrors, result.err)
	}

	// Verify that only 1 operation was successful
	var validRemoteSig spectypes.Signature
	var validRemoteRoot phase0.Root
	remoteSuccessCount := 0

	for i, err := range remoteErrors {
		if err == nil {
			remoteSuccessCount++
			validRemoteSig = remoteSignatures[i]
			validRemoteRoot = remoteRoots[i]
		}
	}
	// RemoteKeyManager allows only 1 successful signature by design (slashing protection)
	s.Require().Equal(1, remoteSuccessCount)

	// Verify that LocalKeyManager and RemoteKeyManager produced identical results
	s.Require().Equal(validLocalSig, validRemoteSig)
	s.Require().Equal(validLocalRoot, validRemoteRoot)

	s.T().Logf("🔀 Phase 3: Testing Web3Signer concurrent protection")
	web3SignerResults := make(chan struct {
		sig  spectypes.Signature
		root phase0.Root
		err  error
	}, numGoroutines)

	// Launch multiple goroutines trying to sign the same attestation directly with Web3Signer
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			sig, root, err := s.SignWeb3Signer(ctx, attestationData, domain, validatorKeyPair.BLSPubKey, testSlot, spectypes.DomainAttester)
			web3SignerResults <- struct {
				sig  spectypes.Signature
				root phase0.Root
				err  error
			}{sig, root, err}
		}(i)
	}

	// Collect results from all Web3Signer goroutines
	var web3SignerSignatures []spectypes.Signature
	var web3SignerErrors []error

	for i := 0; i < numGoroutines; i++ {
		result := <-web3SignerResults
		if result.err == nil {
			web3SignerSignatures = append(web3SignerSignatures, result.sig)
		} else {
			web3SignerErrors = append(web3SignerErrors, result.err)
		}
	}

	s.Require().Empty(web3SignerErrors)

	// Verify all successful signatures are identical (deterministic signing)
	for _, web3sig := range web3SignerSignatures {
		s.Require().Equal(validLocalSig, web3sig)
	}

	// Web3Signer allows same source/target/block root (no slashing protection violation)
	s.Require().Equal(numGoroutines, len(web3SignerSignatures))
}

// TestWeb3SignerRestart validates slashing protection persistence across restarts.
// Tests Web3Signer, PostgreSQL, and SSV-Signer restart scenarios with attestation data.
func (s *AttestationSlashingTestSuite) TestWeb3SignerRestart() {
	s.T().Logf("🔄 Testing attestation restart persistence")
	testCurrentEpoch := phase0.Epoch(4200)
	s.GetEnv().SetTestCurrentEpoch(testCurrentEpoch)

	ctx, cancel := context.WithTimeout(s.GetContext(), 60*time.Second)
	defer cancel()

	validatorKeyPair := s.AddValidator(ctx)

	s.T().Logf("✍️ Phase 1: Signing initial attestation before restart")
	testEpoch := testCurrentEpoch + 10
	testSlot := s.GetEnv().GetMockBeacon().GetEpochFirstSlot(testEpoch) + 5
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
	higherTestSlot := s.GetEnv().GetMockBeacon().GetEpochFirstSlot(higherTestEpoch) + 1
	validAttestationData := common.NewTestAttestationData(testEpoch+2, higherTestEpoch, higherTestSlot)

	s.RequireValidSigning(
		ctx,
		validAttestationData,
		validatorKeyPair.BLSPubKey,
		higherTestSlot,
		spectypes.DomainAttester,
	)
}

// TestPostgreSQLRestart validates slashing protection persistence across PostgreSQL restarts.
func (s *AttestationSlashingTestSuite) TestPostgreSQLRestart() {
	s.T().Logf("💾 Testing PostgreSQL restart with attestation persistence")
	testCurrentEpoch := phase0.Epoch(4200)
	s.GetEnv().SetTestCurrentEpoch(testCurrentEpoch)

	ctx, cancel := context.WithTimeout(s.GetContext(), 60*time.Second)
	defer cancel()

	validatorKeyPair := s.AddValidator(ctx)

	s.T().Logf("✍️ Phase 1: Signing initial attestation to populate database")
	testEpoch := testCurrentEpoch + 10
	testSlot := s.GetEnv().GetMockBeacon().GetEpochFirstSlot(testEpoch) + 5
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
	freshTestSlot := s.GetEnv().GetMockBeacon().GetEpochFirstSlot(freshTestEpoch) + 1
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
	testSlot := s.GetEnv().GetMockBeacon().GetEpochFirstSlot(testEpoch) + 5
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

// TestAttestationSlashing runs the complete attestation slashing test suite
func TestAttestationSlashing(t *testing.T) {
	suite.Run(t, new(AttestationSlashingTestSuite))
}
