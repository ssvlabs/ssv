package ekm

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	slashingprotection "github.com/ssvlabs/eth2-key-manager/slashing_protection"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
	"github.com/ssvlabs/ssv/utils"
)

func TestSlashing(t *testing.T) {
	ctx := context.Background()

	operatorPrivateKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	km, network := testKeyManager(t, nil, operatorPrivateKey)

	sk1 := &bls.SecretKey{}
	require.NoError(t, sk1.SetHexString(sk1Str))

	encryptedSK1, err := operatorPrivateKey.Public().Encrypt([]byte(sk1.SerializeToHexStr()))
	require.NoError(t, err)

	require.NoError(t, km.AddShare(context.Background(), encryptedSK1, phase0.BLSPubKey(sk1.GetPublicKey().Serialize())))

	currentSlot := network.Beacon.EstimatedCurrentSlot()
	currentEpoch := network.Beacon.EstimatedEpochAtSlot(currentSlot)

	highestTarget := currentEpoch + minSPAttestationEpochGap + 1
	highestSource := highestTarget - 1
	highestProposal := currentSlot + minSPProposalSlotGap + 1

	attestationData := testingutils.TestingAttestationData(spec.DataVersionPhase0)
	attestationData.Slot = currentSlot
	attestationData.Source.Epoch = highestSource
	attestationData.Target.Epoch = highestTarget

	beaconBlock := testingutils.TestingBeaconBlockCapella
	beaconBlock.Slot = highestProposal

	t.Run("sign once", func(t *testing.T) {
		err := km.(*LocalKeyManager).IsAttestationSlashable(phase0.BLSPubKey(sk1.GetPublicKey().Serialize()), attestationData)
		require.NoError(t, err)

		_, sig, err := km.(*LocalKeyManager).SignBeaconObject(
			ctx,
			attestationData,
			phase0.Domain{},
			phase0.BLSPubKey(sk1.GetPublicKey().Serialize()),
			currentSlot,
			spectypes.DomainAttester)
		require.NoError(t, err)
		require.NotNil(t, sig)
		require.NotEqual(t, [32]byte{}, sig)
	})
	t.Run("slashable sign, fail (attestation)", func(t *testing.T) {
		_, sig, err := km.(*LocalKeyManager).SignBeaconObject(
			ctx,
			attestationData,
			phase0.Domain{},
			phase0.BLSPubKey(sk1.GetPublicKey().Serialize()),
			currentSlot,
			spectypes.DomainAttester)
		require.EqualError(t, err, "slashable attestation (HighestAttestationVote), not signing")
		require.Equal(t, phase0.Root{}, sig)

		err = km.(*LocalKeyManager).IsAttestationSlashable(phase0.BLSPubKey(sk1.GetPublicKey().Serialize()), attestationData)
		require.EqualError(t, err, "slashable attestation (HighestAttestationVote), not signing")
	})

	t.Run("sign once", func(t *testing.T) {
		err := km.(*LocalKeyManager).IsBeaconBlockSlashable(phase0.BLSPubKey(sk1.GetPublicKey().Serialize()), beaconBlock.Slot)
		require.NoError(t, err)
		_, sig, err := km.(*LocalKeyManager).SignBeaconObject(
			ctx,
			beaconBlock,
			phase0.Domain{},
			phase0.BLSPubKey(sk1.GetPublicKey().Serialize()),
			currentSlot,
			spectypes.DomainProposer,
		)
		require.NoError(t, err)
		require.NotNil(t, sig)
		require.NotEqual(t, [32]byte{}, sig)
	})
	t.Run("slashable sign, fail (proposal)", func(t *testing.T) {
		_, sig, err := km.(*LocalKeyManager).SignBeaconObject(
			ctx,
			beaconBlock,
			phase0.Domain{},
			phase0.BLSPubKey(sk1.GetPublicKey().Serialize()),
			currentSlot,
			spectypes.DomainProposer)
		require.EqualError(t, err, "slashable proposal (HighestProposalVote), not signing")
		require.Equal(t, phase0.Root{}, sig)

		err = km.(*LocalKeyManager).IsBeaconBlockSlashable(phase0.BLSPubKey(sk1.GetPublicKey().Serialize()), beaconBlock.Slot)
		require.EqualError(t, err, "slashable proposal (HighestProposalVote), not signing")
	})
	t.Run("slashable sign after duplicate AddShare, fail", func(t *testing.T) {
		require.NoError(t, km.AddShare(context.Background(), encryptedSK1, phase0.BLSPubKey(sk1.GetPublicKey().Serialize())))
		_, sig, err := km.(*LocalKeyManager).SignBeaconObject(
			ctx,
			beaconBlock,
			phase0.Domain{},
			phase0.BLSPubKey(sk1.GetPublicKey().Serialize()),
			currentSlot,
			spectypes.DomainProposer)
		require.EqualError(t, err, "slashable proposal (HighestProposalVote), not signing")
		require.Equal(t, phase0.Root{}, sig)
	})
}

func TestSlashing_Attestation(t *testing.T) {
	ctx := context.Background()

	operatorPrivateKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	km, _ := testKeyManager(t, nil, operatorPrivateKey)

	var secretKeys [4]*bls.SecretKey
	for i := range secretKeys {
		secretKeys[i] = &bls.SecretKey{}
		secretKeys[i].SetByCSPRNG()

		// Equivalent to AddShare but with a custom slot for minimal slashing protection.
		err := km.(*LocalKeyManager).BumpSlashingProtection(phase0.BLSPubKey(secretKeys[i].GetPublicKey().Serialize()))
		require.NoError(t, err)
		err = km.(*LocalKeyManager).saveShare(secretKeys[i])
		require.NoError(t, err)
	}

	var baseEpoch phase0.Epoch = 10

	createAttestationData := func(sourceEpoch, targetEpoch phase0.Epoch) *phase0.AttestationData {
		return &phase0.AttestationData{
			Source: &phase0.Checkpoint{
				Epoch: baseEpoch + sourceEpoch,
			},
			Target: &phase0.Checkpoint{
				Epoch: baseEpoch + targetEpoch,
			},
		}
	}

	slot := phase0.Slot(100)

	signAttestation := func(sk *bls.SecretKey, signingRoot phase0.Root, attestation *phase0.AttestationData, expectSlashing bool, expectReason string) {
		sig, root, err := km.(*LocalKeyManager).SignBeaconObject(
			ctx,
			attestation,
			phase0.Domain{},
			phase0.BLSPubKey(sk.GetPublicKey().Serialize()),
			slot,
			spectypes.DomainAttester)
		if expectSlashing {
			require.Error(t, err, "expected slashing: %s", expectReason)
			require.Zero(t, sig, "expected zero signature")
			require.Zero(t, root, "expected zero root")
			if expectReason != "" {
				require.ErrorContains(t, err, expectReason, "expected slashing: %s", expectReason)
			}
		} else {
			require.NoError(t, err, "expected no slashing")
			require.NotZero(t, sig, "expected non-zero signature")
			require.NotZero(t, root, "expected non-zero root")

			highAtt, found, err := km.RetrieveHighestAttestation(phase0.BLSPubKey(sk.GetPublicKey().Serialize()))
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, attestation.Source.Epoch, highAtt.Source.Epoch)
			require.Equal(t, attestation.Target.Epoch, highAtt.Target.Epoch)
		}
	}

	// Note: in the current implementation, eth2-key-manager doesn't check the signing roots and is
	// instead blocking the repeated signing of the same attestation.

	// 1. Check a valid attestation.
	signAttestation(secretKeys[1], phase0.Root{1}, createAttestationData(3, 4), false, "HighestAttestationVote")

	// 2. Same signing root -> slashing (stricter than Eth spec).
	signAttestation(secretKeys[1], phase0.Root{1}, createAttestationData(3, 4), true, "HighestAttestationVote")

	// 3. Lower than previous source epoch -> expect slashing.
	signAttestation(secretKeys[1], phase0.Root{1}, createAttestationData(2, 5), true, "HighestAttestationVote")

	// 4. Different signing root -> expect slashing.
	signAttestation(secretKeys[1], phase0.Root{2}, createAttestationData(3, 4), true, "HighestAttestationVote")

	// 5. Different signing root, lower target epoch -> expect slashing.
	signAttestation(secretKeys[1], phase0.Root{2}, createAttestationData(3, 3), true, "HighestAttestationVote")

	// 6. Different signing root, same source epoch, higher target epoch -> no slashing.
	signAttestation(secretKeys[1], phase0.Root{3}, createAttestationData(3, 5), false, "HighestAttestationVote")

	// 7. Different signing root, higher source epoch, same target epoch -> expect slashing.
	signAttestation(secretKeys[1], phase0.Root{3}, createAttestationData(4, 5), true, "HighestAttestationVote")

	// 8. Different signing root, lower source epoch, higher target epoch -> expect slashing.
	//    This should fail due to surrounding, but since we're stricter than Eth spec,
	//    this will fail due to the source epoch being lower than the previous.
	signAttestation(secretKeys[1], phase0.Root{byte(4)}, createAttestationData(2, 6), true, "HighestAttestationVote")

	// 9. Different public key, different signing root -> no slashing.
	signAttestation(secretKeys[2], phase0.Root{4}, createAttestationData(3, 6), false, "HighestAttestationVote")

	// 10. Different signing root, higher source epoch, lower target epoch -> expect slashing.
	//     Same as 8, but in the opposite direction.
	signAttestation(secretKeys[2], phase0.Root{5}, createAttestationData(4, 5), true, "HighestAttestationVote")

	// 11. Different signing root, lower source epoch, lower target epoch -> expect slashing.
	//     The new point is strictly lower in both source & target
	signAttestation(secretKeys[2], phase0.Root{5}, createAttestationData(2, 5), true, "HighestAttestationVote")

	// 12. Different signing root, lower source epoch, same target epoch -> expect slashing.
	//     The new point is lower in source but equal in target
	signAttestation(secretKeys[2], phase0.Root{5}, createAttestationData(2, 6), true, "HighestAttestationVote")

	// (s==t)
	// 13. Different signing root -> no slashing.
	//     The new point on the line s==t, strictly higher in source and target
	signAttestation(secretKeys[2], phase0.Root{6}, createAttestationData(7, 7), false, "HighestAttestationVote")

	// (s==t)
	// 14. Different signing root -> expect slashing.
	//     The new point on the line s==t, strictly lower in source and target
	signAttestation(secretKeys[2], phase0.Root{7}, createAttestationData(6, 6), true, "HighestAttestationVote")
}

func TestConcurrentSlashingProtectionAttData(t *testing.T) {
	ctx := context.Background()

	require.NoError(t, bls.Init(bls.BLS12_381))

	operatorPrivateKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	km, network := testKeyManager(t, nil, operatorPrivateKey)

	sk1 := &bls.SecretKey{}
	require.NoError(t, sk1.SetHexString(sk1Str))

	currentSlot := network.Beacon.EstimatedCurrentSlot()
	currentEpoch := network.Beacon.EstimatedEpochAtSlot(currentSlot)

	highestTarget := currentEpoch + minSPAttestationEpochGap + 1
	highestSource := highestTarget - 1

	attestationData := testingutils.TestingAttestationData(spec.DataVersionPhase0)
	attestationData.Slot = currentSlot
	attestationData.Source.Epoch = highestSource
	attestationData.Target.Epoch = highestTarget

	signAttestation := func(wg *sync.WaitGroup, errChan chan error) {
		defer wg.Done()

		sigBytes, root, err := km.(*LocalKeyManager).SignBeaconObject(
			ctx,
			attestationData,
			phase0.Domain{},
			phase0.BLSPubKey(sk1.GetPublicKey().Serialize()),
			currentSlot,
			spectypes.DomainAttester,
		)
		if err == nil {
			sig := &bls.Sign{}
			require.NoError(t, sig.Deserialize(sigBytes[:]))

			// Perform BLS verification with public key and computed signing root.
			if !sig.VerifyByte(sk1.GetPublicKey(), root[:]) {
				err = fmt.Errorf("BLS verification failed for signature %x", sigBytes)
			}
		}
		errChan <- err
	}

	const goroutineCount = 1000

	var wg sync.WaitGroup

	errChan := make(chan error, goroutineCount)

	for range goroutineCount {
		wg.Add(1)

		go signAttestation(&wg, errChan)
	}

	wg.Wait()
	close(errChan)

	var slashableErrors, successCount int

	for err := range errChan {
		if err != nil {
			if err.Error() != "slashable attestation (HighestAttestationVote), not signing" {
				require.Fail(t, "unexpected error: %v", err)
			}

			slashableErrors++
		} else {
			successCount++
		}
	}

	require.Equal(t, 1, successCount, "expected exactly one successful signing")
	require.Equal(t, goroutineCount-1, slashableErrors, "expected slashing errors for remaining goroutines")
}

func TestConcurrentSlashingProtectionBeaconBlock(t *testing.T) {
	ctx := context.Background()

	require.NoError(t, bls.Init(bls.BLS12_381))

	operatorPrivateKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	km, network := testKeyManager(t, nil, operatorPrivateKey)

	sk1 := &bls.SecretKey{}
	require.NoError(t, sk1.SetHexString(sk1Str))

	currentSlot := network.Beacon.EstimatedCurrentSlot()
	highestProposal := currentSlot + minSPProposalSlotGap + 1

	blockContents := testingutils.TestingBlockContentsDeneb
	blockContents.Block.Slot = highestProposal

	signBeaconBlock := func(wg *sync.WaitGroup, errChan chan error) {
		defer wg.Done()

		sigBytes, root, err := km.(*LocalKeyManager).SignBeaconObject(
			ctx,
			blockContents.Block,
			phase0.Domain{},
			phase0.BLSPubKey(sk1.GetPublicKey().Serialize()),
			currentSlot,
			spectypes.DomainProposer,
		)
		if err == nil {
			sig := &bls.Sign{}
			require.NoError(t, sig.Deserialize(sigBytes[:]))

			// Perform BLS verification with public key and computed signing root.
			if !sig.VerifyByte(sk1.GetPublicKey(), root[:]) {
				err = fmt.Errorf("BLS verification failed for signature %x", sigBytes)
			}
		}
		errChan <- err
	}

	const goroutineCount = 1000

	var wg sync.WaitGroup

	errChan := make(chan error, goroutineCount)

	for range goroutineCount {
		wg.Add(1)

		go signBeaconBlock(&wg, errChan)
	}

	wg.Wait()
	close(errChan)

	// Count errors and successes.
	var slashableErrors, successCount int

	for err := range errChan {
		if err != nil {
			if err.Error() != "slashable proposal (HighestProposalVote), not signing" {
				require.Fail(t, "unexpected error: %v", err)
			}

			slashableErrors++
		} else {
			successCount++
		}
	}

	require.Equal(t, 1, successCount, "expected exactly one successful signing")
	require.Equal(t, goroutineCount-1, slashableErrors, "expected slashing errors for remaining goroutines")
}

func TestConcurrentSlashingProtectionWithMultipleKeysAttData(t *testing.T) {
	ctx := context.Background()

	require.NoError(t, bls.Init(bls.BLS12_381))

	operatorPrivateKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	type testValidator struct {
		sk *bls.SecretKey
		pk *bls.PublicKey
	}

	var testValidators []testValidator

	for range 3 {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()
		pk := sk.GetPublicKey()
		testValidators = append(testValidators, testValidator{sk: sk, pk: pk})
	}

	// Initialize key manager and add shares for each validator
	km, network := testKeyManager(t, nil, operatorPrivateKey)

	for _, validator := range testValidators {
		encryptedPrivKey, err := operatorPrivateKey.Public().Encrypt([]byte(validator.sk.SerializeToHexStr()))
		require.NoError(t, err)

		require.NoError(t, km.AddShare(context.Background(), encryptedPrivKey, phase0.BLSPubKey(validator.sk.GetPublicKey().Serialize())))
	}

	currentSlot := network.Beacon.EstimatedCurrentSlot()
	currentEpoch := network.Beacon.EstimatedEpochAtSlot(currentSlot)

	highestTarget := currentEpoch + minSPAttestationEpochGap + 1
	highestSource := highestTarget - 1

	attestationData := testingutils.TestingAttestationData(spec.DataVersionPhase0)
	attestationData.Slot = currentSlot
	attestationData.Source.Epoch = highestSource
	attestationData.Target.Epoch = highestTarget

	type validatorResult struct {
		signs int
		errs  int
	}

	validatorResults := make(map[string]*validatorResult)
	for _, v := range testValidators {
		validatorResults[v.pk.SerializeToHexStr()] = &validatorResult{}
	}

	var mu sync.Mutex

	const goroutinesPerValidator = 100

	var wg sync.WaitGroup

	for _, validator := range testValidators {
		for range goroutinesPerValidator {
			wg.Add(1)

			go func() {
				defer wg.Done()

				sigBytes, root, err := km.(*LocalKeyManager).SignBeaconObject(
					ctx,
					attestationData,
					phase0.Domain{},
					phase0.BLSPubKey(validator.pk.Serialize()),
					currentSlot,
					spectypes.DomainAttester,
				)

				mu.Lock()
				defer mu.Unlock()

				result := validatorResults[validator.pk.SerializeToHexStr()]
				if err != nil {
					result.errs++
					require.ErrorContains(t, err, "slashable attestation (HighestAttestationVote), not signing")
				} else {
					sig := &bls.Sign{}
					require.NoError(t, sig.Deserialize(sigBytes[:]))

					// Perform BLS verification with public key and computed signing root.
					if !sig.VerifyByte(validator.pk, root[:]) {
						require.Truef(t, sig.VerifyByte(validator.pk, root[:]), "BLS verification failed for signature %x", sigBytes)
					}
					result.signs++
				}
			}()
		}
	}
	wg.Wait()

	// Validate that for each validator, only one signing succeeded, and the rest failed
	for _, result := range validatorResults {
		require.Equal(t, 1, result.signs)
		require.Equal(t, goroutinesPerValidator-1, result.errs)
	}
}

func TestConcurrentSlashingProtectionWithMultipleKeysBeaconBlock(t *testing.T) {
	ctx := context.Background()

	require.NoError(t, bls.Init(bls.BLS12_381))

	operatorPrivateKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	type testValidator struct {
		sk *bls.SecretKey
		pk *bls.PublicKey
	}

	var testValidators []testValidator

	for range 3 {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()
		pk := sk.GetPublicKey()
		testValidators = append(testValidators, testValidator{sk: sk, pk: pk})
	}

	// Initialize key manager and add shares for each validator
	km, network := testKeyManager(t, nil, operatorPrivateKey)

	for _, validator := range testValidators {
		encryptedPrivKey, err := operatorPrivateKey.Public().Encrypt([]byte(validator.sk.SerializeToHexStr()))
		require.NoError(t, err)

		require.NoError(t, km.AddShare(context.Background(), encryptedPrivKey, phase0.BLSPubKey(validator.sk.GetPublicKey().Serialize())))
	}

	currentSlot := network.Beacon.EstimatedCurrentSlot()
	highestProposal := currentSlot + minSPProposalSlotGap + 1

	blockContents := testingutils.TestingBlockContentsDeneb
	blockContents.Block.Slot = highestProposal

	type validatorResult struct {
		signs int
		errs  int
	}

	validatorResults := make(map[string]*validatorResult)
	for _, v := range testValidators {
		validatorResults[v.pk.SerializeToHexStr()] = &validatorResult{}
	}

	var mu sync.Mutex

	const goroutinesPerValidator = 100

	var wg sync.WaitGroup

	for _, validator := range testValidators {
		for range goroutinesPerValidator {
			wg.Add(1)

			go func() {
				defer wg.Done()

				sigBytes, root, err := km.(*LocalKeyManager).SignBeaconObject(
					ctx,
					blockContents.Block,
					phase0.Domain{},
					phase0.BLSPubKey(validator.pk.Serialize()),
					currentSlot,
					spectypes.DomainProposer,
				)

				mu.Lock()
				defer mu.Unlock()

				result := validatorResults[validator.pk.SerializeToHexStr()]
				if err != nil {
					result.errs++
					require.ErrorContains(t, err, "slashable proposal (HighestProposalVote), not signing")
				} else {
					sig := &bls.Sign{}
					require.NoError(t, sig.Deserialize(sigBytes[:]))

					// Perform BLS verification with public key and computed signing root.
					if !sig.VerifyByte(validator.pk, root[:]) {
						require.Truef(t, sig.VerifyByte(validator.pk, root[:]), "BLS verification failed for signature %x", sigBytes)
					}
					result.signs++
				}
			}()
		}
	}
	wg.Wait()

	// Validate that for each validator, only one signing succeeded, and the rest failed
	for _, result := range validatorResults {
		require.Equal(t, 1, result.signs)
		require.Equal(t, goroutinesPerValidator-1, result.errs)
	}
}

// TestComprehensiveSlashingBlockProposal tests various block proposal slashing scenarios in sequence
func TestComprehensiveSlashingBlockProposal(t *testing.T) {
	operatorPrivateKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	km, network := testKeyManager(t, nil, operatorPrivateKey)

	// Initialize test share key
	require.NoError(t, bls.Init(bls.BLS12_381))
	sharePrivKey := &bls.SecretKey{}
	sharePrivKey.SetByCSPRNG()
	sharePubKey := phase0.BLSPubKey(sharePrivKey.GetPublicKey().Serialize())

	// Add the share to the key manager
	encryptedPrivKey, err := operatorPrivateKey.Public().Encrypt([]byte(sharePrivKey.SerializeToHexStr()))
	require.NoError(t, err)
	require.NoError(t, km.AddShare(context.Background(), encryptedPrivKey, sharePubKey))

	// --- First Block Proposal ---
	slotToSign := network.Beacon.EstimatedCurrentSlot() + 5 // Sign a block slightly in the future

	// Check directly with IsBeaconBlockSlashable
	err = km.IsBeaconBlockSlashable(sharePubKey, slotToSign)
	require.NoError(t, err, "First block should not be slashable")

	block1 := testingutils.TestingBeaconBlockCapella
	block1.Slot = slotToSign

	// Sign the block - should succeed
	_, _, err = km.SignBeaconObject(
		context.Background(),
		block1,
		phase0.Domain{},
		sharePubKey,
		slotToSign,
		spectypes.DomainProposer,
	)
	require.NoError(t, err, "Signing first block failed")

	// --- Second Block Proposal (Same Slot) ---
	// Create a different block at same slot
	block2 := testingutils.TestingBeaconBlockCapella
	block2.Slot = slotToSign
	block2.ParentRoot = phase0.Root{0x02} // Different parent root

	// Attempt to sign second block - should fail
	_, _, err = km.SignBeaconObject(
		context.Background(),
		block2,
		phase0.Domain{},
		sharePubKey,
		slotToSign,
		spectypes.DomainProposer,
	)
	require.Error(t, err, "Signing second block at same slot should fail")
	require.Contains(t, err.Error(), "slashable proposal", "Error message should indicate slashable proposal")

	// --- Third Block Proposal (Lower Slot) ---
	// Create a block at a lower slot
	slotToSignLower := slotToSign - 1
	block3 := testingutils.TestingBeaconBlockCapella
	block3.Slot = slotToSignLower

	// Attempt to sign - should fail
	_, _, err = km.SignBeaconObject(
		context.Background(),
		block3,
		phase0.Domain{},
		sharePubKey,
		slotToSignLower,
		spectypes.DomainProposer,
	)
	require.Error(t, err, "Signing block at lower slot should fail")
	require.Contains(t, err.Error(), "slashable proposal", "Error message should indicate slashable proposal")

	// --- Fourth Block Proposal (Higher Slot) ---
	slotToSignHigher := slotToSign + 1
	block4 := testingutils.TestingBeaconBlockCapella
	block4.Slot = slotToSignHigher

	// Attempt to sign - should succeed
	_, _, err = km.SignBeaconObject(
		context.Background(),
		block4,
		phase0.Domain{},
		sharePubKey,
		slotToSignHigher,
		spectypes.DomainProposer,
	)
	require.NoError(t, err, "Signing block at higher slot failed")
}

func TestSlashableBlockDoubleProposal(t *testing.T) {
	logger := logging.TestLogger(t)
	db, err := getBaseStorage(logger)
	require.NoError(t, err)
	defer db.Close()

	mockBeacon := utils.SetupMockBeaconNetwork(t, nil)
	signerStore := NewSignerStorage(db, mockBeacon, logger)
	protection := slashingprotection.NewNormalProtection(signerStore)
	protector := NewSlashingProtector(logger, signerStore, protection)

	// Initialize test share key
	require.NoError(t, bls.Init(bls.BLS12_381))
	sharePrivKey := &bls.SecretKey{}
	sharePrivKey.SetByCSPRNG()
	sharePubKey := phase0.BLSPubKey(sharePrivKey.GetPublicKey().Serialize())

	// Setup for slashing protection
	err = protector.BumpSlashingProtection(sharePubKey)
	require.NoError(t, err, "Failed to bump slashing protection")

	// --- First Block Proposal ---
	slotToSign := mockBeacon.EstimatedCurrentSlot() + 5 // Sign a block slightly in the future

	// Check if first block at this slot is slashable - should not be
	err = protector.IsBeaconBlockSlashable(sharePubKey, slotToSign)
	require.NoError(t, err, "First block should not be slashable")

	// Update slashing protection as if we've signed this block
	err = protector.UpdateHighestProposal(sharePubKey, slotToSign)
	require.NoError(t, err, "Failed to update highest proposal")

	// --- Second Block Proposal (Same Slot) ---
	// Attempt to sign the second block at the same slot - should fail
	err = protector.IsBeaconBlockSlashable(sharePubKey, slotToSign)
	require.Error(t, err, "Signing second block at same slot should fail")
	require.Contains(t, err.Error(), "slashable proposal", "Error message should indicate slashable proposal")

	// --- Third Block Proposal (Lower Slot) ---
	// Attempt to sign a block at a lower slot - should fail
	err = protector.IsBeaconBlockSlashable(sharePubKey, slotToSign-1)
	require.Error(t, err, "Signing block at lower slot should fail")
	require.Contains(t, err.Error(), "slashable proposal", "Error message should indicate slashable proposal")

	// --- Fourth Block Proposal (Higher Slot) ---
	slotToSignHigher := slotToSign + 1

	// Attempt to sign a block at a higher slot - should succeed
	err = protector.IsBeaconBlockSlashable(sharePubKey, slotToSignHigher)
	require.NoError(t, err, "Signing block at higher slot failed")

	// Update slashing protection
	err = protector.UpdateHighestProposal(sharePubKey, slotToSignHigher)
	require.NoError(t, err, "Failed to update highest proposal")
}

func TestSlashableAttestationDoubleVote(t *testing.T) {
	logger := logging.TestLogger(t)
	db, err := getBaseStorage(logger)
	require.NoError(t, err)
	defer db.Close()

	mockBeacon := utils.SetupMockBeaconNetwork(t, nil)
	signerStore := NewSignerStorage(db, mockBeacon, logger)
	protection := slashingprotection.NewNormalProtection(signerStore)
	protector := NewSlashingProtector(logger, signerStore, protection)

	// Initialize test share key
	require.NoError(t, bls.Init(bls.BLS12_381))
	sharePrivKey := &bls.SecretKey{}
	sharePrivKey.SetByCSPRNG()
	sharePubKey := phase0.BLSPubKey(sharePrivKey.GetPublicKey().Serialize())

	// Setup for slashing protection
	err = protector.BumpSlashingProtection(sharePubKey)
	require.NoError(t, err, "Failed to bump slashing protection")

	// --- First Attestation ---
	epochToSign := mockBeacon.EstimatedCurrentEpoch() + 2
	slotToSign := mockBeacon.FirstSlotAtEpoch(epochToSign)

	attData1 := testingutils.TestingAttestationData(spec.DataVersionPhase0)
	attData1.Slot = slotToSign
	attData1.Source.Epoch = epochToSign - 1
	attData1.Target.Epoch = epochToSign
	attData1.BeaconBlockRoot = phase0.Root{0xAA} // Different root
	attData1.Source = &phase0.Checkpoint{Epoch: epochToSign - 1, Root: phase0.Root{0xBB}}
	attData1.Target = &phase0.Checkpoint{Epoch: epochToSign, Root: phase0.Root{0xCC}} // Same target epoch

	// Check if first attestation is slashable - should not be
	err = protector.IsAttestationSlashable(sharePubKey, attData1)
	require.NoError(t, err, "First attestation should not be slashable")

	// Update slashing protection as if we've signed this attestation
	err = protector.UpdateHighestAttestation(sharePubKey, attData1)
	require.NoError(t, err, "Failed to update highest attestation")

	// --- Second Attestation (Same Target Epoch, Different Data) ---
	attData2 := testingutils.TestingAttestationData(spec.DataVersionPhase0)
	attData2.Slot = slotToSign
	attData2.Source.Epoch = epochToSign - 1
	attData2.Target.Epoch = epochToSign
	attData2.BeaconBlockRoot = phase0.Root{0xDD} // Different root
	attData2.Source = &phase0.Checkpoint{Epoch: epochToSign - 1, Root: phase0.Root{0xEE}}
	attData2.Target = &phase0.Checkpoint{Epoch: epochToSign, Root: phase0.Root{0xFF}} // Same target epoch, different root

	// Attempt to sign the second attestation - should fail (Double Vote)
	err = protector.IsAttestationSlashable(sharePubKey, attData2)
	require.Error(t, err, "Signing second attestation with same target epoch should fail")
	require.Contains(t, err.Error(), "slashable attestation", "Error message should indicate slashable attestation")

	// --- Third Attestation (Higher Target Epoch) ---
	epochToSignHigher := epochToSign + 1
	slotToSignHigher := mockBeacon.FirstSlotAtEpoch(epochToSignHigher)

	attData3 := testingutils.TestingAttestationData(spec.DataVersionPhase0)
	attData3.Slot = slotToSignHigher
	attData3.Source.Epoch = epochToSign
	attData3.Target.Epoch = epochToSignHigher
	attData3.BeaconBlockRoot = phase0.Root{0x11}                                            // Different root
	attData3.Source = &phase0.Checkpoint{Epoch: epochToSign, Root: phase0.Root{0x22}}       // Source is previous target
	attData3.Target = &phase0.Checkpoint{Epoch: epochToSignHigher, Root: phase0.Root{0x33}} // Higher target epoch

	// Attempt to sign the third attestation - should succeed
	err = protector.IsAttestationSlashable(sharePubKey, attData3)
	require.NoError(t, err, "Signing third attestation with higher target epoch failed")

	// Update slashing protection
	err = protector.UpdateHighestAttestation(sharePubKey, attData3)
	require.NoError(t, err, "Failed to update highest attestation")
}

func TestSlashableAttestationSurroundingVote(t *testing.T) {
	logger := logging.TestLogger(t)
	db, err := getBaseStorage(logger)
	require.NoError(t, err)
	defer db.Close()

	mockBeacon := utils.SetupMockBeaconNetwork(t, nil)
	signerStore := NewSignerStorage(db, mockBeacon, logger)
	protection := slashingprotection.NewNormalProtection(signerStore)
	protector := NewSlashingProtector(logger, signerStore, protection)

	// Initialize test share key
	require.NoError(t, bls.Init(bls.BLS12_381))
	sharePrivKey := &bls.SecretKey{}
	sharePrivKey.SetByCSPRNG()
	sharePubKey := phase0.BLSPubKey(sharePrivKey.GetPublicKey().Serialize())

	// Setup for slashing protection
	err = protector.BumpSlashingProtection(sharePubKey)
	require.NoError(t, err, "Failed to bump slashing protection")

	// --- First Attestation (Inner) ---
	innerSourceEpoch := mockBeacon.EstimatedCurrentEpoch() + 2
	innerTargetEpoch := innerSourceEpoch + 1
	innerSlot := mockBeacon.FirstSlotAtEpoch(innerTargetEpoch) // Slot within the target epoch

	attDataInner := testingutils.TestingAttestationData(spec.DataVersionPhase0)
	attDataInner.Slot = innerSlot
	attDataInner.BeaconBlockRoot = phase0.Root{0xAA} // Different root
	attDataInner.Source = &phase0.Checkpoint{Epoch: innerSourceEpoch, Root: phase0.Root{0xBB}}
	attDataInner.Target = &phase0.Checkpoint{Epoch: innerTargetEpoch, Root: phase0.Root{0xCC}} // Same target epoch

	// Check if inner attestation is slashable - should not be
	err = protector.IsAttestationSlashable(sharePubKey, attDataInner)
	require.NoError(t, err, "Inner attestation should not be slashable")

	// Update slashing protection as if we've signed this attestation
	err = protector.UpdateHighestAttestation(sharePubKey, attDataInner)
	require.NoError(t, err, "Failed to update highest attestation for inner")

	// --- Second Attestation (Surrounding) ---
	outerSourceEpoch := innerSourceEpoch - 1 // Lower source
	outerTargetEpoch := innerTargetEpoch + 1 // Higher target
	outerSlot := mockBeacon.FirstSlotAtEpoch(outerTargetEpoch)

	attDataOuter := testingutils.TestingAttestationData(spec.DataVersionPhase0)
	attDataOuter.Slot = outerSlot
	attDataOuter.BeaconBlockRoot = phase0.Root{0xDD} // Different root
	attDataOuter.Source = &phase0.Checkpoint{Epoch: outerSourceEpoch, Root: phase0.Root{0xEE}}
	attDataOuter.Target = &phase0.Checkpoint{Epoch: outerTargetEpoch, Root: phase0.Root{0xFF}} // Higher target epoch

	// Attempt to sign the outer attestation - should fail (Surrounding Vote)
	err = protector.IsAttestationSlashable(sharePubKey, attDataOuter)
	require.Error(t, err, "Signing outer (surrounding) attestation should fail")
	require.Contains(t, err.Error(), "slashable attestation", "Error message should indicate slashable attestation")

	// --- Third Attestation (Non-Surrounding, Higher) ---
	higherSourceEpoch := innerSourceEpoch // Same source as inner
	higherTargetEpoch := outerTargetEpoch // Same target as outer (but source is higher than outer's source)
	higherSlot := mockBeacon.FirstSlotAtEpoch(higherTargetEpoch)

	attDataHigher := testingutils.TestingAttestationData(spec.DataVersionPhase0)
	attDataHigher.Slot = higherSlot
	attDataHigher.BeaconBlockRoot = phase0.Root{0x11} // Different root
	attDataHigher.Source = &phase0.Checkpoint{Epoch: higherSourceEpoch, Root: phase0.Root{0x22}}
	attDataHigher.Target = &phase0.Checkpoint{Epoch: higherTargetEpoch, Root: phase0.Root{0x33}} // Same target epoch

	// Attempt to sign the third attestation - should succeed
	err = protector.IsAttestationSlashable(sharePubKey, attDataHigher)
	require.NoError(t, err, "Signing higher (non-surrounding) attestation failed")

	// Update slashing protection
	err = protector.UpdateHighestAttestation(sharePubKey, attDataHigher)
	require.NoError(t, err, "Failed to update highest attestation for higher")
}

func TestSlashingDBIntegrity(t *testing.T) {
	// Create a unique database file path for the test
	dbPath := t.TempDir() + "/slashing_db.db"

	// --- Phase 1: Initial Setup, Sign, and Close ---
	logger := logging.TestLogger(t)
	db, err := kv.New(logger, basedb.Options{Path: dbPath})
	require.NoError(t, err)

	mockBeacon := utils.SetupMockBeaconNetwork(t, nil)
	signerStore := NewSignerStorage(db, mockBeacon, logger)
	protection := slashingprotection.NewNormalProtection(signerStore)
	protector := NewSlashingProtector(logger, signerStore, protection)

	// Initialize test share key
	require.NoError(t, bls.Init(bls.BLS12_381))
	sharePrivKey := &bls.SecretKey{}
	sharePrivKey.SetByCSPRNG()
	sharePubKey := phase0.BLSPubKey(sharePrivKey.GetPublicKey().Serialize())

	// Setup for slashing protection
	err = protector.BumpSlashingProtection(sharePubKey)
	require.NoError(t, err, "DB Integrity: Failed to bump slashing protection (Phase 1)")

	// Sign a block
	slotToSign := mockBeacon.EstimatedCurrentSlot() + 5
	err = protector.IsBeaconBlockSlashable(sharePubKey, slotToSign)
	require.NoError(t, err, "DB Integrity: First block should not be slashable (Phase 1)")

	// Update as if we signed
	err = protector.UpdateHighestProposal(sharePubKey, slotToSign)
	require.NoError(t, err, "DB Integrity: Failed to update highest proposal (Phase 1)")

	// Close the first database
	err = db.Close()
	require.NoError(t, err, "Failed to close database")
	t.Log("Phase 1 completed. Database closed.")

	// --- Phase 2: Reopen DB and Attempt Slashable Signing ---
	// Make sure it's a fresh instance with the same DB path
	t.Log("Starting Phase 2 with the same database path")
	db2, err := kv.New(logger, basedb.Options{Path: dbPath})
	require.NoError(t, err)
	defer db2.Close()

	signerStore2 := NewSignerStorage(db2, mockBeacon, logger)
	protection2 := slashingprotection.NewNormalProtection(signerStore2)
	protector2 := NewSlashingProtector(logger, signerStore2, protection2)

	// Attempt to sign the *same* block again - should fail due to persisted data
	t.Log("Attempting to sign the same block again - should fail")
	err = protector2.IsBeaconBlockSlashable(sharePubKey, slotToSign)
	require.Error(t, err, "DB Integrity: Signing same block after reopen should fail")
	require.Contains(t, err.Error(), "slashable proposal", "DB Integrity: Error message should indicate slashable proposal")

	// Attempt to sign a block at a *higher* slot - should succeed
	slotToSignHigher := slotToSign + 1
	err = protector2.IsBeaconBlockSlashable(sharePubKey, slotToSignHigher)
	require.NoError(t, err, "DB Integrity: Signing block at higher slot after reopen failed")
}

func TestSlashingConcurrency(t *testing.T) {
	logger := logging.TestLogger(t)
	db, err := getBaseStorage(logger)
	require.NoError(t, err)
	defer db.Close()

	mockBeacon := utils.SetupMockBeaconNetwork(t, nil)
	signerStore := NewSignerStorage(db, mockBeacon, logger)
	protection := slashingprotection.NewNormalProtection(signerStore)
	protector := NewSlashingProtector(logger, signerStore, protection)

	// Initialize test share key
	require.NoError(t, bls.Init(bls.BLS12_381))
	sharePrivKey := &bls.SecretKey{}
	sharePrivKey.SetByCSPRNG()
	sharePubKey := phase0.BLSPubKey(sharePrivKey.GetPublicKey().Serialize())

	// Setup for slashing protection
	err = protector.BumpSlashingProtection(sharePubKey)
	require.NoError(t, err, "Concurrency: Failed to bump slashing protection")

	// Sign a valid block first to establish a baseline
	slotToSign := mockBeacon.EstimatedCurrentSlot() + 5
	err = protector.IsBeaconBlockSlashable(sharePubKey, slotToSign)
	require.NoError(t, err, "Concurrency: First block should not be slashable")

	// Update as if we signed
	err = protector.UpdateHighestProposal(sharePubKey, slotToSign)
	require.NoError(t, err, "Concurrency: Failed to update highest proposal")

	// Prepare a second validator to test concurrency on add/remove
	sharePrivKey2 := &bls.SecretKey{}
	sharePrivKey2.SetByCSPRNG()
	sharePubKey2 := phase0.BLSPubKey(sharePrivKey2.GetPublicKey().Serialize())

	numGoroutines := 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(routineID int) {
			defer wg.Done()

			// Mix of operations
			switch routineID % 4 {
			case 0: // Attempt to sign the already signed (slashable) block
				err := protector.IsBeaconBlockSlashable(sharePubKey, slotToSign)
				// We expect an error here because it's slashable
				require.Error(t, err, "Goroutine %d: Expected error signing slashable block", routineID)
				require.Contains(t, err.Error(), "slashable proposal", "Goroutine %d: Error message mismatch", routineID)

			case 1: // Attempt to sign a valid future block
				futureSlot := slotToSign + phase0.Slot(routineID) + 1 // Ensure unique future slots
				err := protector.IsBeaconBlockSlashable(sharePubKey, futureSlot)
				if err == nil {
					// Update if it was signable
					_ = protector.UpdateHighestProposal(sharePubKey, futureSlot)
				} else {
					// If it fails, it should be due to slashing
					require.Contains(t, err.Error(), "slashable proposal", "Goroutine %d: Error signing future block", routineID)
				}

			case 2: // Add validation for second key
				err := protector.BumpSlashingProtection(sharePubKey2)
				// This is an idempotent operation so should not error
				if err != nil {
					t.Logf("Goroutine %d: BumpSlashingProtection returned error: %v", routineID, err)
				}

			case 3: // Check a new attestation
				epoch := mockBeacon.EstimatedCurrentEpoch() + phase0.Epoch(routineID%10) + 1
				slot := mockBeacon.FirstSlotAtEpoch(epoch)
				attData := &phase0.AttestationData{
					Slot:            slot,
					Index:           0,
					BeaconBlockRoot: phase0.Root{byte(routineID % 256)},
					Source:          &phase0.Checkpoint{Epoch: epoch - 1, Root: phase0.Root{byte(routineID % 256)}},
					Target:          &phase0.Checkpoint{Epoch: epoch, Root: phase0.Root{byte(routineID % 256)}},
				}

				err := protector.IsAttestationSlashable(sharePubKey, attData)
				if err == nil {
					// Update if it was signable
					_ = protector.UpdateHighestAttestation(sharePubKey, attData)
				}
			}
		}(i)
	}

	wg.Wait()

	// Final check: Ensure the original slashable block cannot be signed
	err = protector.IsBeaconBlockSlashable(sharePubKey, slotToSign)
	require.Error(t, err, "Concurrency: Final check failed - slashable block was signable")
	require.Contains(t, err.Error(), "slashable proposal", "Concurrency: Final check error message mismatch")
}
