package ekm

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/holiman/uint256"

	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/prysmaticlabs/go-bitfield"
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

	attestationData := &phase0.AttestationData{
		Slot:            currentSlot,
		Index:           1,
		BeaconBlockRoot: [32]byte{1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2},
		Source: &phase0.Checkpoint{
			Epoch: highestSource,
			Root:  [32]byte{},
		},
		Target: &phase0.Checkpoint{
			Epoch: highestTarget,
			Root:  [32]byte{},
		},
	}

	var beaconBlock = &capella.BeaconBlock{
		Slot:          highestProposal,
		ProposerIndex: 0,
		ParentRoot: phase0.Root{
			0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
			0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
		},
		StateRoot: phase0.Root{
			0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f,
			0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f,
		},
		Body: &capella.BeaconBlockBody{
			RANDAOReveal: phase0.BLSSignature{
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
				0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
				0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
				0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
			},
			ETH1Data: &phase0.ETH1Data{
				DepositRoot: phase0.Root{
					0x40, 0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f,
					0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5a, 0x5b, 0x5c, 0x5d, 0x5e, 0x5f,
				},
				DepositCount: 0,
				BlockHash: []byte{
					0x60, 0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69, 0x6a, 0x6b, 0x6c, 0x6d, 0x6e, 0x6f,
					0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79, 0x7a, 0x7b, 0x7c, 0x7d, 0x7e, 0x7f,
				},
			},
			Graffiti: [32]byte{
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
				0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
			},
			ProposerSlashings: []*phase0.ProposerSlashing{},
			AttesterSlashings: []*phase0.AttesterSlashing{},
			Attestations:      []*phase0.Attestation{},
			Deposits:          []*phase0.Deposit{},
			VoluntaryExits:    []*phase0.SignedVoluntaryExit{},
			SyncAggregate: &altair.SyncAggregate{
				SyncCommitteeBits: bitfield.Bitvector512{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
				SyncCommitteeSignature: phase0.BLSSignature{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
			},
			ExecutionPayload: &capella.ExecutionPayload{
				ParentHash: phase0.Hash32{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
				FeeRecipient: bellatrix.ExecutionAddress{},
				StateRoot: [32]byte{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
				ReceiptsRoot: [32]byte{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
				LogsBloom: [256]byte{},
				PrevRandao: [32]byte{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
				BlockNumber: 0,
				GasLimit:    0,
				GasUsed:     0,
				Timestamp:   0,
				ExtraData:   nil,
				BaseFeePerGas: [32]byte{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
				BlockHash: phase0.Hash32{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
				Transactions: []bellatrix.Transaction{},
				Withdrawals:  []*capella.Withdrawal{},
			},
		},
	}

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

	attestationData := buildAttestationData(currentSlot, highestSource, highestTarget)

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

	// Set up concurrency.
	const goroutineCount = 1000

	var wg sync.WaitGroup

	errChan := make(chan error, goroutineCount)

	for range goroutineCount {
		wg.Add(1)

		go signAttestation(&wg, errChan)
	}

	// Wait for all goroutines to complete.
	wg.Wait()
	close(errChan)

	// Count errors and successes.
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

	// Define function to concurrently attempt signing.
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

	// Set up concurrency.
	const goroutineCount = 1000

	var wg sync.WaitGroup

	errChan := make(chan error, goroutineCount)

	for range goroutineCount {
		wg.Add(1)

		go signBeaconBlock(&wg, errChan)
	}

	// Wait for all goroutines to complete.
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

	attestationData := buildAttestationData(currentSlot, highestSource, highestTarget)

	// Map to store results per validator
	type validatorResult struct {
		signs int
		errs  int
	}

	validatorResults := make(map[string]*validatorResult)
	for _, v := range testValidators {
		validatorResults[v.pk.SerializeToHexStr()] = &validatorResult{}
	}

	var mu sync.Mutex

	// Run signing attempts in parallel for each validator
	const goroutinesPerValidator = 100

	var wg sync.WaitGroup

	for _, validator := range testValidators {
		validator := validator // capture range variable

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

	for range 3 { // Adjust the number of validators as needed
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

	// Map to store results per validator
	type validatorResult struct {
		signs int
		errs  int
	}

	validatorResults := make(map[string]*validatorResult)
	for _, v := range testValidators {
		validatorResults[v.pk.SerializeToHexStr()] = &validatorResult{}
	}

	var mu sync.Mutex

	// Run signing attempts in parallel for each validator
	const goroutinesPerValidator = 100

	var wg sync.WaitGroup

	for _, validator := range testValidators {
		validator := validator // capture range variable

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

// buildAttestationData creates a new AttestationData structure with the provided parameters.
func buildAttestationData(slot phase0.Slot, highestSource, highestTarget phase0.Epoch) *phase0.AttestationData {
	return &phase0.AttestationData{
		Slot:            slot,
		Index:           1,
		BeaconBlockRoot: [32]byte{1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2},
		Source: &phase0.Checkpoint{
			Epoch: highestSource,
			Root:  [32]byte{},
		},
		Target: &phase0.Checkpoint{
			Epoch: highestTarget,
			Root:  [32]byte{},
		},
	}
}

// TestComprehensiveSlashingBlockProposal tests various block proposal slashing scenarios in sequence
func TestComprehensiveSlashingBlockProposal(t *testing.T) {
	// Use testKeyManager instead of manual setup
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

	// Create a block
	block1 := &capella.BeaconBlock{
		Slot:          slotToSign,
		ProposerIndex: 0,
		ParentRoot:    phase0.Root{0x01},
		Body: &capella.BeaconBlockBody{
			ETH1Data: &phase0.ETH1Data{
				DepositRoot:  phase0.Root{},
				DepositCount: 0,
				BlockHash:    []byte{0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01},
			},
			SyncAggregate: &altair.SyncAggregate{
				SyncCommitteeBits: bitfield.Bitvector512{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
				SyncCommitteeSignature: phase0.BLSSignature{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
			},
			// Add ExecutionPayload with all required fields
			ExecutionPayload: &capella.ExecutionPayload{
				ParentHash: phase0.Hash32{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
				FeeRecipient: bellatrix.ExecutionAddress{},
				StateRoot: [32]byte{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
				ReceiptsRoot: [32]byte{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
				LogsBloom: [256]byte{},
				PrevRandao: [32]byte{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
				BlockNumber: 0,
				GasLimit:    0,
				GasUsed:     0,
				Timestamp:   0,
				ExtraData:   nil,
				BaseFeePerGas: [32]byte{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
				BlockHash: phase0.Hash32{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
				Transactions: []bellatrix.Transaction{},
				Withdrawals:  []*capella.Withdrawal{},
			},
		},
	}

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
	block2 := &capella.BeaconBlock{
		Slot:          slotToSign,
		ProposerIndex: 0,
		ParentRoot:    phase0.Root{0x02}, // Different parent root
		Body: &capella.BeaconBlockBody{
			ETH1Data: &phase0.ETH1Data{
				DepositRoot:  phase0.Root{},
				DepositCount: 0,
				BlockHash:    []byte{0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02},
			},
			SyncAggregate: &altair.SyncAggregate{
				SyncCommitteeBits: bitfield.Bitvector512{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
				SyncCommitteeSignature: phase0.BLSSignature{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
			},
			ExecutionPayload: &capella.ExecutionPayload{
				ParentHash: phase0.Hash32{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
				FeeRecipient: bellatrix.ExecutionAddress{},
				StateRoot: [32]byte{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
				ReceiptsRoot: [32]byte{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
				LogsBloom: [256]byte{},
				PrevRandao: [32]byte{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
				BlockNumber: 0,
				GasLimit:    0,
				GasUsed:     0,
				Timestamp:   0,
				ExtraData:   nil,
				BaseFeePerGas: [32]byte{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
				BlockHash: phase0.Hash32{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
				Transactions: []bellatrix.Transaction{},
				Withdrawals:  []*capella.Withdrawal{},
			},
		},
	}

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
	block3 := &capella.BeaconBlock{
		Slot:          slotToSign - 1, // Lower slot
		ProposerIndex: 0,
		ParentRoot:    phase0.Root{0x03},
		Body: &capella.BeaconBlockBody{
			ETH1Data: &phase0.ETH1Data{
				DepositRoot:  phase0.Root{},
				DepositCount: 0,
				BlockHash:    []byte{0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03, 0x03},
			},
			SyncAggregate: &altair.SyncAggregate{
				SyncCommitteeBits: bitfield.Bitvector512{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
				SyncCommitteeSignature: phase0.BLSSignature{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
			},
			// Add ExecutionPayload with all required fields
			ExecutionPayload: &capella.ExecutionPayload{
				ParentHash:    [32]byte{0x03},
				FeeRecipient:  [20]byte{0x03},
				StateRoot:     [32]byte{0x03},
				ReceiptsRoot:  [32]byte{0x03},
				LogsBloom:     [256]byte{0x03},
				PrevRandao:    [32]byte{0x03},
				BlockNumber:   3,
				GasLimit:      3,
				GasUsed:       3,
				Timestamp:     3,
				ExtraData:     []byte{0x03},
				BaseFeePerGas: uint256.NewInt(3).Bytes32(),
				BlockHash:     [32]byte{0x03},
				Transactions:  []bellatrix.Transaction{},
				Withdrawals:   []*capella.Withdrawal{},
			},
		},
	}

	// Attempt to sign - should fail
	_, _, err = km.SignBeaconObject(
		context.Background(),
		block3,
		phase0.Domain{},
		sharePubKey,
		slotToSign-1,
		spectypes.DomainProposer,
	)
	require.Error(t, err, "Signing block at lower slot should fail")
	require.Contains(t, err.Error(), "slashable proposal", "Error message should indicate slashable proposal")

	// --- Fourth Block Proposal (Higher Slot) ---
	slotToSignHigher := slotToSign + 1
	block4 := &capella.BeaconBlock{
		Slot:          slotToSignHigher,
		ProposerIndex: 0,
		ParentRoot:    phase0.Root{0x04},
		Body: &capella.BeaconBlockBody{
			ETH1Data: &phase0.ETH1Data{
				DepositRoot:  phase0.Root{},
				DepositCount: 0,
				BlockHash:    []byte{0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04},
			},
			SyncAggregate: &altair.SyncAggregate{
				SyncCommitteeBits: bitfield.Bitvector512{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
				SyncCommitteeSignature: phase0.BLSSignature{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
			},
			// Add ExecutionPayload with all required fields
			ExecutionPayload: &capella.ExecutionPayload{
				ParentHash:    [32]byte{0x04},
				FeeRecipient:  [20]byte{0x04},
				StateRoot:     [32]byte{0x04},
				ReceiptsRoot:  [32]byte{0x04},
				LogsBloom:     [256]byte{0x04},
				PrevRandao:    [32]byte{0x04},
				BlockNumber:   4,
				GasLimit:      4,
				GasUsed:       4,
				Timestamp:     4,
				ExtraData:     []byte{0x04},
				BaseFeePerGas: uint256.NewInt(4).Bytes32(),
				BlockHash:     [32]byte{0x04},
				Transactions:  []bellatrix.Transaction{},
				Withdrawals:   []*capella.Withdrawal{},
			},
		},
	}

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

// The following tests are added/adapted from key_manager_slashing_test.go
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
	attData1 := &phase0.AttestationData{
		Slot:            slotToSign,
		Index:           0,
		BeaconBlockRoot: phase0.Root{0xAA}, // Different root
		Source:          &phase0.Checkpoint{Epoch: epochToSign - 1, Root: phase0.Root{0xBB}},
		Target:          &phase0.Checkpoint{Epoch: epochToSign, Root: phase0.Root{0xCC}}, // Same target epoch
	}

	// Check if first attestation is slashable - should not be
	err = protector.IsAttestationSlashable(sharePubKey, attData1)
	require.NoError(t, err, "First attestation should not be slashable")

	// Update slashing protection as if we've signed this attestation
	err = protector.UpdateHighestAttestation(sharePubKey, attData1)
	require.NoError(t, err, "Failed to update highest attestation")

	// --- Second Attestation (Same Target Epoch, Different Data) ---
	attData2 := &phase0.AttestationData{
		Slot:            slotToSign,
		Index:           0,
		BeaconBlockRoot: phase0.Root{0xDD}, // Different root
		Source:          &phase0.Checkpoint{Epoch: epochToSign - 1, Root: phase0.Root{0xEE}},
		Target:          &phase0.Checkpoint{Epoch: epochToSign, Root: phase0.Root{0xFF}}, // Same target epoch, different root
	}

	// Attempt to sign the second attestation - should fail (Double Vote)
	err = protector.IsAttestationSlashable(sharePubKey, attData2)
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
	attDataInner := &phase0.AttestationData{
		Slot:            innerSlot,
		Index:           0,
		BeaconBlockRoot: phase0.Root{0xAA},
		Source:          &phase0.Checkpoint{Epoch: innerSourceEpoch, Root: phase0.Root{0xBB}},
		Target:          &phase0.Checkpoint{Epoch: innerTargetEpoch, Root: phase0.Root{0xCC}},
	}

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
	attDataOuter := &phase0.AttestationData{
		Slot:            outerSlot,
		Index:           0,
		BeaconBlockRoot: phase0.Root{0xDD},
		Source:          &phase0.Checkpoint{Epoch: outerSourceEpoch, Root: phase0.Root{0xEE}},
		Target:          &phase0.Checkpoint{Epoch: outerTargetEpoch, Root: phase0.Root{0xFF}},
	}

	// Attempt to sign the outer attestation - should fail (Surrounding Vote)
	err = protector.IsAttestationSlashable(sharePubKey, attDataOuter)
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

	numGoroutines := 50 // Number of concurrent operations
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
