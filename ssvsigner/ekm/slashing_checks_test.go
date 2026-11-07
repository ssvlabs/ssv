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
	slashingprotection "github.com/ssvlabs/eth2-key-manager/slashing_protection"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"

	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

// raceWindow is how long a test waits for a goroutine that a lock may be holding back. With the lock
// in place the test always waits this long; without it, the goroutine gets through well within it.
// Too short a window can only make a test miss a missing lock, never fail a correct implementation.
const raceWindow = 250 * time.Millisecond

// hangTimeout bounds a wait that only a broken implementation can make endless, so that the test
// fails instead of hanging until go test's -timeout.
const hangTimeout = 5 * time.Second

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

		// So must the attestation's root, as the 32-byte root the sync-committee arm takes.
		att2Root, err := att2.HashTreeRoot()
		require.NoError(t, err)
		_, _, err = lkm.SignBeaconObject(ctx, spectypes.SSZBytes(att2Root[:]), attesterDomain, pk, slot, spectypes.DomainSyncCommittee)
		require.ErrorContains(t, err, "does not match signature domain")
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

		b2Root, err := b2.HashTreeRoot()
		require.NoError(t, err)
		_, _, err = lkm.SignBeaconObject(ctx, spectypes.SSZBytes(b2Root[:]), proposerDomain, pk, blockSlot, spectypes.DomainSyncCommittee)
		require.ErrorContains(t, err, "does not match signature domain")
	})

	t.Run("matching pairs still sign", func(t *testing.T) {
		agg := testingutils.TestingPhase0AggregateAndProof(testingutils.TestingValidatorIndex)
		_, _, err := lkm.SignBeaconObject(ctx, agg, domainFor(spectypes.DomainAggregateAndProof), pk, slot, spectypes.DomainAggregateAndProof)
		require.NoError(t, err)
	})
}

func TestRemoteKeyManager_RefusesMismatchedDomainType(t *testing.T) {
	cfg := testBeaconConfig()
	client := &MockRemoteSigner{}
	// Only so that a missing check fails on the assertions below instead of panicking in the mock.
	client.On("Sign", mock.Anything, mock.Anything, mock.Anything).Return(phase0.BLSSignature{}, nil).Maybe()
	protector := &MockSlashingProtector{}
	rm := &RemoteKeyManager{
		logger:            testLogger(t),
		beaconConfig:      cfg,
		genesisRoot:       cfg.GenesisValidatorsRoot,
		signerClient:      client,
		getOperatorId:     func() spectypes.OperatorID { return 1 },
		operatorPubKey:    &MockOperatorPublicKey{},
		slashingProtector: protector,
		signLocks:         map[signKey]*sync.RWMutex{},
	}

	pk := phase0.BLSPubKey{1, 2, 3}
	slot := cfg.EstimatedCurrentSlot()
	epoch := cfg.EstimatedEpochAtSlot(slot)
	att := &phase0.AttestationData{
		Slot: slot, BeaconBlockRoot: phase0.Root{0xA1},
		Source: &phase0.Checkpoint{Epoch: epoch}, Target: &phase0.Checkpoint{Epoch: epoch + 1},
	}
	attRoot, err := att.HashTreeRoot()
	require.NoError(t, err)
	attesterDomain := domainFor(spectypes.DomainAttester)

	for label, obj := range map[phase0.DomainType]spectypes.HashRoot{
		spectypes.DomainAggregateAndProof: att,
		spectypes.DomainSyncCommittee:     spectypes.SSZBytes(attRoot[:]),
	} {
		_, _, err := rm.SignBeaconObject(t.Context(), obj, attesterDomain, pk, slot, label)
		require.ErrorContains(t, err, "does not match signature domain", "label %x", label)
	}

	// Refused before any slashing check, record update or remote call.
	client.AssertNotCalled(t, "Sign", mock.Anything, mock.Anything, mock.Anything)
	protector.AssertNotCalled(t, "IsAttestationSlashable", mock.Anything, mock.Anything)
	protector.AssertNotCalled(t, "UpdateHighestAttestation", mock.Anything, mock.Anything)
}

func TestSignBeaconObject_GloasAndPreGloasBlocksShareOneProposalLock(t *testing.T) {
	ctx := t.Context()
	km, store, pk := newGatedLocalKeyManager(t)

	blockSlot := testBeaconConfig().EstimatedCurrentSlot() + 1
	deneb := *testingutils.TestingBlockContentsDeneb.Block
	deneb.Slot = blockSlot
	gloas := gloasTestBlock(blockSlot)
	domain := domainFor(spectypes.DomainProposer)

	// Hold each signer after its first read of the highest proposal until the other one has read it
	// too, unless a common lock keeps the other one out until the first has recorded its block.
	r := newRendezvous(2)
	store.arm(func(proposal bool) {
		if proposal {
			r.wait()
		}
	})

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, block := range []spectypes.HashRoot{gloas, &deneb} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, errs[i] = km.SignBeaconObject(ctx, block, domain, pk, blockSlot, spectypes.DomainProposer)
		}()
	}
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

func TestBumpSlashingProtection_ExcludesSigning(t *testing.T) {
	newKeyManagers := map[string]func(*testing.T) (KeyManager, *gatedStorage, phase0.BLSPubKey){
		"local": func(t *testing.T) (KeyManager, *gatedStorage, phase0.BLSPubKey) {
			return newGatedLocalKeyManager(t)
		},
		"remote": func(t *testing.T) (KeyManager, *gatedStorage, phase0.BLSPubKey) {
			return newGatedRemoteKeyManager(t)
		},
	}
	for name, newKeyManager := range newKeyManagers {
		for _, record := range []string{"attestation", "proposal"} {
			t.Run(name+"/"+record, func(t *testing.T) {
				km, store, pk := newKeyManager(t)
				requireBumpExcludesSigning(t, km, store, pk, record == "proposal")
			})
		}
	}
}

// requireBumpExcludesSigning parks km's bump right after it reads a stale record, signs in that
// window, and releases the bump. Then the record must hold what was signed, and a second, different
// object for the same target epoch or slot must be refused.
func requireBumpExcludesSigning(t *testing.T, km KeyManager, store *gatedStorage, pk phase0.BLSPubKey, proposal bool) {
	t.Helper()
	ctx := t.Context()
	cfg := testBeaconConfig()
	cur := cfg.EstimatedCurrentSlot()
	epoch := cfg.EstimatedEpochAtSlot(cur)

	// The signed objects sit a few slots / epochs above the current ones, so that a slot or epoch
	// boundary passing mid-test can't lift the bump's write up to them.
	blockSlot := cur + 3
	target := epoch + 2

	var sign func(root byte) error
	if proposal {
		require.NoError(t, store.SaveHighestProposal(pk[:], cur-5))
		sign = func(root byte) error {
			block := *testingutils.TestingBlockContentsElectra.Block // shallow copy; Body is shared and untouched
			block.Slot = blockSlot
			block.ParentRoot = phase0.Root{root}
			_, _, err := km.SignBeaconObject(ctx, &block, domainFor(spectypes.DomainProposer), pk, blockSlot, spectypes.DomainProposer)
			return err
		}
	} else {
		require.NoError(t, store.SaveHighestAttestation(pk[:], &phase0.AttestationData{
			Source: &phase0.Checkpoint{Epoch: epoch - 6},
			Target: &phase0.Checkpoint{Epoch: epoch - 5},
		}))
		sign = func(root byte) error {
			att := &phase0.AttestationData{
				Slot:            cur,
				BeaconBlockRoot: phase0.Root{root},
				Source:          &phase0.Checkpoint{Epoch: target - 1},
				Target:          &phase0.Checkpoint{Epoch: target},
			}
			_, _, err := km.SignBeaconObject(ctx, att, domainFor(spectypes.DomainAttester), pk, cur, spectypes.DomainAttester)
			return err
		}
	}

	// Park the bump between its read of the stale record and its write.
	p := newParker()
	t.Cleanup(p.release)
	store.arm(func(isProposal bool) {
		if isProposal == proposal {
			p.wait()
		}
	})
	bumpErr := make(chan error, 1)
	go func() { bumpErr <- km.BumpSlashingProtection(pk) }()
	select {
	case <-p.parked:
	case err := <-bumpErr:
		require.FailNow(t, "the bump returned without reading the record through the gated store", "bump error: %v", err)
	case <-time.After(hangTimeout):
		require.FailNow(t, "the bump didn't read the record through the gated store within "+hangTimeout.String())
	}

	// Sign while the bump is parked. With the bump's lock in place, the signing waits for the bump.
	var signErr error
	signed := make(chan struct{})
	go func() {
		defer close(signed)
		signErr = sign(0x01)
	}()
	select {
	case <-signed:
	case <-time.After(raceWindow):
	}
	p.release()

	require.NoError(t, recvWithin(t, bumpErr, "the bump"))
	recvWithin(t, signed, "the signing")
	require.NoError(t, signErr, "the first signature must go through")
	store.arm(nil)

	if proposal {
		highest, found, err := store.Storage.RetrieveHighestProposal(pk[:])
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, blockSlot, highest, "record must stay at the signed slot")
	} else {
		highest, found, err := store.Storage.RetrieveHighestAttestation(pk[:])
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, target, highest.Target.Epoch, "record must stay at the signed target")
	}
	require.ErrorContains(t, sign(0x02), "slashable", "a second signature for the same target must be refused")
}

// newGatedLocalKeyManager returns a LocalKeyManager over a gatedStorage, with sk1 added as a share.
func newGatedLocalKeyManager(t *testing.T) (*LocalKeyManager, *gatedStorage, phase0.BLSPubKey) {
	t.Helper()
	initBLSTest()
	logger := testLogger(t)
	cfg := testBeaconConfig()

	opKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)
	encKey, err := opKey.EKMEncryptionKey()
	require.NoError(t, err)

	signerStore := NewSignerStorage(newTestMemoryDB(), cfg.NetworkName(), logger)
	signerStore.SetEncryptionKey(encKey)
	store := &gatedStorage{Storage: signerStore}

	km, err := newLocalKeyManager(logger, store, cfg, opKey)
	require.NoError(t, err)

	sk := &bls.SecretKey{}
	require.NoError(t, sk.SetHexString(sk1Str))
	enc, err := opKey.Public().Encrypt([]byte(sk.SerializeToHexStr()))
	require.NoError(t, err)
	pk := phase0.BLSPubKey(sk.GetPublicKey().Serialize())
	require.NoError(t, km.AddShare(t.Context(), enc, pk))

	return km, store, pk
}

// newGatedRemoteKeyManager returns a RemoteKeyManager whose slashing protection runs over a
// gatedStorage and whose remote signer accepts every share and signs everything, with one share added.
func newGatedRemoteKeyManager(t *testing.T) (*RemoteKeyManager, *gatedStorage, phase0.BLSPubKey) {
	t.Helper()
	logger := testLogger(t)
	cfg := testBeaconConfig()
	store := &gatedStorage{Storage: NewSignerStorage(newTestMemoryDB(), cfg.NetworkName(), logger)}

	client := &MockRemoteSigner{}
	client.On("AddValidators", mock.Anything, mock.Anything).Return([]web3signer.Status{web3signer.StatusImported}, nil)
	client.On("Sign", mock.Anything, mock.Anything, mock.Anything).Return(phase0.BLSSignature{1}, nil)

	km := &RemoteKeyManager{
		logger:            logger,
		beaconConfig:      cfg,
		genesisRoot:       cfg.GenesisValidatorsRoot,
		signerClient:      client,
		getOperatorId:     func() spectypes.OperatorID { return 1 },
		operatorPubKey:    &MockOperatorPublicKey{},
		slashingProtector: NewSlashingProtector(logger, cfg, store, slashingprotection.NewNormalProtection(store)),
		signLocks:         map[signKey]*sync.RWMutex{},
	}

	pk := phase0.BLSPubKey{1, 2, 3}
	require.NoError(t, km.AddShare(t.Context(), []byte("encrypted share"), pk))

	return km, store, pk
}

// gatedStorage wraps a Storage and, once armed, calls afterRead right after each highest-attestation
// or highest-proposal read: the point between a read-compare-write's read and its write.
type gatedStorage struct {
	Storage

	mu        sync.Mutex
	afterRead func(proposal bool)
}

func (s *gatedStorage) arm(afterRead func(proposal bool)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.afterRead = afterRead
}

func (s *gatedStorage) read(proposal bool) {
	s.mu.Lock()
	afterRead := s.afterRead
	s.mu.Unlock()
	if afterRead != nil {
		afterRead(proposal)
	}
}

func (s *gatedStorage) RetrieveHighestAttestation(pubKey []byte) (*phase0.AttestationData, bool, error) {
	att, found, err := s.Storage.RetrieveHighestAttestation(pubKey)
	s.read(false)
	return att, found, err
}

func (s *gatedStorage) RetrieveHighestProposal(pubKey []byte) (phase0.Slot, bool, error) {
	slot, found, err := s.Storage.RetrieveHighestProposal(pubKey)
	s.read(true)
	return slot, found, err
}

// rendezvous holds each of the first n callers of wait until all n have arrived, or until raceWindow
// passes, so n concurrent signers all read the same record before any of them can update it. When a
// lock serializes the signers, the later ones can't arrive while the first one waits.
type rendezvous struct {
	mu      sync.Mutex
	n       int
	arrived int
	all     chan struct{}
}

func newRendezvous(n int) *rendezvous {
	return &rendezvous{n: n, all: make(chan struct{})}
}

func (r *rendezvous) wait() {
	r.mu.Lock()
	r.arrived++
	arrived := r.arrived
	if arrived == r.n {
		close(r.all)
	}
	r.mu.Unlock()
	if arrived > r.n {
		return
	}
	select {
	case <-r.all:
	case <-time.After(raceWindow):
	}
}

// parker holds the first caller of wait until release, and lets every later caller through.
type parker struct {
	first       sync.Once
	parked      chan struct{}
	releaseOnce sync.Once
	released    chan struct{}
}

func newParker() *parker {
	return &parker{parked: make(chan struct{}), released: make(chan struct{})}
}

func (p *parker) wait() {
	first := false
	p.first.Do(func() { first = true })
	if !first {
		return
	}
	close(p.parked)
	<-p.released
}

func (p *parker) release() {
	p.releaseOnce.Do(func() { close(p.released) })
}

// recvWithin receives from ch, failing the test if nothing arrives within hangTimeout.
func recvWithin[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(hangTimeout):
		require.FailNow(t, what+" didn't finish within "+hangTimeout.String())
	}
	var zero T
	return zero
}
