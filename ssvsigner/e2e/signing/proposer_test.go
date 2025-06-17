package signing

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/suite"

	"github.com/ssvlabs/ssv/ssvsigner/e2e"
	"github.com/ssvlabs/ssv/ssvsigner/e2e/common"
)

// BlockSlashingTestSuite tests block proposal slashing protection functionality
type BlockSlashingTestSuite struct {
	e2e.E2ETestSuite
}

// TestBlockDoubleProposal validates slashing protection against double proposals.
// Ensures both SSV and Web3Signer prevent signing two different blocks for the same slot.
func (s *BlockSlashingTestSuite) TestBlockDoubleProposal() {
	s.T().Logf("🧪 Testing double proposal slashing protection")
	testCurrentEpoch := phase0.Epoch(4200)
	s.GetEnv().SetTestCurrentEpoch(testCurrentEpoch)

	ctx, cancel := context.WithTimeout(s.GetContext(), 60*time.Second)
	defer cancel()

	validatorKeyPair := s.AddValidator(ctx)

	s.T().Logf("✍️ Phase 1: Signing first valid block proposal")
	testEpoch := testCurrentEpoch + 1
	testSlot := s.GetEnv().GetMockBeacon().GetEpochFirstSlot(testEpoch) + 5
	initialBlock := common.NewTestDenebBlock(testSlot, 0)

	s.RequireValidSigning(
		ctx,
		initialBlock,
		validatorKeyPair.BLSPubKey,
		testSlot,
		spectypes.DomainProposer,
	)

	s.T().Logf("🚫 Phase 2: Attempting double proposal (should be rejected)")
	doubleProposalBlock := common.NewTestDenebBlock(testSlot, 0)
	doubleProposalBlock.ParentRoot = phase0.Root{0x02} // Different parent root

	s.RequireFailedSigning(
		ctx,
		doubleProposalBlock,
		validatorKeyPair.BLSPubKey,
		testSlot,
		spectypes.DomainProposer,
	)

	s.T().Logf("✅ Phase 3: Testing valid progression to higher slot")
	validNextSlot := testSlot + 10
	validProgressionBlock := common.NewTestDenebBlock(validNextSlot, 0)

	s.RequireValidSigning(
		ctx,
		validProgressionBlock,
		validatorKeyPair.BLSPubKey,
		validNextSlot,
		spectypes.DomainProposer,
	)
}

// TestBlockLowerSlotProposal validates slashing protection against lower slot proposals.
// Ensures both layers prevent signing blocks with slots lower than previously signed.
func (s *BlockSlashingTestSuite) TestBlockLowerSlotProposal() {
	s.T().Logf("🧪 Testing lower slot proposal slashing protection")
	testCurrentEpoch := phase0.Epoch(4200)
	s.GetEnv().SetTestCurrentEpoch(testCurrentEpoch)

	ctx, cancel := context.WithTimeout(s.GetContext(), 60*time.Second)
	defer cancel()

	validatorKeyPair := s.AddValidator(ctx)

	s.T().Logf("✍️ Phase 1: Signing block at higher slot")
	testEpoch := testCurrentEpoch + 10
	higherSlot := s.GetEnv().GetMockBeacon().GetEpochFirstSlot(testEpoch) + 15
	initialBlock := common.NewTestDenebBlock(higherSlot, 0)

	s.RequireValidSigning(
		ctx,
		initialBlock,
		validatorKeyPair.BLSPubKey,
		higherSlot,
		spectypes.DomainProposer,
	)

	s.T().Logf("🚫 Phase 2: Attempting lower slot proposal (should be rejected)")
	lowerSlot := higherSlot - 5
	lowerSlotBlock := common.NewTestDenebBlock(lowerSlot, 0)

	s.RequireFailedSigning(
		ctx,
		lowerSlotBlock,
		validatorKeyPair.BLSPubKey,
		lowerSlot,
		spectypes.DomainProposer,
	)

	s.T().Logf("✅ Phase 3: Testing valid progression to even higher slot")
	validNextSlot := higherSlot + 10
	validProgressionBlock := common.NewTestDenebBlock(validNextSlot, 0)

	s.RequireValidSigning(
		ctx,
		validProgressionBlock,
		validatorKeyPair.BLSPubKey,
		validNextSlot,
		spectypes.DomainProposer,
	)
}

// TestBlockValidProgression validates proper block proposal progression.
// Ensures valid sequential block proposals with increasing slots are signed successfully.
func (s *BlockSlashingTestSuite) TestBlockValidProgression() {
	s.T().Logf("🧪 Testing valid block proposal progression")
	testCurrentEpoch := phase0.Epoch(4200)
	s.GetEnv().SetTestCurrentEpoch(testCurrentEpoch)

	ctx, cancel := context.WithTimeout(s.GetContext(), 60*time.Second)
	defer cancel()

	validatorKeyPair := s.AddValidator(ctx)

	s.T().Logf("✍️ Phase 1: Creating sequence of valid progressive block proposals")
	startEpoch := testCurrentEpoch + 10
	startSlot := s.GetEnv().GetMockBeacon().GetEpochFirstSlot(startEpoch) + 1

	// Define a series of valid sequential block proposals with increasing slots
	blocks := make([]*deneb.BeaconBlock, 5)
	for i := 0; i < 5; i++ {
		slot := startSlot + phase0.Slot(i*10)
		blocks[i] = common.NewTestDenebBlock(slot, 0)
		blocks[i].ParentRoot = phase0.Root{byte(i + 1)}
	}

	s.T().Logf("✍️ Phase 2: Signing blocks sequentially")
	var allSigs []spectypes.Signature

	for _, block := range blocks {
		sig := s.RequireValidSigning(
			ctx,
			block,
			validatorKeyPair.BLSPubKey,
			block.Slot,
			spectypes.DomainProposer,
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

// TestConcurrentBlockSigning validates concurrent signing safety.
// Ensures SSV allows only one concurrent signature while Web3Signer allows all.
func (s *BlockSlashingTestSuite) TestConcurrentBlockSigning() {
	s.T().Logf("🔄 Testing concurrent block signing")
	testCurrentEpoch := phase0.Epoch(4200)
	s.GetEnv().SetTestCurrentEpoch(testCurrentEpoch)

	ctx, cancel := context.WithTimeout(s.GetContext(), 60*time.Second)
	defer cancel()

	validatorKeyPair := s.AddValidator(ctx)

	testEpoch := testCurrentEpoch + 10
	testSlot := s.GetEnv().GetMockBeacon().GetEpochFirstSlot(testEpoch) + 5
	block := common.NewTestDenebBlock(testSlot, 0)
	block.ParentRoot = phase0.Root{0x42} // Distinguish this concurrent test block

	s.T().Logf("🔀 Phase 1: Testing LocalKeyManager concurrent protection")
	numGoroutines := 12
	localResults := make(chan struct {
		sig  spectypes.Signature
		root phase0.Root
		err  error
	}, numGoroutines)

	domain, err := s.CalculateDomain(spectypes.DomainProposer, testEpoch)
	s.Require().NoError(err)

	// Launch multiple goroutines trying to sign the same block concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			sig, root, err := s.GetEnv().GetLocalKeyManager().SignBeaconObject(
				ctx,
				block,
				domain,
				validatorKeyPair.BLSPubKey,
				testSlot,
				spectypes.DomainProposer,
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

	// Launch multiple goroutines trying to sign the same block concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			sig, root, err := s.GetEnv().GetRemoteKeyManager().SignBeaconObject(
				ctx,
				block,
				domain,
				validatorKeyPair.BLSPubKey,
				testSlot,
				spectypes.DomainProposer,
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
	remoteSuccessCount := 0
	var validRemoteSig spectypes.Signature
	var validRemoteRoot phase0.Root

	for i, err := range remoteErrors {
		if err == nil {
			remoteSuccessCount++
			validRemoteSig = remoteSignatures[i]
			validRemoteRoot = remoteRoots[i]
		}
	}

	// RemoteKeyManager allows only 1 successful signature by design (slashing protection)
	s.Require().Equal(1, remoteSuccessCount)

	s.Require().Equal(validLocalSig, validRemoteSig)
	s.Require().Equal(validLocalRoot, validRemoteRoot)

	s.T().Logf("🔀 Phase 3: Testing Web3Signer concurrent protection")
	web3SignerResults := make(chan struct {
		sig  spectypes.Signature
		root phase0.Root
		err  error
	}, numGoroutines)

	// Launch multiple goroutines trying to sign the same block directly with Web3Signer
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			sig, root, err := s.SignWeb3Signer(ctx, block, domain, validatorKeyPair.BLSPubKey, testSlot, spectypes.DomainProposer)
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

	// Web3Signer allows same slot/parent_root
	s.Require().Equal(numGoroutines, len(web3SignerSignatures))
}

// TestBlockRestart validates slashing protection persistence across restarts.
// Tests Web3Signer, PostgreSQL, and SSV-Signer restart scenarios with block data.
func (s *BlockSlashingTestSuite) TestWeb3SignerRestart() {
	s.T().Logf("🔄 Testing block restart persistence")
	testCurrentEpoch := phase0.Epoch(4200)
	s.GetEnv().SetTestCurrentEpoch(testCurrentEpoch)

	ctx, cancel := context.WithTimeout(s.GetContext(), 60*time.Second)
	defer cancel()

	validatorKeyPair := s.AddValidator(ctx)

	s.T().Logf("✍️ Phase 1: Signing initial block before restart")
	testEpoch := testCurrentEpoch + 10
	testSlot := s.GetEnv().GetMockBeacon().GetEpochFirstSlot(testEpoch) + 5
	initialBlock := common.NewTestDenebBlock(testSlot, 0)

	s.RequireValidSigning(
		ctx,
		initialBlock,
		validatorKeyPair.BLSPubKey,
		testSlot,
		spectypes.DomainProposer,
	)

	s.T().Logf("🔄 Phase 2: Restarting Web3Signer container")
	err := s.GetEnv().RestartWeb3Signer()
	s.Require().NoError(err, "Failed to restart Web3Signer")

	s.T().Logf("🔍 Phase 3: Testing slashing protection persistence (should be rejected)")
	conflictingBlock := common.NewTestDenebBlock(testSlot, 0)
	conflictingBlock.ParentRoot = phase0.Root{0x02} // Same slot, different parent

	s.RequireFailedSigning(
		ctx,
		conflictingBlock,
		validatorKeyPair.BLSPubKey,
		testSlot,
		spectypes.DomainProposer,
	)

	s.T().Logf("✅ Phase 4: Testing valid operations after restart")
	higherTestSlot := testSlot + 10
	validBlock := common.NewTestDenebBlock(higherTestSlot, 0)

	s.RequireValidSigning(
		ctx,
		validBlock,
		validatorKeyPair.BLSPubKey,
		higherTestSlot,
		spectypes.DomainProposer,
	)
}

// TestPostgreSQLRestart validates slashing protection persistence across PostgreSQL restarts.
func (s *BlockSlashingTestSuite) TestPostgreSQLRestart() {
	s.T().Logf("💾 Testing PostgreSQL restart with block persistence")
	testCurrentEpoch := phase0.Epoch(4200)
	s.GetEnv().SetTestCurrentEpoch(testCurrentEpoch)

	ctx, cancel := context.WithTimeout(s.GetContext(), 60*time.Second)
	defer cancel()

	validatorKeyPair := s.AddValidator(ctx)

	s.T().Logf("✍️ Phase 1: Signing initial block to populate database")
	testEpoch := testCurrentEpoch + 10
	testSlot := s.GetEnv().GetMockBeacon().GetEpochFirstSlot(testEpoch) + 5
	initialBlock := common.NewTestDenebBlock(testSlot, 0)

	s.RequireValidSigning(
		ctx,
		initialBlock,
		validatorKeyPair.BLSPubKey,
		testSlot,
		spectypes.DomainProposer,
	)

	s.T().Logf("🔄 Phase 2: Restarting PostgreSQL database container with persistent volume")
	err := s.GetEnv().RestartPostgreSQL()
	s.Require().NoError(err, "Failed to restart PostgreSQL")

	s.T().Logf("🔍 Phase 3: Verifying data persistence - attempting conflicting block (should be rejected)")
	conflictingBlock := common.NewTestDenebBlock(testSlot, 0)
	conflictingBlock.ParentRoot = phase0.Root{0x02} // Same slot, different parent

	s.RequireFailedSigning(
		ctx,
		conflictingBlock,
		validatorKeyPair.BLSPubKey,
		testSlot,
		spectypes.DomainProposer,
	)

	s.T().Logf("✅ Phase 4: Signing new valid block with different slot")
	freshTestSlot := testSlot + 10
	freshBlock := common.NewTestDenebBlock(freshTestSlot, 0)

	s.RequireValidSigning(
		ctx,
		freshBlock,
		validatorKeyPair.BLSPubKey,
		freshTestSlot,
		spectypes.DomainProposer,
	)

	s.T().Logf("🛡️ Phase 5: Testing slashing protection with double proposal (should be rejected)")
	conflictingFreshBlock := common.NewTestDenebBlock(freshTestSlot, 0)
	conflictingFreshBlock.ParentRoot = phase0.Root{0x04} // Same slot, different parent

	s.RequireFailedSigning(
		ctx,
		conflictingFreshBlock,
		validatorKeyPair.BLSPubKey,
		freshTestSlot,
		spectypes.DomainProposer,
	)
}

// TestSSVSignerRestart validates SSV-Signer restart functionality with blocks.
func (s *BlockSlashingTestSuite) TestSSVSignerRestart() {
	s.T().Logf("🔄 Testing SSV-Signer restart with block functionality")
	testCurrentEpoch := phase0.Epoch(4200)
	s.GetEnv().SetTestCurrentEpoch(testCurrentEpoch)

	ctx, cancel := context.WithTimeout(s.GetContext(), 60*time.Second)
	defer cancel()

	validatorKeyPair := s.AddValidator(ctx)

	s.T().Logf("✍️ Phase 1: Signing initial block before restart")
	testEpoch := testCurrentEpoch + 10
	testSlot := s.GetEnv().GetMockBeacon().GetEpochFirstSlot(testEpoch) + 5
	initialBlock := common.NewTestDenebBlock(testSlot, 0)

	s.RequireValidSigning(
		ctx,
		initialBlock,
		validatorKeyPair.BLSPubKey,
		testSlot,
		spectypes.DomainProposer,
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
	newBlock := common.NewTestDenebBlock(higherTestSlot, 0)

	s.RequireValidSigning(
		ctx,
		newBlock,
		validatorKeyPair.BLSPubKey,
		higherTestSlot,
		spectypes.DomainProposer,
	)

	s.T().Logf("🛡️ Phase 5: Testing slashing protection after SSV-Signer restart (should be rejected)")
	conflictingBlock := common.NewTestDenebBlock(testSlot, 0)
	conflictingBlock.ParentRoot = phase0.Root{0x03} // Same slot as initial, different parent

	s.RequireFailedSigning(
		ctx,
		conflictingBlock,
		validatorKeyPair.BLSPubKey,
		testSlot,
		spectypes.DomainProposer,
	)
}

// TestBlockSlashing runs the complete block slashing test suite
func TestBlockSlashing(t *testing.T) {
	suite.Run(t, new(BlockSlashingTestSuite))
}
