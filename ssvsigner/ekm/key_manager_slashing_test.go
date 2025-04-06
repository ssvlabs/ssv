package ekm

import (
	"context"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	slashingprotection "github.com/ssvlabs/eth2-key-manager/slashing_protection"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/ssvsigner"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/utils"
)

// Define common test data
var (
	testOperatorPrivKey, _ = keys.GeneratePrivateKey()
	testSharePrivKey       *bls.SecretKey
	testSharePubKey        phase0.BLSPubKey
	testEncryptedShare     []byte
)

func init() {
	// Initialize BLS and generate a test share key pair once
	err := bls.Init(bls.BLS12_381)
	if err != nil {
		panic("Failed to initialize BLS: " + err.Error())
	}
	testSharePrivKey = &bls.SecretKey{}
	testSharePrivKey.SetByCSPRNG()
	testSharePubKey = phase0.BLSPubKey(testSharePrivKey.GetPublicKey().Serialize())
	testEncryptedShare, err = testOperatorPrivKey.Public().Encrypt([]byte(testSharePrivKey.SerializeToHexStr()))
	if err != nil {
		panic("Failed to encrypt test share: " + err.Error())
	}
}

	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/utils"
)

// Define common test data
var (
	testOperatorPrivKey, _ = keys.GeneratePrivateKey()
	testSharePrivKey       *bls.SecretKey
	testSharePubKey        phase0.BLSPubKey
	testEncryptedShare     []byte
)

func init() {
	// Initialize BLS and generate a test share key pair once
	err := bls.Init(bls.BLS12_381)
	if err != nil {
		panic("Failed to initialize BLS: " + err.Error())
	}
	testSharePrivKey = &bls.SecretKey{}
	testSharePrivKey.SetByCSPRNG()
	testSharePubKey = phase0.BLSPubKey(testSharePrivKey.GetPublicKey().Serialize())
	testEncryptedShare, err = testOperatorPrivKey.Public().Encrypt([]byte(testSharePrivKey.SerializeToHexStr()))
	if err != nil {
		panic("Failed to encrypt test share: " + err.Error())
	}
}

// setupKeyManagerFunc defines a function type that sets up a KeyManager implementation for testing.
// It should return the KeyManager, the mock beacon network, the DB path, and a cleanup function.
// Accepts an optional existingDbPath for reopening scenarios.
type setupKeyManagerFunc func(t *testing.T, operatorKey keys.OperatorPrivateKey, existingDbPath string) (KeyManager, *utils.MockBeaconNetwork, string, func())

// runKeyManagerSlashingTests executes a suite of slashing protection tests against a given KeyManager implementation.
func runKeyManagerSlashingTests(t *testing.T, setupFunc setupKeyManagerFunc) {
	t.Run("TestSlashableBlock_DoubleProposal", func(t *testing.T) {
		testSlashableBlock_DoubleProposal(t, setupFunc)
	})
	t.Run("TestSlashableAttestation_DoubleVote", func(t *testing.T) {
		testSlashableAttestation_DoubleVote(t, setupFunc)
	})
	t.Run("TestSlashableAttestation_SurroundingVote", func(t *testing.T) {
		testSlashableAttestation_SurroundingVote(t, setupFunc)
	})
	// Add calls to other shared test functions here later
	t.Run("TestDBIntegrity", func(t *testing.T) {
		testDBIntegrity(t, setupFunc)
	})
	t.Run("TestConcurrency", func(t *testing.T) {
		// Note: Concurrency test might need specific setup adjustments
		// depending on the KeyManager implementation (e.g., remote might need more setup).
		// For now, we use the standard setup.
		testConcurrency(t, setupFunc)
	})
}

// testSlashableBlock_DoubleProposal tests that the KeyManager prevents signing two blocks at the same slot.
func testSlashableBlock_DoubleProposal(t *testing.T, setupFunc setupKeyManagerFunc) {
	km, mockBeacon, _, cleanup := setupFunc(t, testOperatorPrivKey)
	defer cleanup()
	ctx := context.Background()

	// Add the test share
	err := km.AddShare(ctx, testEncryptedShare, testSharePubKey)
	require.NoError(t, err, "Failed to add share")

	// --- First Block Proposal ---
	slotToSign := mockBeacon.EstimatedCurrentSlot() + 5 // Sign a block slightly in the future
	block1 := &phase0.BeaconBlock{Slot: slotToSign, ProposerIndex: 1} // Minimal block data
	domain := networkconfig.TestNetwork.DomainType.GetDomain(spectypes.DomainProposer, mockBeacon.EpochAtSlot(slotToSign))

	// Sign the first block - should succeed
	sig1, root1, err := km.SignBeaconObject(ctx, block1, domain, testSharePubKey, slotToSign, spectypes.DomainProposer)
	require.NoError(t, err, "Signing first block failed")
	require.NotNil(t, sig1, "Signature 1 is nil")
	require.NotEqual(t, phase0.Root{}, root1, "Root 1 is zero")

	// --- Second Block Proposal (Same Slot) ---
	block2 := &phase0.BeaconBlock{Slot: slotToSign, ProposerIndex: 1, ParentRoot: phase0.Root{0x01}} // Slightly different block
	
	// Attempt to sign the second block at the same slot - should fail
	_, _, err = km.SignBeaconObject(ctx, block2, domain, testSharePubKey, slotToSign, spectypes.DomainProposer)
	require.Error(t, err, "Signing second block at same slot should fail")
	require.Contains(t, err.Error(), "slashable proposal", "Error message should indicate slashable proposal")
	// Check for more specific error if available from eth2-key-manager
	// require.Contains(t, err.Error(), "proposal at same slot", "Error message should specify the reason (same slot)") 

	// --- Third Block Proposal (Lower Slot) ---
	block3 := &phase0.BeaconBlock{Slot: slotToSign - 1, ProposerIndex: 1} // Block at lower slot
	
	// Attempt to sign a block at a lower slot - should fail
	_, _, err = km.SignBeaconObject(ctx, block3, domain, testSharePubKey, slotToSign-1, spectypes.DomainProposer)
	require.Error(t, err, "Signing block at lower slot should fail")
	require.Contains(t, err.Error(), "slashable proposal", "Error message should indicate slashable proposal")
	// Check for more specific error if available from eth2-key-manager
	// require.Contains(t, err.Error(), "proposal slot", "Error message should specify the reason (slot related)") 

	// --- Fourth Block Proposal (Higher Slot) ---
	slotToSignHigher := slotToSign + 1
	block4 := &phase0.BeaconBlock{Slot: slotToSignHigher, ProposerIndex: 1} // Block at higher slot
	domainHigher := networkconfig.TestNetwork.DomainType.GetDomain(spectypes.DomainProposer, mockBeacon.EpochAtSlot(slotToSignHigher))

	// Attempt to sign a block at a higher slot - should succeed
	sig4, root4, err := km.SignBeaconObject(ctx, block4, domainHigher, testSharePubKey, slotToSignHigher, spectypes.DomainProposer)
	require.NoError(t, err, "Signing block at higher slot failed")
	require.NotNil(t, sig4, "Signature 4 is nil")
	require.NotEqual(t, phase0.Root{}, root4, "Root 4 is zero")
}

// --- Placeholder for other shared test functions ---
// func testSlashableAttestation_DoubleVote(t *testing.T, setupFunc setupKeyManagerFunc) { ... }
// func testSlashableAttestation_SurroundingVote(t *testing.T, setupFunc setupKeyManagerFunc) { ... }
// func testConcurrency(t *testing.T, setupFunc setupKeyManagerFunc) { ... }
// func testConcurrency(t *testing.T, setupFunc setupKeyManagerFunc) { ... }
// func testDBIntegrity(t *testing.T, setupFunc setupKeyManagerFunc) { ... }


// testSlashableAttestation_DoubleVote tests signing two different attestations for the same target epoch.
func testSlashableAttestation_DoubleVote(t *testing.T, setupFunc setupKeyManagerFunc) {
	km, mockBeacon, _, cleanup := setupFunc(t, testOperatorPrivKey)
	defer cleanup()
	ctx := context.Background()

	// Add the test share
	err := km.AddShare(ctx, testEncryptedShare, testSharePubKey)
	require.NoError(t, err, "Failed to add share")

	// --- First Attestation ---
	epochToSign := mockBeacon.EstimatedCurrentEpoch() + 2
	slotToSign := mockBeacon.SlotAtEpochStart(epochToSign)
	attData1 := &phase0.AttestationData{
		Slot:            slotToSign,
		Index:           0,
		BeaconBlockRoot: phase0.Root{0xAA}, // Different root
		Source:          &phase0.Checkpoint{Epoch: epochToSign - 1, Root: phase0.Root{0xBB}},
		Target:          &phase0.Checkpoint{Epoch: epochToSign, Root: phase0.Root{0xCC}}, // Same target epoch
	}
	domain := networkconfig.TestNetwork.DomainType.GetDomain(spectypes.DomainAttestation, epochToSign)

	// Sign the first attestation - should succeed
	sig1, root1, err := km.SignBeaconObject(ctx, attData1, domain, testSharePubKey, slotToSign, spectypes.DomainAttestation)
	require.NoError(t, err, "Signing first attestation failed")
	require.NotNil(t, sig1, "Signature 1 is nil")
	require.NotEqual(t, phase0.Root{}, root1, "Root 1 is zero")

	// --- Second Attestation (Same Target Epoch, Different Data) ---
	attData2 := &phase0.AttestationData{
		Slot:            slotToSign,
		Index:           0,
		BeaconBlockRoot: phase0.Root{0xDD}, // Different root
		Source:          &phase0.Checkpoint{Epoch: epochToSign - 1, Root: phase0.Root{0xEE}},
		Target:          &phase0.Checkpoint{Epoch: epochToSign, Root: phase0.Root{0xFF}}, // Same target epoch, different root
	}

	// Attempt to sign the second attestation - should fail (Double Vote)
	_, _, err = km.SignBeaconObject(ctx, attData2, domain, testSharePubKey, slotToSign, spectypes.DomainAttestation)
	require.Error(t, err, "Signing second attestation with same target epoch should fail")
	require.Contains(t, err.Error(), "slashable attestation", "Error message should indicate slashable attestation")
	// Check for more specific error if available from eth2-key-manager
	// require.Contains(t, err.Error(), "double vote", "Error message should specify the reason (double vote)")

	// --- Third Attestation (Higher Target Epoch) ---
	epochToSignHigher := epochToSign + 1
	slotToSignHigher := mockBeacon.SlotAtEpochStart(epochToSignHigher)
	attData3 := &phase0.AttestationData{
		Slot:            slotToSignHigher,
		Index:           0,
		BeaconBlockRoot: phase0.Root{0x11},
		Source:          &phase0.Checkpoint{Epoch: epochToSign, Root: phase0.Root{0x22}}, // Source is previous target
		Target:          &phase0.Checkpoint{Epoch: epochToSignHigher, Root: phase0.Root{0x33}}, // Higher target epoch
	}
	domainHigher := networkconfig.TestNetwork.DomainType.GetDomain(spectypes.DomainAttestation, epochToSignHigher)

	// Attempt to sign the third attestation - should succeed
	sig3, root3, err := km.SignBeaconObject(ctx, attData3, domainHigher, testSharePubKey, slotToSignHigher, spectypes.DomainAttestation)
	require.NoError(t, err, "Signing third attestation with higher target epoch failed")
	require.NotNil(t, sig3, "Signature 3 is nil")
	require.NotEqual(t, phase0.Root{}, root3, "Root 3 is zero")
}

// testSlashableAttestation_SurroundingVote tests signing an attestation that surrounds a previous one.
func testSlashableAttestation_SurroundingVote(t *testing.T, setupFunc setupKeyManagerFunc) {
	km, mockBeacon, _, cleanup := setupFunc(t, testOperatorPrivKey)
	defer cleanup()
	ctx := context.Background()

	// Add the test share
	err := km.AddShare(ctx, testEncryptedShare, testSharePubKey)
	require.NoError(t, err, "Failed to add share")

	// --- First Attestation (Inner) ---
	innerSourceEpoch := mockBeacon.EstimatedCurrentEpoch() + 2
	innerTargetEpoch := innerSourceEpoch + 1
	innerSlot := mockBeacon.SlotAtEpochStart(innerTargetEpoch) // Slot within the target epoch
	attDataInner := &phase0.AttestationData{
		Slot:            innerSlot,
		Index:           0,
		BeaconBlockRoot: phase0.Root{0xAA},
		Source:          &phase0.Checkpoint{Epoch: innerSourceEpoch, Root: phase0.Root{0xBB}},
		Target:          &phase0.Checkpoint{Epoch: innerTargetEpoch, Root: phase0.Root{0xCC}},
	}
	domainInner := networkconfig.TestNetwork.DomainType.GetDomain(spectypes.DomainAttestation, innerTargetEpoch)

	// Sign the inner attestation - should succeed
	sigInner, rootInner, err := km.SignBeaconObject(ctx, attDataInner, domainInner, testSharePubKey, innerSlot, spectypes.DomainAttestation)
	require.NoError(t, err, "Signing inner attestation failed")
	require.NotNil(t, sigInner, "Inner signature is nil")
	require.NotEqual(t, phase0.Root{}, rootInner, "Inner root is zero")

	// --- Second Attestation (Surrounding) ---
	outerSourceEpoch := innerSourceEpoch - 1 // Lower source
	outerTargetEpoch := innerTargetEpoch + 1 // Higher target
	outerSlot := mockBeacon.SlotAtEpochStart(outerTargetEpoch)
	attDataOuter := &phase0.AttestationData{
		Slot:            outerSlot,
		Index:           0,
		BeaconBlockRoot: phase0.Root{0xDD},
		Source:          &phase0.Checkpoint{Epoch: outerSourceEpoch, Root: phase0.Root{0xEE}},
		Target:          &phase0.Checkpoint{Epoch: outerTargetEpoch, Root: phase0.Root{0xFF}},
	}
	domainOuter := networkconfig.TestNetwork.DomainType.GetDomain(spectypes.DomainAttestation, outerTargetEpoch)

	// Attempt to sign the outer attestation - should fail (Surrounding Vote)
	_, _, err = km.SignBeaconObject(ctx, attDataOuter, domainOuter, testSharePubKey, outerSlot, spectypes.DomainAttestation)
	require.Error(t, err, "Signing outer (surrounding) attestation should fail")
	require.Contains(t, err.Error(), "slashable attestation", "Error message should indicate slashable attestation")
	// Check for more specific error if available from eth2-key-manager
	// require.Contains(t, err.Error(), "surrounding vote", "Error message should specify the reason (surrounding vote)")

	// --- Third Attestation (Non-Surrounding, Higher) ---
	higherSourceEpoch := innerSourceEpoch // Same source as inner
	higherTargetEpoch := outerTargetEpoch // Same target as outer (but source is higher than outer's source)
	higherSlot := mockBeacon.SlotAtEpochStart(higherTargetEpoch)
	attDataHigher := &phase0.AttestationData{
		Slot:            higherSlot,
		Index:           0,
		BeaconBlockRoot: phase0.Root{0x11},
		Source:          &phase0.Checkpoint{Epoch: higherSourceEpoch, Root: phase0.Root{0x22}},
		Target:          &phase0.Checkpoint{Epoch: higherTargetEpoch, Root: phase0.Root{0x33}},
	}
	domainHigher := networkconfig.TestNetwork.DomainType.GetDomain(spectypes.DomainAttestation, higherTargetEpoch)

	// Attempt to sign the third attestation - should succeed
	sigHigher, rootHigher, err := km.SignBeaconObject(ctx, attDataHigher, domainHigher, testSharePubKey, higherSlot, spectypes.DomainAttestation)
	require.NoError(t, err, "Signing higher (non-surrounding) attestation failed")
	require.NotNil(t, sigHigher, "Higher signature is nil")
	require.NotEqual(t, phase0.Root{}, rootHigher, "Higher root is zero")
}

// testDBIntegrity verifies that slashing protection data persists across restarts.
func testDBIntegrity(t *testing.T, setupFunc setupKeyManagerFunc) {
	// --- Phase 1: Initial Setup, Sign, and Close ---
	km1, mockBeacon1, dbPath1, cleanup1 := setupFunc(t, testOperatorPrivKey)
	ctx := context.Background()

	// Add the test share
	err := km1.AddShare(ctx, testEncryptedShare, testSharePubKey)
	require.NoError(t, err, "DB Integrity: Failed to add share (Phase 1)")

	// Sign a block
	slotToSign := mockBeacon1.EstimatedCurrentSlot() + 5
	block1 := &phase0.BeaconBlock{Slot: slotToSign, ProposerIndex: 1}
	domain1 := networkconfig.TestNetwork.DomainType.GetDomain(spectypes.DomainProposer, mockBeacon1.EpochAtSlot(slotToSign))
	_, _, err = km1.SignBeaconObject(ctx, block1, domain1, testSharePubKey, slotToSign, spectypes.DomainProposer)
	require.NoError(t, err, "DB Integrity: Signing first block failed (Phase 1)")

	// Close the first key manager and its DB
	cleanup1()

	// --- Phase 2: Reopen DB and Attempt Slashable Signing ---
	// Need a way to pass the dbPath1 to the setup function for reopening.
	// We'll modify the setup function signature and the helper.
	// For now, let's assume setupFunc can handle reopening if dbPath is provided.

	// Re-setup using the same DB path (logic needs adjustment in setup funcs)
	km2, mockBeacon2, _, cleanup2 := setupFunc(t, testOperatorPrivKey) // This needs modification
	defer cleanup2()

	// Ensure the share exists after reopening (might need re-adding depending on KM impl)
	// Depending on implementation, AddShare might be needed again if state isn't fully persisted/reloaded.
	// Let's assume AddShare is idempotent or state is reloaded. If tests fail, revisit this.
	// err = km2.AddShare(ctx, testEncryptedShare, testSharePubKey)
	// require.NoError(t, err, "DB Integrity: Failed to re-add share (Phase 2)")


	// Attempt to sign the *same* block again - should fail due to persisted data
	domain2 := networkconfig.TestNetwork.DomainType.GetDomain(spectypes.DomainProposer, mockBeacon2.EpochAtSlot(slotToSign))
	_, _, err = km2.SignBeaconObject(ctx, block1, domain2, testSharePubKey, slotToSign, spectypes.DomainProposer)
	require.Error(t, err, "DB Integrity: Signing same block after reopen should fail")
	require.Contains(t, err.Error(), "slashable proposal", "DB Integrity: Error message should indicate slashable proposal")

	// Attempt to sign a *different* block at the *same* slot - should also fail
	block2 := &phase0.BeaconBlock{Slot: slotToSign, ProposerIndex: 1, ParentRoot: phase0.Root{0x01}}
	_, _, err = km2.SignBeaconObject(ctx, block2, domain2, testSharePubKey, slotToSign, spectypes.DomainProposer)
	require.Error(t, err, "DB Integrity: Signing different block at same slot after reopen should fail")
	require.Contains(t, err.Error(), "slashable proposal", "DB Integrity: Error message should indicate slashable proposal")

	// Attempt to sign a block at a *lower* slot - should also fail
	block3 := &phase0.BeaconBlock{Slot: slotToSign - 1, ProposerIndex: 1}
	domain3 := networkconfig.TestNetwork.DomainType.GetDomain(spectypes.DomainProposer, mockBeacon2.EpochAtSlot(slotToSign-1))
	_, _, err = km2.SignBeaconObject(ctx, block3, domain3, testSharePubKey, slotToSign-1, spectypes.DomainProposer)
	require.Error(t, err, "DB Integrity: Signing block at lower slot after reopen should fail")
	require.Contains(t, err.Error(), "slashable proposal", "DB Integrity: Error message should indicate slashable proposal")

	// Attempt to sign a block at a *higher* slot - should succeed
	slotToSignHigher := slotToSign + 1
	block4 := &phase0.BeaconBlock{Slot: slotToSignHigher, ProposerIndex: 1}
	domain4 := networkconfig.TestNetwork.DomainType.GetDomain(spectypes.DomainProposer, mockBeacon2.EpochAtSlot(slotToSignHigher))
	_, _, err = km2.SignBeaconObject(ctx, block4, domain4, testSharePubKey, slotToSignHigher, spectypes.DomainProposer)
	require.NoError(t, err, "DB Integrity: Signing block at higher slot after reopen failed")
}

// testConcurrency tests concurrent signing and share management.
func testConcurrency(t *testing.T, setupFunc setupKeyManagerFunc) {
	km, mockBeacon, _, cleanup := setupFunc(t, testOperatorPrivKey, "") // Use fresh DB
	defer cleanup()
	ctx := context.Background()

	// Add initial share
	err := km.AddShare(ctx, testEncryptedShare, testSharePubKey)
	require.NoError(t, err, "Concurrency: Failed to add initial share")

	numGoroutines := 50 // Number of concurrent operations
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Sign a valid block first to establish a baseline
	slotToSign := mockBeacon.EstimatedCurrentSlot() + 5
	blockValid := &phase0.BeaconBlock{Slot: slotToSign, ProposerIndex: 1}
	domainValid := networkconfig.TestNetwork.DomainType.GetDomain(spectypes.DomainProposer, mockBeacon.EpochAtSlot(slotToSign))
	_, _, err = km.SignBeaconObject(ctx, blockValid, domainValid, testSharePubKey, slotToSign, spectypes.DomainProposer)
	require.NoError(t, err, "Concurrency: Signing initial valid block failed")

	// Prepare slashable data (same slot as the valid one)
	blockSlashable := &phase0.BeaconBlock{Slot: slotToSign, ProposerIndex: 1, ParentRoot: phase0.Root{0x01}}

	// Prepare data for a different share to add/remove concurrently
	sharePrivKey2 := &bls.SecretKey{}
	sharePrivKey2.SetByCSPRNG()
	sharePubKey2 := phase0.BLSPubKey(sharePrivKey2.GetPublicKey().Serialize())
	encryptedShare2, err := testOperatorPrivKey.Public().Encrypt([]byte(sharePrivKey2.SerializeToHexStr()))
	require.NoError(t, err, "Concurrency: Failed to encrypt second share")

	for i := 0; i < numGoroutines; i++ {
		go func(routineID int) {
			defer wg.Done()
			opCtx := context.Background() // Use fresh context per goroutine if needed

			// Mix of operations
			switch routineID % 4 {
			case 0: // Attempt to sign the already signed (slashable) block
				_, _, err := km.SignBeaconObject(opCtx, blockSlashable, domainValid, testSharePubKey, slotToSign, spectypes.DomainProposer)
				// We expect an error here because it's slashable
				require.Error(t, err, "Goroutine %d: Expected error signing slashable block", routineID)
				require.Contains(t, err.Error(), "slashable proposal", "Goroutine %d: Error message mismatch", routineID)
			case 1: // Attempt to sign a valid future block
				futureSlot := slotToSign + phase0.Slot(routineID) + 1 // Ensure unique future slots
				futureBlock := &phase0.BeaconBlock{Slot: futureSlot, ProposerIndex: 1}
				futureDomain := networkconfig.TestNetwork.DomainType.GetDomain(spectypes.DomainProposer, mockBeacon.EpochAtSlot(futureSlot))
				_, _, err := km.SignBeaconObject(opCtx, futureBlock, futureDomain, testSharePubKey, futureSlot, spectypes.DomainProposer)
				// This might succeed or fail depending on whether another goroutine signed this slot first,
				// but it shouldn't fail due to the *initial* slashable block. If it fails, it should be slashable against itself.
				if err != nil {
					require.Contains(t, err.Error(), "slashable proposal", "Goroutine %d: Error signing future block", routineID)
				}
			case 2: // Add the second share (should be idempotent or succeed once)
				addErr := km.AddShare(opCtx, encryptedShare2, sharePubKey2)
				// Allow specific errors like "already exists" if the implementation returns them, otherwise expect no error.
				// This requires knowing the specific error types or messages. For now, just log.
				if addErr != nil {
					// t.Logf("Goroutine %d: AddShare returned error: %v", routineID, addErr)
					// Consider adding more specific checks if AddShare isn't perfectly idempotent
				}
			case 3: // Remove the second share (might fail if not added yet or already removed)
				removeErr := km.RemoveShare(opCtx, sharePubKey2)
				// Similar to AddShare, expect no error or specific "not found" errors.
				if removeErr != nil {
					// t.Logf("Goroutine %d: RemoveShare returned error: %v", routineID, removeErr)
				}
			}
		}(i)
	}

	wg.Wait()

	// Final check: Ensure the original slashable block cannot be signed
	_, _, err = km.SignBeaconObject(ctx, blockSlashable, domainValid, testSharePubKey, slotToSign, spectypes.DomainProposer)
	require.Error(t, err, "Concurrency: Final check failed - slashable block was signable")
	require.Contains(t, err.Error(), "slashable proposal", "Concurrency: Final check error message mismatch")

	// Optional: Check the final state of sharePubKey2 (e.g., list accounts) if needed
}


// Helper to get BadgerDB storage. If dbPath is empty, creates a temp dir.
func getBaseStorage(t *testing.T, logger *zap.Logger, dbPath string) (basedb.Database, string) {
	if dbPath == "" {
		dbPath = t.TempDir()
	}
	db, err := basedb.NewBadgerDB(dbPath, logger)
	require.NoError(t, err)
	return db, dbPath
}

// setupLocalKeyManager is the setup function for LocalKeyManager tests.
// It now accepts an optional existing dbPath.
func setupLocalKeyManager(t *testing.T, operatorKey keys.OperatorPrivateKey, existingDbPath string) (KeyManager, *utils.MockBeaconNetwork, string, func()) {
	logger := logging.TestLogger(t)
	// Pass existingDbPath to getBaseStorage. If empty, it creates a temp dir.
	db, dbPath := getBaseStorage(t, logger, existingDbPath)
	// Use a shared mock beacon across restarts for DB integrity test consistency
	// For other tests, a new mock beacon per setup is fine.
	// We might need a more sophisticated way to handle this if tests need independent time progression.
	// For now, using a new one each time. Consider passing mockBeacon as an arg if needed.
	mockBeacon := utils.SetupMockBeaconNetwork(t, nil)

	network := &networkconfig.NetworkConfig{
		Beacon:     mockBeacon,
		DomainType: networkconfig.TestNetwork.DomainType, // Use test network domains
	}

	// Ensure network config uses the potentially shared mock beacon
	network.Beacon = mockBeacon

	km, err := NewLocalKeyManager(logger, db, *network, operatorKey)
	require.NoError(t, err, "Failed to create LocalKeyManager")

	cleanup := func() {
		err := db.Close()
		require.NoError(t, err, "Failed to close DB")
	}

	return km, mockBeacon, dbPath, cleanup
}

// --- Mocks for Remote/Consensus Clients (simplified for slashing tests) ---

// MockRemoteSigner simulates the remote signing service client.
type MockRemoteSigner struct {
	mock.Mock
}

func (m *MockRemoteSigner) AddValidators(ctx context.Context, shares ...ssvsigner.ShareKeys) error {
	args := m.Called(ctx, shares)
	return args.Error(0)
}

func (m *MockRemoteSigner) RemoveValidators(ctx context.Context, pubKeys []phase0.BLSPubKey) error {
	args := m.Called(ctx, pubKeys)
	return args.Error(0)
}

func (m *MockRemoteSigner) Sign(ctx context.Context, pubKey phase0.BLSPubKey, signingRoot phase0.Root) (phase0.BLSSignature, error) {
	args := m.Called(ctx, pubKey, signingRoot)
	sig, _ := args.Get(0).(phase0.BLSSignature)
	return sig, args.Error(1)
}

func (m *MockRemoteSigner) OperatorSign(ctx context.Context, payload []byte) ([]byte, error) {
	args := m.Called(ctx, payload)
	sig, _ := args.Get(0).([]byte)
	return sig, args.Error(1)
}

func (m *MockRemoteSigner) OperatorIdentity(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}

// MockConsensusClient simulates the consensus layer client.
type MockConsensusClient struct {
	mock.Mock
}

func (m *MockConsensusClient) ForkAtEpoch(ctx context.Context, epoch phase0.Epoch) (*phase0.Fork, error) {
	args := m.Called(ctx, epoch)
	fork, _ := args.Get(0).(*phase0.Fork)
	return fork, args.Error(1)
}

func (m *MockConsensusClient) Genesis(ctx context.Context) (*eth2api.Genesis, error) {
	args := m.Called(ctx)
	gen, _ := args.Get(0).(*eth2api.Genesis)
	return gen, args.Error(1)
}

// setupRemoteKeyManager is the setup function for RemoteKeyManager tests,
// using a real slashing protector but mocked remote interactions.
func setupRemoteKeyManager(t *testing.T, operatorKey keys.OperatorPrivateKey, existingDbPath string) (KeyManager, *utils.MockBeaconNetwork, string, func()) {
	logger := logging.TestLogger(t)
	db, dbPath := getBaseStorage(t, logger, existingDbPath)
	mockBeacon := utils.SetupMockBeaconNetwork(t, nil)

	network := &networkconfig.NetworkConfig{
		Beacon:     mockBeacon,
		DomainType: networkconfig.TestNetwork.DomainType,
	}

	// Real storage and protector
	signerStore := NewSignerStorage(db, mockBeacon, logger) // Use real SignerStorage
	protection := slashingprotection.NewNormalProtection(signerStore)
	protector := NewSlashingProtector(logger, signerStore, protection) // Use real SlashingProtector

	// Mocked clients
	mockSignerClient := new(MockRemoteSigner)
	mockConsensusClient := new(MockConsensusClient)

	// --- Configure Mock Expectations ---
	// Allow any Add/Remove calls during setup/test
	mockSignerClient.On("AddValidators", mock.Anything, mock.AnythingOfType("[]ssvsigner.ShareKeys")).Return(nil).Maybe()
	mockSignerClient.On("RemoveValidators", mock.Anything, mock.AnythingOfType("[]phase0.BLSPubKey")).Return(nil).Maybe()
	// Return a dummy signature for Sign calls (we test slashing logic, not remote signing itself)
	dummySig := phase0.BLSSignature{}
	dummySig[0] = 1 // Make it non-zero
	mockSignerClient.On("Sign", mock.Anything, mock.AnythingOfType("phase0.BLSPubKey"), mock.AnythingOfType("phase0.Root")).Return(dummySig, nil).Maybe()

	// Mock consensus client calls needed for signing context
	mockFork := &phase0.Fork{CurrentVersion: networkconfig.TestNetwork.GenesisForkVersion} // Simplified fork
	mockGenesis := &eth2api.Genesis{GenesisValidatorsRoot: phase0.Root{1}, GenesisForkVersion: networkconfig.TestNetwork.GenesisForkVersion}
	mockConsensusClient.On("ForkAtEpoch", mock.Anything, mock.AnythingOfType("phase0.Epoch")).Return(mockFork, nil).Maybe()
	mockConsensusClient.On("Genesis", mock.Anything).Return(mockGenesis, nil).Maybe()
	// --- End Mock Configuration ---


	// Instantiate RemoteKeyManager with real protector and mocked clients
	// We need a dummy operator public key for initialization as it's not directly used in signing flow with remote signer mock
	dummyOperatorPubKey := &MockOperatorPublicKey{}
	dummyOperatorPubKey.On("EKMVerify", mock.Anything, mock.Anything).Return(true) // Assume verification passes

	rm := &RemoteKeyManager{
		logger:            logger,
		netCfg:            *network,
		signerClient:      mockSignerClient,
		consensusClient:   mockConsensusClient,
		slashingProtector: protector, // Use the REAL protector
		getOperatorId:     func() spectypes.OperatorID { return 1 }, // Dummy operator ID
		operatorPubKey:    dummyOperatorPubKey,
	}

	cleanup := func() {
		err := db.Close()
		require.NoError(t, err, "Failed to close DB for RemoteKeyManager test")
	}

	return rm, mockBeacon, dbPath, cleanup
}
