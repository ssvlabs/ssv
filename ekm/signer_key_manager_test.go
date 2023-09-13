package ekm

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/eth2-key-manager/wallets/hd"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-bitfield"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/bloxapp/ssv/utils/threshold"
)

const (
	sk1Str = "3548db63ab5701878daf25fa877638dc7809778815b9d9ecd5369da33ca9e64f"
	pk1Str = "a8cb269bd7741740cfe90de2f8db6ea35a9da443385155da0fa2f621ba80e5ac14b5c8f65d23fd9ccc170cc85f29e27d"
	sk2Str = "66dd37ae71b35c81022cdde98370e881cff896b689fa9136917f45afce43fd3b"
	pk2Str = "8796fafa576051372030a75c41caafea149e4368aebaca21c9f90d9974b3973d5cee7d7874e4ec9ec59fb2c8945b3e01"
)

func testKeyManager(t *testing.T, network *networkconfig.NetworkConfig) spectypes.KeyManager {
	threshold.Init()

	logger := logging.TestLogger(t)

	db, err := getBaseStorage(logger)
	require.NoError(t, err)

	if network == nil {
		network = &networkconfig.NetworkConfig{
			Beacon: utils.SetupMockBeaconNetwork(t, nil),
			Domain: networkconfig.TestNetwork.Domain,
		}
	}

	km, err := NewETHKeyManagerSigner(logger, db, *network, true, "")
	require.NoError(t, err)

	sk1 := &bls.SecretKey{}
	require.NoError(t, sk1.SetHexString(sk1Str))

	sk2 := &bls.SecretKey{}
	require.NoError(t, sk2.SetHexString(sk2Str))

	require.NoError(t, km.AddShare(sk1))
	require.NoError(t, km.AddShare(sk2))

	return km
}

func TestEncryptedKeyManager(t *testing.T) {
	// Generate key 1.
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	keyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	encryptionKey, err := rsaencryption.HashRsaKey(keyBytes)
	require.NoError(t, err)

	// Create account with key 1.
	threshold.Init()
	sk := bls.SecretKey{}
	sk.SetByCSPRNG()
	index := 0
	logger := logging.TestLogger(t)
	db, err := getBaseStorage(logger)
	require.NoError(t, err)
	signerStorage := NewSignerStorage(db, networkconfig.TestNetwork.Beacon.GetNetwork(), logger)
	err = signerStorage.SetEncryptionKey(encryptionKey)
	require.NoError(t, err)
	defer func(db basedb.Database, logger *zap.Logger) {
		err := db.Close()
		if err != nil {
			t.Fatal(err)
		}
	}(db, logging.TestLogger(t))
	hdwallet := hd.NewWallet(&core.WalletContext{Storage: signerStorage})
	require.NoError(t, signerStorage.SaveWallet(hdwallet))
	a, err := hdwallet.CreateValidatorAccountFromPrivateKey(sk.Serialize(), &index)
	require.NoError(t, err)

	// Load account with key 1 (should succeed).
	wallet, err := signerStorage.OpenWallet()
	require.NoError(t, err)
	_, err = wallet.AccountByPublicKey(hex.EncodeToString(a.ValidatorPublicKey()))
	require.NoError(t, err)

	// Generate key 2.
	privateKey2, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	keyBytes2 := x509.MarshalPKCS1PrivateKey(privateKey2)
	encryptionKey2, err := rsaencryption.HashRsaKey(keyBytes2)
	require.NoError(t, err)

	// Load account with key 2 (should fail).
	wallet2, err := signerStorage.OpenWallet()
	require.NoError(t, err)
	err = signerStorage.SetEncryptionKey(encryptionKey2)
	require.NoError(t, err)
	_, err = wallet2.AccountByPublicKey(hex.EncodeToString(a.ValidatorPublicKey()))
	require.True(t, errors.Is(err, ErrCantDecrypt))

	// Retry with key 1 (should succeed).
	wallet3, err := signerStorage.OpenWallet()
	require.NoError(t, err)
	err = signerStorage.SetEncryptionKey(encryptionKey)
	require.NoError(t, err)
	_, err = wallet3.AccountByPublicKey(hex.EncodeToString(a.ValidatorPublicKey()))
	require.NoError(t, err)
}

func TestSlashing(t *testing.T) {
	km := testKeyManager(t, nil)

	sk1 := &bls.SecretKey{}
	require.NoError(t, sk1.SetHexString(sk1Str))
	require.NoError(t, km.AddShare(sk1))

	currentSlot := km.(*ethKeyManagerSigner).storage.Network().EstimatedCurrentSlot()
	currentEpoch := km.(*ethKeyManagerSigner).storage.Network().EstimatedEpochAtSlot(currentSlot)

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

	var beaconBlock = &bellatrix.BeaconBlock{
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
		Body: &bellatrix.BeaconBlockBody{
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
			ExecutionPayload: &bellatrix.ExecutionPayload{
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
			},
		},
	}

	t.Run("sign once", func(t *testing.T) {
		_, sig, err := km.(*ethKeyManagerSigner).SignBeaconObject(attestationData, phase0.Domain{}, sk1.GetPublicKey().Serialize(), spectypes.DomainAttester)
		require.NoError(t, err)
		require.NotNil(t, sig)
		require.NotEqual(t, [32]byte{}, sig)
	})
	t.Run("slashable sign, fail", func(t *testing.T) {
		_, sig, err := km.(*ethKeyManagerSigner).SignBeaconObject(attestationData, phase0.Domain{}, sk1.GetPublicKey().Serialize(), spectypes.DomainAttester)
		require.EqualError(t, err, "slashable attestation (HighestAttestationVote), not signing")
		require.Equal(t, [32]byte{}, sig)
	})

	t.Run("sign once", func(t *testing.T) {
		_, sig, err := km.(*ethKeyManagerSigner).SignBeaconObject(beaconBlock, phase0.Domain{}, sk1.GetPublicKey().Serialize(), spectypes.DomainProposer)
		require.NoError(t, err)
		require.NotNil(t, sig)
		require.NotEqual(t, [32]byte{}, sig)
	})
	t.Run("slashable sign, fail", func(t *testing.T) {
		_, sig, err := km.(*ethKeyManagerSigner).SignBeaconObject(beaconBlock, phase0.Domain{}, sk1.GetPublicKey().Serialize(), spectypes.DomainProposer)
		require.EqualError(t, err, "slashable proposal (HighestProposalVote), not signing")
		require.Equal(t, [32]byte{}, sig)
	})
	t.Run("slashable sign after duplicate AddShare, fail", func(t *testing.T) {
		require.NoError(t, km.AddShare(sk1))
		_, sig, err := km.(*ethKeyManagerSigner).SignBeaconObject(beaconBlock, phase0.Domain{}, sk1.GetPublicKey().Serialize(), spectypes.DomainProposer)
		require.EqualError(t, err, "slashable proposal (HighestProposalVote), not signing")
		require.Equal(t, [32]byte{}, sig)
	})
}

func TestSlashing_Attestation(t *testing.T) {
	km := testKeyManager(t, nil)

	var secretKeys [4]*bls.SecretKey
	for i := range secretKeys {
		secretKeys[i] = &bls.SecretKey{}
		secretKeys[i].SetByCSPRNG()

		// Equivalent to AddShare but with a custom slot for minimal slashing protection.
		err := km.(*ethKeyManagerSigner).BumpSlashingProtection(secretKeys[i].GetPublicKey().Serialize())
		require.NoError(t, err)
		err = km.(*ethKeyManagerSigner).saveShare(secretKeys[i])
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

	signAttestation := func(sk *bls.SecretKey, signingRoot phase0.Root, attestation *phase0.AttestationData, expectSlashing bool, expectReason string) {
		sig, root, err := km.(*ethKeyManagerSigner).SignBeaconObject(
			attestation,
			phase0.Domain{},
			sk.GetPublicKey().Serialize(),
			spectypes.DomainAttester,
		)
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

			highAtt, found, err := km.(*ethKeyManagerSigner).storage.RetrieveHighestAttestation(sk.GetPublicKey().Serialize())
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
}

func TestSignRoot(t *testing.T) {
	require.NoError(t, bls.Init(bls.BLS12_381))

	km := testKeyManager(t, nil)

	t.Run("pk 1", func(t *testing.T) {
		pk := &bls.PublicKey{}
		require.NoError(t, pk.Deserialize(_byteArray(pk1Str)))

		msg := specqbft.Message{
			MsgType:    specqbft.CommitMsgType,
			Height:     specqbft.Height(3),
			Round:      specqbft.Round(2),
			Identifier: []byte("identifier1"),
			Root:       [32]byte{1, 2, 3},
		}

		// sign
		sig, err := km.SignRoot(&msg, spectypes.QBFTSignatureType, pk.Serialize())
		require.NoError(t, err)

		// verify
		signed := &specqbft.SignedMessage{
			Signature: sig,
			Signers:   []spectypes.OperatorID{1},
			Message:   msg,
		}

		err = signed.GetSignature().VerifyByOperators(signed, networkconfig.TestNetwork.Domain, spectypes.QBFTSignatureType, []*spectypes.Operator{{OperatorID: spectypes.OperatorID(1), PubKey: pk.Serialize()}})
		// res, err := signed.VerifySig(pk)
		require.NoError(t, err)
		// require.True(t, res)
	})

	t.Run("pk 2", func(t *testing.T) {
		pk := &bls.PublicKey{}
		require.NoError(t, pk.Deserialize(_byteArray(pk2Str)))

		msg := specqbft.Message{
			MsgType:    specqbft.CommitMsgType,
			Height:     specqbft.Height(1),
			Round:      specqbft.Round(3),
			Identifier: []byte("identifier2"),
			Root:       [32]byte{4, 5, 6},
		}

		// sign
		sig, err := km.SignRoot(&msg, spectypes.QBFTSignatureType, pk.Serialize())
		require.NoError(t, err)

		// verify
		signed := &specqbft.SignedMessage{
			Signature: sig,
			Signers:   []spectypes.OperatorID{1},
			Message:   msg,
		}

		err = signed.GetSignature().VerifyByOperators(signed, networkconfig.TestNetwork.Domain, spectypes.QBFTSignatureType, []*spectypes.Operator{{OperatorID: spectypes.OperatorID(1), PubKey: pk.Serialize()}})
		// res, err := signed.VerifySig(pk)
		require.NoError(t, err)
		// require.True(t, res)
	})
}
