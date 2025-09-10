package signing

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/electra"
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
	testSlot := s.GetEnv().GetBeaconConfig().FirstSlotAtEpoch(testEpoch) + 5
	initialBlock := common.NewTestBlock(testSlot, 0)

	s.RequireValidSigning(
		ctx,
		initialBlock,
		validatorKeyPair.BLSPubKey,
		testSlot,
		spectypes.DomainProposer,
	)

	s.T().Logf("🚫 Phase 2: Attempting double proposal (should be rejected)")
	doubleProposalBlock := common.NewTestBlock(testSlot, 0)
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
	validProgressionBlock := common.NewTestBlock(validNextSlot, 0)

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
	testEpoch := testCurrentEpoch + 1
	higherSlot := s.GetEnv().GetBeaconConfig().FirstSlotAtEpoch(testEpoch) + 15
	initialBlock := common.NewTestBlock(higherSlot, 0)

	s.RequireValidSigning(
		ctx,
		initialBlock,
		validatorKeyPair.BLSPubKey,
		higherSlot,
		spectypes.DomainProposer,
	)

	s.T().Logf("🚫 Phase 2: Attempting lower slot proposal (should be rejected)")
	lowerSlot := higherSlot - 5
	lowerSlotBlock := common.NewTestBlock(lowerSlot, 0)

	s.RequireFailedSigning(
		ctx,
		lowerSlotBlock,
		validatorKeyPair.BLSPubKey,
		lowerSlot,
		spectypes.DomainProposer,
	)

	s.T().Logf("✅ Phase 3: Testing valid progression to even higher slot")
	validNextSlot := higherSlot + 10
	validProgressionBlock := common.NewTestBlock(validNextSlot, 0)

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
	startEpoch := testCurrentEpoch + 1
	startSlot := s.GetEnv().GetBeaconConfig().FirstSlotAtEpoch(startEpoch) + 1

	// Define a series of valid sequential block proposals with increasing slots
	blocks := make([]*electra.BeaconBlock, 5)
	for i := 0; i < 5; i++ {
		slot := startSlot + phase0.Slot(i*10)
		blocks[i] = common.NewTestBlock(slot, 0)
		blocks[i].ParentRoot = phase0.Root{byte(i + 1)}
	}

	s.T().Logf("✍️ Phase 2: Signing blocks sequentially")

	allSigs := make([]spectypes.Signature, 0, len(blocks))

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

	testEpoch := testCurrentEpoch + 1
	testSlot := s.GetEnv().GetBeaconConfig().FirstSlotAtEpoch(testEpoch) + 5
	block := common.NewTestBlock(testSlot, 0)
	block.ParentRoot = phase0.Root{0x42} // Distinguish this concurrent test block

	domain, err := s.CalculateDomain(spectypes.DomainProposer, testEpoch)
	s.Require().NoError(err)

	numGoroutines := 12

	s.T().Logf("🔀 Phase 1: Testing LocalKeyManager concurrent protection")
	localSuccessCount := s.RunConcurrentSigning(ctx, numGoroutines, "LocalKeyManager", func() (spectypes.Signature, phase0.Root, error) {
		return s.GetEnv().GetLocalKeyManager().SignBeaconObject(
			ctx,
			block,
			domain,
			validatorKeyPair.BLSPubKey,
			testSlot,
			spectypes.DomainProposer,
		)
	})

	// LocalKeyManager allows only 1 successful signature by design (slashing protection)
	s.Require().Equal(1, localSuccessCount)

	s.T().Logf("🔀 Phase 2: Testing RemoteKeyManager concurrent protection")
	remoteSuccessCount := s.RunConcurrentSigning(ctx, numGoroutines, "RemoteKeyManager", func() (spectypes.Signature, phase0.Root, error) {
		return s.GetEnv().GetRemoteKeyManager().SignBeaconObject(
			ctx,
			block,
			domain,
			validatorKeyPair.BLSPubKey,
			testSlot,
			spectypes.DomainProposer,
		)
	})

	// RemoteKeyManager allows only 1 successful signature by design (slashing protection)
	s.Require().Equal(1, remoteSuccessCount)

	s.T().Logf("🔀 Phase 3: Testing Web3Signer concurrent protection")
	web3SignerSuccessCount := s.RunConcurrentSigning(ctx, numGoroutines, "Web3Signer", func() (spectypes.Signature, phase0.Root, error) {
		return s.SignWeb3Signer(
			ctx,
			block,
			domain,
			validatorKeyPair.BLSPubKey,
			testSlot,
			spectypes.DomainProposer,
		)
	})

	// Web3Signer allows same slot/parent_root (no slashing protection violation)
	s.Require().Equal(numGoroutines, web3SignerSuccessCount)
}

// TestBlockRestart validates slashing protection persistence across restarts.
func (s *BlockSlashingTestSuite) TestWeb3SignerRestart() {
	s.T().Logf("🔄 Testing block restart persistence")
	testCurrentEpoch := phase0.Epoch(4200)
	s.GetEnv().SetTestCurrentEpoch(testCurrentEpoch)

	ctx, cancel := context.WithTimeout(s.GetContext(), 60*time.Second)
	defer cancel()

	validatorKeyPair := s.AddValidator(ctx)

	s.T().Logf("✍️ Phase 1: Signing initial block before restart")
	testEpoch := testCurrentEpoch + 1
	testSlot := s.GetEnv().GetBeaconConfig().FirstSlotAtEpoch(testEpoch) + 5
	initialBlock := common.NewTestBlock(testSlot, 0)

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
	conflictingBlock := common.NewTestBlock(testSlot, 0)
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
	validBlock := common.NewTestBlock(higherTestSlot, 0)

	s.RequireValidSigning(
		ctx,
		validBlock,
		validatorKeyPair.BLSPubKey,
		higherTestSlot,
		spectypes.DomainProposer,
	)
}

// TestWeb3SignerPostgreSQLRestart validates slashing protection persistence across PostgreSQL restarts.
func (s *BlockSlashingTestSuite) TestWeb3SignerPostgreSQLRestart() {
	s.T().Logf("💾 Testing Web3Signer PostgreSQL restart with block persistence")
	testCurrentEpoch := phase0.Epoch(4200)
	s.GetEnv().SetTestCurrentEpoch(testCurrentEpoch)

	ctx, cancel := context.WithTimeout(s.GetContext(), 60*time.Second)
	defer cancel()

	validatorKeyPair := s.AddValidator(ctx)

	s.T().Logf("✍️ Phase 1: Signing initial block to populate database")
	testEpoch := testCurrentEpoch + 1
	testSlot := s.GetEnv().GetBeaconConfig().FirstSlotAtEpoch(testEpoch) + 5
	initialBlock := common.NewTestBlock(testSlot, 0)

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
	conflictingBlock := common.NewTestBlock(testSlot, 0)
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
	freshBlock := common.NewTestBlock(freshTestSlot, 0)

	s.RequireValidSigning(
		ctx,
		freshBlock,
		validatorKeyPair.BLSPubKey,
		freshTestSlot,
		spectypes.DomainProposer,
	)

	s.T().Logf("🛡️ Phase 5: Testing slashing protection with double proposal (should be rejected)")
	conflictingFreshBlock := common.NewTestBlock(freshTestSlot, 0)
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
	testEpoch := testCurrentEpoch + 1
	testSlot := s.GetEnv().GetBeaconConfig().FirstSlotAtEpoch(testEpoch) + 5
	initialBlock := common.NewTestBlock(testSlot, 0)

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
	newBlock := common.NewTestBlock(higherTestSlot, 0)

	s.RequireValidSigning(
		ctx,
		newBlock,
		validatorKeyPair.BLSPubKey,
		higherTestSlot,
		spectypes.DomainProposer,
	)

	s.T().Logf("🛡️ Phase 5: Testing slashing protection after SSV-Signer restart (should be rejected)")
	conflictingBlock := common.NewTestBlock(testSlot, 0)
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
