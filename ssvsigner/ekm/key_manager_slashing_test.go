package ekm

import (
	"context"
	"sync"
	"testing"

	eth2api "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/prysmaticlabs/go-bitfield"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	mocknetwork "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon/mocks"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
	"github.com/stretchr/testify/require"

	"github.com/holiman/uint256"
	slashingprotection "github.com/ssvlabs/eth2-key-manager/slashing_protection"
	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/utils"
	"github.com/stretchr/testify/mock"
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
type setupKeyManagerFunc func(t *testing.T, operatorKey keys.OperatorPrivateKey, existingDbPath ...string) (KeyManager, *mocknetwork.MockBeaconNetwork, string, func())

// runKeyManagerSlashingTests executes a suite of slashing protection tests against a given KeyManager implementation.
func runKeyManagerSlashingTests(t *testing.T, setupFunc setupKeyManagerFunc) {
	t.Run("TestSlashableBlockDoubleProposal", func(t *testing.T) {
		testSlashableBlockDoubleProposal(t, setupFunc)
	})
	t.Run("TestSlashableAttestationDoubleVote", func(t *testing.T) {
		testSlashableAttestationDoubleVote(t, setupFunc)
	})
	t.Run("TestSlashableAttestationSurroundingVote", func(t *testing.T) {
		testSlashableAttestationSurroundingVote(t, setupFunc)
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

// testSlashableBlockDoubleProposal tests that the KeyManager prevents signing two blocks at the same slot.
func testSlashableBlockDoubleProposal(t *testing.T, setupFunc setupKeyManagerFunc) {
	km, mockBeacon, _, cleanup := setupFunc(t, testOperatorPrivKey)
	defer cleanup()
	ctx := context.Background()

	// Add the test share
	err := km.AddShare(ctx, testEncryptedShare, testSharePubKey)
	require.NoError(t, err, "Failed to add share")

	// --- First Block Proposal ---
	slotToSign := mockBeacon.EstimatedCurrentSlot() + 5 // Sign a block slightly in the future
	block1 := createMinimalDenebBlock(slotToSign, 1)
	domain := phase0.Domain{} // Use empty domain for testing

	// Sign the first block - should succeed
	sig1, root1, err := km.SignBeaconObject(ctx, block1, domain, testSharePubKey, slotToSign, spectypes.DomainProposer)
	require.NoError(t, err, "Signing first block failed")
	require.NotNil(t, sig1, "Signature 1 is nil")
	require.NotEqual(t, phase0.Root{}, root1, "Root 1 is zero")

	// --- Second Block Proposal (Same Slot) ---
	block2 := createMinimalDenebBlock(slotToSign, 1)

	// Attempt to sign the second block at the same slot - should fail
	_, _, err = km.SignBeaconObject(ctx, block2, domain, testSharePubKey, slotToSign, spectypes.DomainProposer)
	require.Error(t, err, "Signing second block at same slot should fail")
	require.Contains(t, err.Error(), "slashable proposal", "Error message should indicate slashable proposal")

	// --- Third Block Proposal (Lower Slot) ---
	block3 := createMinimalDenebBlock(slotToSign-1, 1)

	// Attempt to sign a block at a lower slot - should fail
	_, _, err = km.SignBeaconObject(ctx, block3, domain, testSharePubKey, slotToSign-1, spectypes.DomainProposer)
	require.Error(t, err, "Signing block at lower slot should fail")
	require.Contains(t, err.Error(), "slashable proposal", "Error message should indicate slashable proposal")

	// --- Fourth Block Proposal (Higher Slot) ---
	slotToSignHigher := slotToSign + 1
	block4 := createMinimalDenebBlock(slotToSignHigher, 1)
	domainHigher := phase0.Domain{} // Use empty domain for testing

	// Attempt to sign a block at a higher slot - should succeed
	sig4, root4, err := km.SignBeaconObject(ctx, block4, domainHigher, testSharePubKey, slotToSignHigher, spectypes.DomainProposer)
	require.NoError(t, err, "Signing block at higher slot failed")
	require.NotNil(t, sig4, "Signature 4 is nil")
	require.NotEqual(t, phase0.Root{}, root4, "Root 4 is zero")
}

// testSlashableAttestationDoubleVote tests signing two different attestations for the same target epoch.
func testSlashableAttestationDoubleVote(t *testing.T, setupFunc setupKeyManagerFunc) {
	km, mockBeacon, _, cleanup := setupFunc(t, testOperatorPrivKey)
	defer cleanup()
	ctx := context.Background()

	// Add the test share
	err := km.AddShare(ctx, testEncryptedShare, testSharePubKey)
	require.NoError(t, err, "Failed to add share")

	// --- First Attestation ---
	epochToSign := mockBeacon.EstimatedCurrentEpoch() + 2
	slotToSign := mockBeacon.FirstSlotAtEpoch(epochToSign)
	attData1 := &phase0.AttestationData{
		Slot:            slotToSign,
		Index:           0,
		BeaconBlockRoot: phase0.Root{0xAA}, // Different root
		Source:          &phase0.Checkpoint{Epoch: epochToSign - 1, Root: phase0.Root{0xBB}},
		Target:          &phase0.Checkpoint{Epoch: epochToSign, Root: phase0.Root{0xCC}}, // Same target epoch
	}
	domain := phase0.Domain{} // Use empty domain for testing

	// Sign the first attestation - should succeed
	sig1, root1, err := km.SignBeaconObject(ctx, attData1, domain, testSharePubKey, slotToSign, spectypes.DomainAttester)
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
	_, _, err = km.SignBeaconObject(ctx, attData2, domain, testSharePubKey, slotToSign, spectypes.DomainAttester)
	require.Error(t, err, "Signing second attestation with same target epoch should fail")
	require.Contains(t, err.Error(), "slashable attestation", "Error message should indicate slashable attestation")

	// --- Third Attestation (Higher Target Epoch) ---
	epochToSignHigher := epochToSign + 1
	slotToSignHigher := mockBeacon.FirstSlotAtEpoch(epochToSignHigher)
	attData3 := &phase0.AttestationData{
		Slot:            slotToSignHigher,
		Index:           0,
		BeaconBlockRoot: phase0.Root{0x11},
		Source:          &phase0.Checkpoint{Epoch: epochToSign, Root: phase0.Root{0x22}},       // Source is previous target
		Target:          &phase0.Checkpoint{Epoch: epochToSignHigher, Root: phase0.Root{0x33}}, // Higher target epoch
	}
	domainHigher := phase0.Domain{} // Use empty domain for testing

	// Attempt to sign the third attestation - should succeed
	sig3, root3, err := km.SignBeaconObject(ctx, attData3, domainHigher, testSharePubKey, slotToSignHigher, spectypes.DomainAttester)
	require.NoError(t, err, "Signing third attestation with higher target epoch failed")
	require.NotNil(t, sig3, "Signature 3 is nil")
	require.NotEqual(t, phase0.Root{}, root3, "Root 3 is zero")
}

// testSlashableAttestationSurroundingVote tests signing an attestation that surrounds a previous one.
func testSlashableAttestationSurroundingVote(t *testing.T, setupFunc setupKeyManagerFunc) {
	km, mockBeacon, _, cleanup := setupFunc(t, testOperatorPrivKey)
	defer cleanup()
	ctx := context.Background()

	// Add the test share
	err := km.AddShare(ctx, testEncryptedShare, testSharePubKey)
	require.NoError(t, err, "Failed to add share")

	// --- First Attestation (Inner) ---
	innerSourceEpoch := mockBeacon.EstimatedCurrentEpoch() + 2
	innerTargetEpoch := innerSourceEpoch + 1
	innerSlot := mockBeacon.FirstSlotAtEpoch(innerTargetEpoch) // Slot within the target epoch
	attDataInner := &phase0.AttestationData{
		Slot:            innerSlot,
		Index:           0,
		BeaconBlockRoot: phase0.Root{0xAA},
		Source:          &phase0.Checkpoint{Epoch: innerSourceEpoch, Root: phase0.Root{0xBB}},
		Target:          &phase0.Checkpoint{Epoch: innerTargetEpoch, Root: phase0.Root{0xCC}},
	}
	domainInner := phase0.Domain{} // Use empty domain for testing

	// Sign the inner attestation - should succeed
	sigInner, rootInner, err := km.SignBeaconObject(ctx, attDataInner, domainInner, testSharePubKey, innerSlot, spectypes.DomainAttester)
	require.NoError(t, err, "Signing inner attestation failed")
	require.NotNil(t, sigInner, "Inner signature is nil")
	require.NotEqual(t, phase0.Root{}, rootInner, "Inner root is zero")

	// --- Second Attestation (Surrounding) ---
	outerSourceEpoch := innerSourceEpoch - 1 // Lower source
	outerTargetEpoch := innerTargetEpoch + 1 // Higher target
	outerSlot := mockBeacon.FirstSlotAtEpoch(outerTargetEpoch)
	attDataOuter := &phase0.AttestationData{
		Slot:            outerSlot,
		Index:           0,
		BeaconBlockRoot: phase0.Root{0xDD},
		Source:          &phase0.Checkpoint{Epoch: outerSourceEpoch, Root: phase0.Root{0xEE}},
		Target:          &phase0.Checkpoint{Epoch: outerTargetEpoch, Root: phase0.Root{0xFF}},
	}
	domainOuter := phase0.Domain{} // Use empty domain for testing

	// Attempt to sign the outer attestation - should fail (Surrounding Vote)
	_, _, err = km.SignBeaconObject(ctx, attDataOuter, domainOuter, testSharePubKey, outerSlot, spectypes.DomainAttester)
	require.Error(t, err, "Signing outer (surrounding) attestation should fail")
	require.Contains(t, err.Error(), "slashable attestation", "Error message should indicate slashable attestation")

	// --- Third Attestation (Non-Surrounding, Higher) ---
	higherSourceEpoch := innerSourceEpoch // Same source as inner
	higherTargetEpoch := outerTargetEpoch // Same target as outer (but source is higher than outer's source)
	higherSlot := mockBeacon.FirstSlotAtEpoch(higherTargetEpoch)
	attDataHigher := &phase0.AttestationData{
		Slot:            higherSlot,
		Index:           0,
		BeaconBlockRoot: phase0.Root{0x11},
		Source:          &phase0.Checkpoint{Epoch: higherSourceEpoch, Root: phase0.Root{0x22}},
		Target:          &phase0.Checkpoint{Epoch: higherTargetEpoch, Root: phase0.Root{0x33}},
	}
	domainHigher := phase0.Domain{} // Use empty domain for testing

	// Attempt to sign the third attestation - should succeed
	sigHigher, rootHigher, err := km.SignBeaconObject(ctx, attDataHigher, domainHigher, testSharePubKey, higherSlot, spectypes.DomainAttester)
	require.NoError(t, err, "Signing higher (non-surrounding) attestation failed")
	require.NotNil(t, sigHigher, "Higher signature is nil")
	require.NotEqual(t, phase0.Root{}, rootHigher, "Higher root is zero")
}

// testDBIntegrity tests that slashing protection data persists across restarts.
func testDBIntegrity(t *testing.T, setupFunc setupKeyManagerFunc) {
	// Create a unique database file path for the test
	dbPath := t.TempDir() + "/slashing_db.db"
	t.Logf("Using persistent DB path: %s", dbPath)

	// --- Phase 1: Initial Setup, Sign, and Close ---
	km1, mockBeacon1, _, cleanup1 := setupFunc(t, testOperatorPrivKey, dbPath)
	ctx := context.Background()

	// Add the test share
	err := km1.AddShare(ctx, testEncryptedShare, testSharePubKey)
	require.NoError(t, err, "DB Integrity: Failed to add share (Phase 1)")

	// Sign a block
	slotToSign := mockBeacon1.EstimatedCurrentSlot() + 5
	block1 := createMinimalDenebBlock(slotToSign, 1)
	domain1 := phase0.Domain{} // Use empty domain for testing
	sig1, root1, err := km1.SignBeaconObject(ctx, block1, domain1, testSharePubKey, slotToSign, spectypes.DomainProposer)
	require.NoError(t, err, "DB Integrity: Signing first block failed (Phase 1)")
	require.NotNil(t, sig1, "Signature 1 is nil")
	require.NotEqual(t, phase0.Root{}, root1, "Root 1 is zero")

	// Close the first key manager and its DB
	cleanup1()
	t.Log("Phase 1 completed. Database closed.")

	// --- Phase 2: Reopen DB and Attempt Slashable Signing ---
	// Make sure it's a fresh instance with the same DB path
	t.Log("Starting Phase 2 with the same database path")
	km2, _, _, cleanup2 := setupFunc(t, testOperatorPrivKey, dbPath) // Use the same path
	defer cleanup2()

	// Need to add the share again for the second key manager as it's a fresh instance
	err = km2.AddShare(ctx, testEncryptedShare, testSharePubKey)
	require.NoError(t, err, "DB Integrity: Failed to add share (Phase 2)")

	// Attempt to sign the *same* block again - should fail due to persisted data
	t.Log("Attempting to sign the same block again - should fail")
	domain2 := phase0.Domain{} // Use empty domain for testing
	_, _, err = km2.SignBeaconObject(ctx, block1, domain2, testSharePubKey, slotToSign, spectypes.DomainProposer)
	require.Error(t, err, "DB Integrity: Signing same block after reopen should fail")
	require.Contains(t, err.Error(), "slashable proposal", "DB Integrity: Error message should indicate slashable proposal")

	// Attempt to sign a *different* block at the *same* slot - should also fail
	block2 := createMinimalDenebBlock(slotToSign, 2) // Different block with different parent
	_, _, err = km2.SignBeaconObject(ctx, block2, domain2, testSharePubKey, slotToSign, spectypes.DomainProposer)
	require.Error(t, err, "DB Integrity: Signing different block at same slot after reopen should fail")
	require.Contains(t, err.Error(), "slashable proposal", "DB Integrity: Error message should indicate slashable proposal")

	// Attempt to sign a block at a *higher* slot - should succeed
	slotToSignHigher := slotToSign + 1
	block4 := createMinimalDenebBlock(slotToSignHigher, 3) // Different parent root
	domain4 := phase0.Domain{}                             // Use empty domain for testing
	_, _, err = km2.SignBeaconObject(ctx, block4, domain4, testSharePubKey, slotToSignHigher, spectypes.DomainProposer)
	require.NoError(t, err, "DB Integrity: Signing block at higher slot after reopen failed")
}

// testConcurrency tests concurrent signing and share management.
func testConcurrency(t *testing.T, setupFunc setupKeyManagerFunc) {
	km, mockBeacon, _, cleanup := setupFunc(t, testOperatorPrivKey)
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
	blockValid := createMinimalDenebBlock(slotToSign, 1)
	domainValid := phase0.Domain{} // Use empty domain for testing
	_, _, err = km.SignBeaconObject(ctx, blockValid, domainValid, testSharePubKey, slotToSign, spectypes.DomainProposer)
	require.NoError(t, err, "Concurrency: Signing initial valid block failed")

	// Prepare slashable data (same slot as the valid one)
	blockSlashable := createMinimalDenebBlock(slotToSign, 1)

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
				futureBlock := createMinimalDenebBlock(futureSlot, 1)
				futureDomain := phase0.Domain{} // Use empty domain for testing
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
}

// Helper function to create a minimal deneb.BeaconBlock with valid data
func createMinimalDenebBlock(slot phase0.Slot, proposerIndex phase0.ValidatorIndex) *deneb.BeaconBlock {
	// Import bellatrix for Transaction type
	// Create a minimal block with all required fields properly initialized
	block := &deneb.BeaconBlock{
		Slot:          slot,
		ProposerIndex: proposerIndex,
		ParentRoot:    phase0.Root{},
		StateRoot:     phase0.Root{},
		Body: &deneb.BeaconBlockBody{
			RANDAOReveal: phase0.BLSSignature{},
			ETH1Data: &phase0.ETH1Data{
				DepositRoot:  phase0.Root{},
				DepositCount: 0,
				BlockHash:    make([]byte, 32), // Ensure proper length of 32 bytes
			},
			Graffiti: [32]byte{},
			// Initialize SyncAggregate with proper byte lengths
			SyncAggregate: &altair.SyncAggregate{
				// Initialize with bitfield.Bitvector512 correctly
				SyncCommitteeBits:      bitfield.NewBitvector512(),
				SyncCommitteeSignature: phase0.BLSSignature{},
			},
			// Initialize ExecutionPayload properly to avoid nil pointer dereference
			ExecutionPayload: &deneb.ExecutionPayload{
				ParentHash:   [32]byte{},
				FeeRecipient: [20]byte{},
				StateRoot:    [32]byte{},
				ReceiptsRoot: [32]byte{},
				LogsBloom:    [256]byte{},
				PrevRandao:   [32]byte{},
				BlockNumber:  0,
				GasLimit:     0,
				GasUsed:      0,
				Timestamp:    0,
				ExtraData:    []byte{},
				// Initialize with a non-nil value but keeping it simple
				// The github.com/holiman/uint256 package requires a non-nil BaseFeePerGas
				BaseFeePerGas: new(uint256.Int), // Initialize with empty uint256 instead of nil
				BlockHash:     [32]byte{},
				Transactions:  nil, // Init as nil to avoid type errors
				Withdrawals:   nil, // Init as nil to avoid type errors
				BlobGasUsed:   0,
				ExcessBlobGas: 0,
			},
		},
	}
	return block
}

// setupLocalKeyManager is the setup function for LocalKeyManager tests.
// It now accepts an optional existing dbPath.
func setupLocalKeyManager(t *testing.T, operatorKey keys.OperatorPrivateKey, existingDbPath ...string) (KeyManager, *mocknetwork.MockBeaconNetwork, string, func()) {
	logger := logging.TestLogger(t)

	var db basedb.Database
	var err error
	var dbPath string

	// Use the existing DB path if provided, otherwise create a new in-memory DB
	if len(existingDbPath) > 0 && existingDbPath[0] != "" {
		// For reusing an existing DB (testing persistence), use the provided path
		dbPath = existingDbPath[0]
		t.Logf("Using persistent DB at %s", dbPath)
		db, err = kv.New(logger, basedb.Options{Path: dbPath})
		require.NoError(t, err, "Failed to create persistent DB")
	} else {
		// For normal tests, use in-memory DB
		db, err = kv.NewInMemory(logger, basedb.Options{})
		require.NoError(t, err, "Failed to create in-memory DB")
	}

	// Use a shared mock beacon across restarts for DB integrity test consistency
	mockBeacon := utils.SetupMockBeaconNetwork(t, nil)

	network := &networkconfig.NetworkConfig{
		Beacon:     mockBeacon,
		DomainType: [4]byte{0, 0, 0, 0}, // Use a simple domain type for testing
	}

	km, err := NewLocalKeyManager(logger, db, *network, operatorKey)
	require.NoError(t, err, "Failed to create LocalKeyManager")

	cleanup := func() {
		err := db.Close()
		require.NoError(t, err, "Failed to close DB")
	}

	return km, mockBeacon, dbPath, cleanup
}

// setupRemoteKeyManager is the setup function for RemoteKeyManager tests,
// using a real slashing protector but mocked remote interactions.
func setupRemoteKeyManager(t *testing.T, operatorKey keys.OperatorPrivateKey, existingDbPath ...string) (KeyManager, *mocknetwork.MockBeaconNetwork, string, func()) {
	logger := logging.TestLogger(t)

	var db basedb.Database
	var err error
	var dbPath string

	// Use the existing DB path if provided, otherwise create a new in-memory DB
	if len(existingDbPath) > 0 && existingDbPath[0] != "" {
		// For reusing an existing DB (testing persistence), use the provided path
		dbPath = existingDbPath[0]
		t.Logf("Using persistent DB for RemoteKeyManager at %s", dbPath)
		db, err = kv.New(logger, basedb.Options{Path: dbPath})
		require.NoError(t, err, "Failed to create persistent DB for RemoteKeyManager")
	} else {
		// For normal tests, use in-memory DB
		db, err = kv.NewInMemory(logger, basedb.Options{})
		require.NoError(t, err, "Failed to create in-memory DB for RemoteKeyManager")
	}

	mockBeacon := utils.SetupMockBeaconNetwork(t, nil)

	network := &networkconfig.NetworkConfig{
		Beacon:     mockBeacon,
		DomainType: [4]byte{0, 0, 0, 0}, // Use a simple domain type for testing
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
	mockSignerClient.On("AddValidators", mock.Anything, mock.Anything).Return(nil).Maybe()
	mockSignerClient.On("RemoveValidators", mock.Anything, mock.Anything).Return(nil).Maybe()

	// Return a dummy signature for Sign calls (we test slashing logic, not remote signing itself)
	dummySig := phase0.BLSSignature{}
	dummySig[0] = 1 // Make it non-zero
	mockSignerClient.On("Sign", mock.Anything, mock.Anything, mock.Anything).Return(dummySig, nil).Maybe()

	// Mock consensus client calls needed for signing context
	mockFork := &phase0.Fork{CurrentVersion: [4]byte{0, 0, 0, 0}} // Simplified fork
	mockGenesis := &eth2api.Genesis{GenesisValidatorsRoot: phase0.Root{1}, GenesisForkVersion: [4]byte{0, 0, 0, 0}}
	mockConsensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Maybe()
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
		slashingProtector: protector,                                // Use the REAL protector
		getOperatorId:     func() spectypes.OperatorID { return 1 }, // Dummy operator ID
		operatorPubKey:    dummyOperatorPubKey,
	}

	cleanup := func() {
		err := db.Close()
		require.NoError(t, err, "Failed to close DB for RemoteKeyManager test")
	}

	return rm, mockBeacon, dbPath, cleanup
}

// TestLocalKeyManagerSlashingProtection runs slashing protection tests against the LocalKeyManager.
func TestLocalKeyManagerSlashingProtection(t *testing.T) {
	runKeyManagerSlashingTests(t, setupLocalKeyManager)
}

// TestRemoteKeyManagerSlashingProtection runs slashing protection tests against the RemoteKeyManager.
func TestRemoteKeyManagerSlashingProtection(t *testing.T) {
	runKeyManagerSlashingTests(t, setupRemoteKeyManager)
}
