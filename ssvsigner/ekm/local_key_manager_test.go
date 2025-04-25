package ekm

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/prysmaticlabs/go-bitfield"
	"github.com/ssvlabs/eth2-key-manager/core"
	"github.com/ssvlabs/eth2-key-manager/wallets/hd"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/utils"
	"github.com/ssvlabs/ssv/utils/threshold"

	"github.com/ssvlabs/ssv/ssvsigner/keys"
)

const (
	sk1Str = "3548db63ab5701878daf25fa877638dc7809778815b9d9ecd5369da33ca9e64f"
	pk1Str = "a8cb269bd7741740cfe90de2f8db6ea35a9da443385155da0fa2f621ba80e5ac14b5c8f65d23fd9ccc170cc85f29e27d"
	sk2Str = "66dd37ae71b35c81022cdde98370e881cff896b689fa9136917f45afce43fd3b"
	pk2Str = "8796fafa576051372030a75c41caafea149e4368aebaca21c9f90d9974b3973d5cee7d7874e4ec9ec59fb2c8945b3e01"
)

func testKeyManager(t *testing.T, network *networkconfig.NetworkConfig, operatorPrivateKey keys.OperatorPrivateKey) (KeyManager, *networkconfig.NetworkConfig) {
	threshold.Init()

	logger := logging.TestLogger(t)

	db, err := getBaseStorage(logger)
	require.NoError(t, err)

	if network == nil {
		network = &networkconfig.NetworkConfig{}
		network.Beacon = utils.SetupMockBeaconNetwork(t, nil)
		network.DomainType = networkconfig.TestNetwork.DomainType
	}

	km, err := NewLocalKeyManager(logger, db, *network, operatorPrivateKey)
	require.NoError(t, err)

	sk1 := &bls.SecretKey{}
	require.NoError(t, sk1.SetHexString(sk1Str))

	sk2 := &bls.SecretKey{}
	require.NoError(t, sk2.SetHexString(sk2Str))

	encryptedSK1, err := operatorPrivateKey.Public().Encrypt([]byte(sk1.SerializeToHexStr()))
	require.NoError(t, err)

	encryptedSK2, err := operatorPrivateKey.Public().Encrypt([]byte(sk2.SerializeToHexStr()))
	require.NoError(t, err)

	require.NoError(t, km.AddShare(context.Background(), encryptedSK1, phase0.BLSPubKey(sk1.GetPublicKey().Serialize())))
	require.NoError(t, km.AddShare(context.Background(), encryptedSK2, phase0.BLSPubKey(sk2.GetPublicKey().Serialize())))

	return km, network
}

func TestEncryptedKeyManager(t *testing.T) {
	// Generate key 1.
	privateKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	encryptionKey := privateKey.EKMHash()

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
	privateKey2, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	encryptionKey2 := privateKey2.EKMHash()

	// Load account with key 2 (should fail).
	wallet2, err := signerStorage.OpenWallet()
	require.NoError(t, err)
	err = signerStorage.SetEncryptionKey(encryptionKey2)
	require.NoError(t, err)
	_, err = wallet2.AccountByPublicKey(hex.EncodeToString(a.ValidatorPublicKey()))
	require.ErrorContains(t, err, "decrypt stored wallet")

	// Retry with key 1 (should succeed).
	wallet3, err := signerStorage.OpenWallet()
	require.NoError(t, err)
	err = signerStorage.SetEncryptionKey(encryptionKey)
	require.NoError(t, err)
	_, err = wallet3.AccountByPublicKey(hex.EncodeToString(a.ValidatorPublicKey()))
	require.NoError(t, err)
}

func TestSignBeaconObject(t *testing.T) {
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
	highestProposal := currentSlot + minSPProposalSlotGap + 1

	t.Run("Sign Deneb block", func(t *testing.T) {
		beaconBlock := testingutils.TestingBlockContentsDeneb.Block
		beaconBlock.Slot = highestProposal

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
	t.Run("DomainVoluntaryExit", func(t *testing.T) {
		voluntaryExit := testingutils.TestingVoluntaryExit

		_, sig, err := km.(*LocalKeyManager).SignBeaconObject(
			ctx,
			voluntaryExit,
			phase0.Domain{},
			phase0.BLSPubKey(sk1.GetPublicKey().Serialize()),
			currentSlot,
			spectypes.DomainVoluntaryExit,
		)
		require.NoError(t, err)
		require.NotNil(t, sig)
		require.NotEqual(t, [32]byte{}, sig)
	})
	t.Run("DomainAggregateAndProof", func(t *testing.T) {
		aggregateAndProof := testingutils.TestingPhase0AggregateAndProof

		_, sig, err := km.(*LocalKeyManager).SignBeaconObject(
			ctx,
			aggregateAndProof,
			phase0.Domain{},
			phase0.BLSPubKey(sk1.GetPublicKey().Serialize()),
			currentSlot,
			spectypes.DomainAggregateAndProof,
		)
		require.NoError(t, err)
		require.NotNil(t, sig)
		require.NotEqual(t, [32]byte{}, sig)
	})
	t.Run("DomainSelectionProof", func(t *testing.T) {
		_, sig, err := km.(*LocalKeyManager).SignBeaconObject(
			ctx,
			spectypes.SSZUint64(1),
			phase0.Domain{},
			phase0.BLSPubKey(sk1.GetPublicKey().Serialize()),
			currentSlot,
			spectypes.DomainSelectionProof,
		)
		require.NoError(t, err)
		require.NotNil(t, sig)
		require.NotEqual(t, [32]byte{}, sig)
	})
	t.Run("DomainRandao", func(t *testing.T) {
		_, sig, err := km.(*LocalKeyManager).SignBeaconObject(
			ctx,
			spectypes.SSZUint64(1),
			phase0.Domain{},
			phase0.BLSPubKey(sk1.GetPublicKey().Serialize()),
			currentSlot,
			spectypes.DomainRandao,
		)
		require.NoError(t, err)
		require.NotNil(t, sig)
		require.NotEqual(t, [32]byte{}, sig)
	})
	t.Run("DomainSyncCommittee", func(t *testing.T) {
		data := spectypes.SSZBytes{
			0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
			0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
		}
		_, sig, err := km.(*LocalKeyManager).SignBeaconObject(
			ctx,
			data,
			phase0.Domain{},
			phase0.BLSPubKey(sk1.GetPublicKey().Serialize()),
			currentSlot,
			spectypes.DomainSyncCommittee,
		)
		require.NoError(t, err)
		require.NotNil(t, sig)
		require.NotEqual(t, [32]byte{}, sig)
	})
	t.Run("DomainSyncCommitteeSelectionProof", func(t *testing.T) {
		data := &altair.SyncAggregatorSelectionData{
			Slot:              currentSlot,
			SubcommitteeIndex: 1,
		}
		_, sig, err := km.(*LocalKeyManager).SignBeaconObject(
			ctx,
			data,
			phase0.Domain{},
			phase0.BLSPubKey(sk1.GetPublicKey().Serialize()),
			currentSlot,
			spectypes.DomainSyncCommitteeSelectionProof)
		require.NoError(t, err)
		require.NotNil(t, sig)
		require.NotEqual(t, [32]byte{}, sig)
	})
	t.Run("DomainContributionAndProof", func(t *testing.T) {
		data := &altair.ContributionAndProof{
			AggregatorIndex: 1,
			Contribution: &altair.SyncCommitteeContribution{
				Slot:              currentSlot,
				BeaconBlockRoot:   [32]byte{1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2},
				SubcommitteeIndex: 1,
				AggregationBits:   bitfield.Bitvector128{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 0xa, 0xb, 0xc, 0xd, 0xe},
				Signature: phase0.BLSSignature{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				},
			},
			SelectionProof: phase0.BLSSignature{
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
				0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
				0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
				0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
			},
		}
		_, sig, err := km.(*LocalKeyManager).SignBeaconObject(
			ctx,
			data,
			phase0.Domain{},
			phase0.BLSPubKey(sk1.GetPublicKey().Serialize()),
			currentSlot,
			spectypes.DomainContributionAndProof)
		require.NoError(t, err)
		require.NotNil(t, sig)
		require.NotEqual(t, [32]byte{}, sig)
	})
	t.Run("DomainApplicationBuilder", func(t *testing.T) {
		pk := &bls.SecretKey{}
		pk.SetByCSPRNG()

		data := &eth2apiv1.ValidatorRegistration{
			GasLimit:     123,
			FeeRecipient: bellatrix.ExecutionAddress{},
			Timestamp:    time.Unix(1231006505, 0),
			Pubkey: phase0.BLSPubKey{
				0x0a, 0x0d, 0x0e, 0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x0b, 0x0e, 0x0e, 0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x0c, 0x0f, 0x0e, 0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
		}
		_, sig, err := km.(*LocalKeyManager).SignBeaconObject(
			ctx,
			data,
			phase0.Domain{},
			phase0.BLSPubKey(sk1.GetPublicKey().Serialize()),
			currentSlot,
			spectypes.DomainApplicationBuilder)
		require.NoError(t, err)
		require.NotNil(t, sig)
		require.NotEqual(t, [32]byte{}, sig)
	})
}

func TestRemoveShare(t *testing.T) {
	require.NoError(t, bls.Init(bls.BLS12_381))

	operatorPrivateKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	t.Run("key exists", func(t *testing.T) {
		km, _ := testKeyManager(t, nil, operatorPrivateKey)
		pk := &bls.SecretKey{}
		// generate random key
		pk.SetByCSPRNG()

		encryptedPrivKey, err := operatorPrivateKey.Public().Encrypt([]byte(pk.SerializeToHexStr()))
		require.NoError(t, err)

		require.NoError(t, km.AddShare(context.Background(), encryptedPrivKey, phase0.BLSPubKey(pk.GetPublicKey().Serialize())))
		require.NoError(t, km.RemoveShare(context.Background(), phase0.BLSPubKey(pk.GetPublicKey().Serialize())))
	})

	t.Run("key doesn't exist", func(t *testing.T) {
		km, _ := testKeyManager(t, nil, operatorPrivateKey)

		pk := &bls.SecretKey{}
		pk.SetByCSPRNG()

		err := km.RemoveShare(context.Background(), phase0.BLSPubKey(pk.GetPublicKey().Serialize()))
		require.NoError(t, err)
	})
}

func TestEkmListAccounts(t *testing.T) {
	require.NoError(t, bls.Init(bls.BLS12_381))

	operatorPrivateKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	km, _ := testKeyManager(t, nil, operatorPrivateKey)
	accounts, err := km.(*LocalKeyManager).ListAccounts()
	require.NoError(t, err)
	require.Len(t, accounts, 2)
}
