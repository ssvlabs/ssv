package ekm

import (
	"encoding/hex"
	"math"
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
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"

	"github.com/ssvlabs/ssv/ssvsigner/keys"
)

const (
	sk1Str = "3548db63ab5701878daf25fa877638dc7809778815b9d9ecd5369da33ca9e64f"
	sk2Str = "66dd37ae71b35c81022cdde98370e881cff896b689fa9136917f45afce43fd3b"
)

func testKeyManager(t *testing.T, operatorPrivateKey keys.OperatorPrivateKey) KeyManager {
	initBLSTest()

	logger := testLogger(t)

	db, err := getBaseStorage(logger)
	require.NoError(t, err)

	network := testBeaconConfig()

	km, err := NewLocalKeyManager(logger, db, network, operatorPrivateKey)
	require.NoError(t, err)

	sk1 := &bls.SecretKey{}
	require.NoError(t, sk1.SetHexString(sk1Str))

	sk2 := &bls.SecretKey{}
	require.NoError(t, sk2.SetHexString(sk2Str))

	encryptedSK1, err := operatorPrivateKey.Public().Encrypt([]byte(sk1.SerializeToHexStr()))
	require.NoError(t, err)

	encryptedSK2, err := operatorPrivateKey.Public().Encrypt([]byte(sk2.SerializeToHexStr()))
	require.NoError(t, err)

	require.NoError(t, km.AddShare(t.Context(), nil, encryptedSK1, phase0.BLSPubKey(sk1.GetPublicKey().Serialize())))
	require.NoError(t, km.AddShare(t.Context(), nil, encryptedSK2, phase0.BLSPubKey(sk2.GetPublicKey().Serialize())))

	return km
}

func TestEncryptedKeyManager(t *testing.T) {
	// Generate key 1.
	privateKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	encryptionKey, err := privateKey.EKMEncryptionKey()
	require.NoError(t, err)

	// Create account with key 1.
	initBLSTest()

	sk := bls.SecretKey{}
	sk.SetByCSPRNG()

	index := 0
	logger := testLogger(t)
	db, err := getBaseStorage(logger)
	require.NoError(t, err)

	signerStorage := NewSignerStorage(db, testBeaconConfig().Name, logger)
	signerStorage.SetEncryptionKey(encryptionKey)

	defer func() {
		err := db.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()

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

	encryptionKey2, err := privateKey2.EKMEncryptionKey()
	require.NoError(t, err)

	// Load account with key 2 (should fail).
	wallet2, err := signerStorage.OpenWallet()
	require.NoError(t, err)

	signerStorage.SetEncryptionKey(encryptionKey2)

	_, err = wallet2.AccountByPublicKey(hex.EncodeToString(a.ValidatorPublicKey()))
	require.ErrorContains(t, err, "decrypt stored wallet")

	// Retry with key 1 (should succeed).
	wallet3, err := signerStorage.OpenWallet()
	require.NoError(t, err)

	signerStorage.SetEncryptionKey(encryptionKey)

	_, err = wallet3.AccountByPublicKey(hex.EncodeToString(a.ValidatorPublicKey()))
	require.NoError(t, err)
}

func TestSignBeaconObject(t *testing.T) {
	ctx := t.Context()

	operatorPrivateKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	km := testKeyManager(t, operatorPrivateKey)

	sk1 := &bls.SecretKey{}
	require.NoError(t, sk1.SetHexString(sk1Str))

	encryptedSK1, err := operatorPrivateKey.Public().Encrypt([]byte(sk1.SerializeToHexStr()))
	require.NoError(t, err)

	require.NoError(t, km.AddShare(t.Context(), nil, encryptedSK1, phase0.BLSPubKey(sk1.GetPublicKey().Serialize())))

	currentSlot := testBeaconConfig().EstimatedCurrentSlot()
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
		aggregateAndProof := testingutils.TestingPhase0AggregateAndProof(testingutils.TestingValidatorIndex)

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
	// The Gloas (ePBS) domains sign a generic SSZ root via signSSZRoot (no slashing protection); the obj
	// type is incidental — the point is each domain is handled, not falling through to "domain unknown".
	for _, tc := range []struct {
		name   string
		domain phase0.DomainType
	}{
		{"DomainBeaconBuilder", spectypes.DomainBeaconBuilder},
		{"DomainPTCAttester", spectypes.DomainPTCAttester},
		{"DomainProposerPreferences", spectypes.DomainProposerPreferences},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, sig, err := km.(*LocalKeyManager).SignBeaconObject(
				ctx,
				spectypes.SSZUint64(1),
				phase0.Domain{},
				phase0.BLSPubKey(sk1.GetPublicKey().Serialize()),
				currentSlot,
				tc.domain,
			)
			require.NoError(t, err)
			require.NotNil(t, sig)
			require.NotEqual(t, [32]byte{}, sig)
		})
	}
}

// gloasBlockStub stands in for *gloas.BeaconBlock in these tests. The ssvsigner module can't import the
// node-side gloas package, so we exercise signBeaconObject's slashableBeaconBlock path with a type that,
// like the real block, is HTR-able (via the embedded header) and exposes its own slot through BlockSlot.
type gloasBlockStub struct {
	*phase0.BeaconBlockHeader
}

func (b gloasBlockStub) BlockSlot() phase0.Slot { return b.BeaconBlockHeader.Slot }

// newLocalKeyManagerWithShare returns a LocalKeyManager with sk1 added as a share, plus its public key.
func newLocalKeyManagerWithShare(t *testing.T) (*LocalKeyManager, phase0.BLSPubKey) {
	operatorPrivateKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)
	km := testKeyManager(t, operatorPrivateKey)

	sk1 := &bls.SecretKey{}
	require.NoError(t, sk1.SetHexString(sk1Str))
	encryptedSK1, err := operatorPrivateKey.Public().Encrypt([]byte(sk1.SerializeToHexStr()))
	require.NoError(t, err)
	pk := phase0.BLSPubKey(sk1.GetPublicKey().Serialize())
	require.NoError(t, km.AddShare(t.Context(), nil, encryptedSK1, pk))

	return km.(*LocalKeyManager), pk
}

func TestSignBeaconObjectGloasBlockSlashingProtection(t *testing.T) {
	ctx := t.Context()
	lkm, pk := newLocalKeyManagerWithShare(t)

	// A Gloas block reaches signBeaconObject's default case via the slashableBeaconBlock interface. That
	// path must key slashing protection to the block's OWN slot, not the plumbed duty slot, so pass a
	// different plumbed slot and assert the recorded highest proposal is the block's.
	blockSlot := testBeaconConfig().EstimatedCurrentSlot() + minSPProposalSlotGap + 10
	block := gloasBlockStub{&phase0.BeaconBlockHeader{Slot: blockSlot}}
	plumbedSlot := blockSlot - 5 // deliberately different; the Gloas arm must ignore it

	// First proposal signs and records the highest proposal keyed to the block's own slot.
	_, root, err := lkm.SignBeaconObject(ctx, block, phase0.Domain{}, pk, plumbedSlot, spectypes.DomainProposer)
	require.NoError(t, err)
	require.NotEqual(t, phase0.Root{}, root)

	highest, found, err := lkm.RetrieveHighestProposal(pk)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, blockSlot, highest, "slashing protection must record the block's own slot, not the plumbed slot")

	// Re-proposing the same block slot is slashable → rejected (proves the highest-proposal record + the
	// IsBeaconBlockSlashable guard that the direct signSSZRoot path used to skip).
	_, _, err = lkm.SignBeaconObject(ctx, block, phase0.Domain{}, pk, plumbedSlot, spectypes.DomainProposer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "slashable")
}

func TestSignBeaconObjectGloasRejectsUnknownProposerObject(t *testing.T) {
	ctx := t.Context()
	lkm, pk := newLocalKeyManagerWithShare(t)

	// A proposer-domain object that is neither a known go-eth2-client block nor a slashableBeaconBlock is a
	// routing bug: the signer must fail loud rather than sign an unrecognized object without slashing
	// protection. A bare header (no BlockSlot method) does not satisfy the interface.
	slot := testBeaconConfig().EstimatedCurrentSlot() + minSPProposalSlotGap + 10
	_, _, err := lkm.SignBeaconObject(ctx, &phase0.BeaconBlockHeader{Slot: slot}, phase0.Domain{}, pk, slot, spectypes.DomainProposer)
	require.ErrorContains(t, err, "unexpected object type")
}

func TestSignBeaconObjectGloasRejectsFarFutureSlot(t *testing.T) {
	ctx := t.Context()
	lkm, pk := newLocalKeyManagerWithShare(t)

	// Keying slashing protection to the block's own slot means the Gloas arm must guard the far-future
	// bound itself (the plumbed slot is no longer the bound). An absurd block slot is rejected before signing.
	block := gloasBlockStub{&phase0.BeaconBlockHeader{Slot: math.MaxUint64 / 2}}
	_, _, err := lkm.SignBeaconObject(ctx, block, phase0.Domain{}, pk, testBeaconConfig().EstimatedCurrentSlot(), spectypes.DomainProposer)
	require.ErrorContains(t, err, "too far into the future")
}

func TestRemoveShare(t *testing.T) {
	require.NoError(t, bls.Init(bls.BLS12_381))

	operatorPrivateKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	t.Run("key exists", func(t *testing.T) {
		km := testKeyManager(t, operatorPrivateKey)
		pk := &bls.SecretKey{}
		// generate random key
		pk.SetByCSPRNG()

		encryptedPrivKey, err := operatorPrivateKey.Public().Encrypt([]byte(pk.SerializeToHexStr()))
		require.NoError(t, err)

		require.NoError(t, km.AddShare(t.Context(), nil, encryptedPrivKey, phase0.BLSPubKey(pk.GetPublicKey().Serialize())))
		require.NoError(t, km.RemoveShare(t.Context(), nil, phase0.BLSPubKey(pk.GetPublicKey().Serialize())))
	})

	t.Run("key doesn't exist", func(t *testing.T) {
		km := testKeyManager(t, operatorPrivateKey)

		pk := &bls.SecretKey{}
		pk.SetByCSPRNG()

		err := km.RemoveShare(t.Context(), nil, phase0.BLSPubKey(pk.GetPublicKey().Serialize()))
		require.NoError(t, err)
	})
}

func TestEkmListAccounts(t *testing.T) {
	require.NoError(t, bls.Init(bls.BLS12_381))

	operatorPrivateKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	km := testKeyManager(t, operatorPrivateKey)
	accounts, err := km.(*LocalKeyManager).slashingProtector.ListAccounts()
	require.NoError(t, err)
	require.Len(t, accounts, 2)
}
