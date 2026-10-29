package ekm

// Covers slashing-protection properties that depend on the caller's arguments or on locking.
//
//  1. TestSignBeaconObject_RefusesMismatchedDomainType, TestRemoteKeyManager_RefusesMismatchedDomainType:
//     a signatureDomain whose type differs from the domain's is refused (see checkDomainType),
//     before any slashing check, record update or signing.
//
//  2. TestSignBeaconObject_GloasAndPreGloasBlocksShareOneProposalLock: LocalKeyManager runs
//     check→record→sign for every block type under one lock, so a Gloas and a pre-Gloas block for
//     the same share and slot can't both pass the check.
//
//  3. TestBumpSlashingProtection_ExcludesSigning: the bump's read-compare-write, through
//     BumpSlashingProtection or AddShare, holds the lock that signing holds for its check→record,
//     so a record signed concurrently with a bump is kept.

import (
	"sync"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	eth2keymanager "github.com/ssvlabs/eth2-key-manager"
	"github.com/ssvlabs/eth2-key-manager/core"
	"github.com/ssvlabs/eth2-key-manager/signer"
	slashingprotection "github.com/ssvlabs/eth2-key-manager/slashing_protection"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"

	"github.com/ssvlabs/ssv/ssvsigner/keys"
)

func TestSignBeaconObject_RefusesMismatchedDomainType(t *testing.T) {
	ctx := t.Context()
	lkm, pk := newLocalKeyManagerWithShare(t)
	cfg := testBeaconConfig()
	slot := cfg.EstimatedCurrentSlot()
	epoch := cfg.EstimatedEpochAtSlot(slot)

	unprotected := []phase0.DomainType{
		spectypes.DomainAggregateAndProof,
		spectypes.DomainPTCAttester,
		spectypes.DomainProposerPreferences,
		spectypes.DomainBeaconBuilder,
		spectypes.DomainBuilderRequestAuth,
	}

	t.Run("attestation under the attester domain", func(t *testing.T) {
		attesterDomain := domainFor(spectypes.DomainAttester)
		att1 := &phase0.AttestationData{
			Slot: slot, BeaconBlockRoot: phase0.Root{0xA1},
			Source: &phase0.Checkpoint{Epoch: epoch}, Target: &phase0.Checkpoint{Epoch: epoch + 1},
		}
		att2 := *att1
		att2.BeaconBlockRoot = phase0.Root{0xA2}

		// Protected path: the first vote signs, the conflicting second vote is refused.
		_, _, err := lkm.SignBeaconObject(ctx, att1, attesterDomain, pk, slot, spectypes.DomainAttester)
		require.NoError(t, err)
		_, _, err = lkm.SignBeaconObject(ctx, &att2, attesterDomain, pk, slot, spectypes.DomainAttester)
		require.ErrorContains(t, err, "slashable attestation")

		// The same object and domain under another label must be refused for the mismatch itself.
		for _, label := range unprotected {
			_, _, err := lkm.SignBeaconObject(ctx, &att2, attesterDomain, pk, slot, label)
			require.ErrorContains(t, err, "does not match signature domain", "label %x", label)
		}
	})

	t.Run("block under the proposer domain", func(t *testing.T) {
		proposerDomain := domainFor(spectypes.DomainProposer)
		blockSlot := slot + 1
		b1 := *testingutils.TestingBlockContentsDeneb.Block
		b1.Slot = blockSlot
		b1.ParentRoot = phase0.Root{0xB1}
		b2 := b1
		b2.ParentRoot = phase0.Root{0xB2}

		_, _, err := lkm.SignBeaconObject(ctx, &b1, proposerDomain, pk, blockSlot, spectypes.DomainProposer)
		require.NoError(t, err)
		_, _, err = lkm.SignBeaconObject(ctx, &b2, proposerDomain, pk, blockSlot, spectypes.DomainProposer)
		require.ErrorContains(t, err, "slashable proposal")

		for _, label := range unprotected {
			_, _, err := lkm.SignBeaconObject(ctx, &b2, proposerDomain, pk, blockSlot, label)
			require.ErrorContains(t, err, "does not match signature domain", "label %x", label)
		}
	})

	t.Run("matching pairs still sign", func(t *testing.T) {
		agg := testingutils.TestingPhase0AggregateAndProof(testingutils.TestingValidatorIndex)
		_, _, err := lkm.SignBeaconObject(ctx, agg, domainFor(spectypes.DomainAggregateAndProof), pk, slot, spectypes.DomainAggregateAndProof)
		require.NoError(t, err)
	})
}

// gate makes the first `want` callers of RetrieveHighestProposal rendezvous after their read, so two
// concurrent signers hold the same (stale) highest proposal before either can update it. If a lock
// serializes the signers, the second caller never arrives and the first proceeds after a timeout.
type gate struct {
	mu    sync.Mutex
	n     int
	want  int
	ready chan struct{}
}

func (g *gate) wait() {
	g.mu.Lock()
	g.n++
	n := g.n
	if n == g.want {
		close(g.ready)
	}
	g.mu.Unlock()
	if n <= g.want {
		select {
		case <-g.ready:
		case <-time.After(2 * time.Second):
		}
	}
}

type gatedStorage struct {
	Storage
	g *gate
}

func (s *gatedStorage) RetrieveHighestProposal(pubKey []byte) (phase0.Slot, bool, error) {
	slot, found, err := s.Storage.RetrieveHighestProposal(pubKey)
	s.g.wait()
	return slot, found, err
}

func TestSignBeaconObject_GloasAndPreGloasBlocksShareOneProposalLock(t *testing.T) {
	initBLSTest()
	ctx := t.Context()
	logger := testLogger(t)
	cfg := testBeaconConfig()

	opKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)
	encKey, err := opKey.EKMEncryptionKey()
	require.NoError(t, err)

	// Replicate NewLocalKeyManager, but route both slashing-protection consumers (the lib signer and the
	// SlashingProtector) through a gated store.
	db := newTestMemoryDB()
	store := NewSignerStorage(db, cfg.Name, logger)
	store.SetEncryptionKey(encKey)
	gated := &gatedStorage{Storage: store, g: &gate{want: 2, ready: make(chan struct{})}}
	protection := slashingprotection.NewNormalProtection(gated)

	options := &eth2keymanager.KeyVaultOptions{}
	options.SetStorage(store)
	options.SetWalletType(core.NDWallet)
	vault, err := eth2keymanager.NewKeyVault(options)
	require.NoError(t, err)
	wallet, err := vault.Wallet()
	require.NoError(t, err)

	km := &LocalKeyManager{
		logger:            logger,
		wallet:            wallet,
		walletLock:        &sync.RWMutex{},
		signer:            signer.NewSimpleSigner(wallet, protection, cfg),
		slashingProtector: NewSlashingProtector(logger, cfg, gated, protection),
		operatorDecrypter: opKey,
		beaconConfig:      cfg,
	}

	sk := &bls.SecretKey{}
	require.NoError(t, sk.SetHexString(sk1Str))
	enc, err := opKey.Public().Encrypt([]byte(sk.SerializeToHexStr()))
	require.NoError(t, err)
	pk := phase0.BLSPubKey(sk.GetPublicKey().Serialize())
	require.NoError(t, km.AddShare(ctx, nil, enc, pk))

	blockSlot := cfg.EstimatedCurrentSlot() + 1
	deneb := *testingutils.TestingBlockContentsDeneb.Block
	deneb.Slot = blockSlot
	gloas := gloasTestBlock(blockSlot)
	domain := domainFor(spectypes.DomainProposer)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _, errs[0] = km.SignBeaconObject(ctx, gloas, domain, pk, blockSlot, spectypes.DomainProposer)
	}()
	go func() {
		defer wg.Done()
		_, _, errs[1] = km.SignBeaconObject(ctx, &deneb, domain, pk, blockSlot, spectypes.DomainProposer)
	}()
	wg.Wait()

	signed := 0
	for _, e := range errs {
		if e == nil {
			signed++
		}
	}
	t.Logf("gloas err=%v, deneb err=%v", errs[0], errs[1])
	require.Equal(t, 1, signed, "exactly one of two different blocks for slot %d may be signed", blockSlot)
}
