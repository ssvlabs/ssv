package ekmadapter_test

// Guards the slashing records end to end, with the real LocalKeyManager over real badger:
// a share's highest-attestation / highest-proposal record never goes backwards, whatever the event
// handler does to the share (remove, re-add, bump) while the validator keeps signing.
//
// The event handler runs one badger transaction per EL block, which can remove and re-add a share
// while the validator is still signing. RemoveShare keeps the records, and the bump writes them
// immediately, under the signing lock, and only ever raises them.
//
// Where the bump's minimal record already covers what was signed, the bump alone refuses the double
// signature, whatever the stored record was. So the *_ClockSteppedBack tests and the bump test
// step the wall clock back between the signature and the re-add or bump: the signed record then sits
// above the bump's, and only a kept record refuses the double signature.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ekmadapter"
	"github.com/ssvlabs/ssv/protocol/v2/types/ssvtestingutils"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	badgerdb "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

const slotsPerEpoch = 32

func slotOf(e phase0.Epoch) phase0.Slot { return phase0.Slot(e) * slotsPerEpoch }

// fakeBeacon is an ekm.BeaconNetwork whose wall clock the test controls.
type fakeBeacon struct {
	mu   sync.Mutex
	slot phase0.Slot
}

func (f *fakeBeacon) set(slot phase0.Slot) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.slot = slot
}

func (f *fakeBeacon) NetworkName() string      { return "slashing-records-testnet" }
func (f *fakeBeacon) GenesisRoot() phase0.Root { return phase0.Root{} }
func (f *fakeBeacon) EstimatedCurrentSlot() phase0.Slot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.slot
}
func (f *fakeBeacon) EstimatedEpochAtSlot(slot phase0.Slot) phase0.Epoch {
	return phase0.Epoch(slot / slotsPerEpoch)
}
func (f *fakeBeacon) EstimatedSlotAtTime(t time.Time) phase0.Slot {
	// Far-future protection asks for the slot 20 minutes from now; model it as +100 slots. A time in
	// the past maps to the current slot rather than wrapping a negative duration.
	ahead := time.Until(t)
	if ahead < 0 {
		ahead = 0
	}
	return f.EstimatedCurrentSlot() + phase0.Slot(ahead/(12*time.Second))
}
func (f *fakeBeacon) ForkAtEpoch(phase0.Epoch) (spec.DataVersion, *phase0.Fork) {
	return spec.DataVersionDeneb, &phase0.Fork{}
}
func (f *fakeBeacon) ForkAtVersion(spec.DataVersion) (phase0.Fork, bool) { return phase0.Fork{}, true }

var initBLSOnce sync.Once

type fixture struct {
	km     *ekm.LocalKeyManager
	beacon *fakeBeacon
	pk     phase0.BLSPubKey
	enc    []byte
}

func newFixture(t *testing.T, startSlot phase0.Slot) *fixture {
	t.Helper()
	initBLSOnce.Do(func() {
		require.NoError(t, bls.Init(bls.BLS12_381))
		require.NoError(t, bls.SetETHmode(bls.EthModeDraft07))
	})

	db, err := badgerdb.NewInMemory(zap.NewNop(), basedb.Options{Ctx: t.Context()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	beacon := &fakeBeacon{slot: startSlot}

	opKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	km, err := ekm.NewLocalKeyManager(zap.NewNop(), ekmadapter.NewDatabaseAdapter(db), beacon, opKey)
	require.NoError(t, err)

	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()
	enc, err := opKey.Public().Encrypt([]byte(sk.SerializeToHexStr()))
	require.NoError(t, err)
	pk := phase0.BLSPubKey(sk.GetPublicKey().Serialize())

	// Initial registration, in an earlier block.
	require.NoError(t, km.AddShare(t.Context(), enc, pk))

	return &fixture{km: km, beacon: beacon, pk: pk, enc: enc}
}

func attData(slot phase0.Slot, source, target phase0.Epoch, root byte) *phase0.AttestationData {
	return &phase0.AttestationData{
		Slot:            slot,
		Index:           0,
		BeaconBlockRoot: phase0.Root{root},
		Source:          &phase0.Checkpoint{Epoch: source},
		Target:          &phase0.Checkpoint{Epoch: target},
	}
}

// domainFor returns a signing domain whose type prefix matches dt, as a beacon node derives it.
func domainFor(dt phase0.DomainType) phase0.Domain {
	var d phase0.Domain
	copy(d[:4], dt[:])
	return d
}

func (f *fixture) signAttestation(ctx context.Context, data *phase0.AttestationData) error {
	_, _, err := f.km.SignBeaconObject(ctx, data, domainFor(spectypes.DomainAttester), f.pk, data.Slot, spectypes.DomainAttester)
	return err
}

func (f *fixture) signBlock(ctx context.Context, slot phase0.Slot, parent byte) error {
	block := *ssvtestingutils.TestingBlockContentsDeneb.Block // shallow copy; Body is shared and untouched
	block.Slot = slot
	block.ParentRoot = phase0.Root{parent}
	_, _, err := f.km.SignBeaconObject(ctx, &block, domainFor(spectypes.DomainProposer), f.pk, slot, spectypes.DomainProposer)
	return err
}

// TestRemoveAndReAdd_KeepsHigherAttestationRecord: a block carrying ValidatorRemoved +
// ValidatorAdded for the same share, processed across an epoch boundary while the validator
// attests, leaves the record at the signed target and refuses the double vote.
func TestRemoveAndReAdd_KeepsHigherAttestationRecord(t *testing.T) {
	ctx := t.Context()
	const E = phase0.Epoch(1000)

	f := newFixture(t, slotOf(E-2)) // registered at epoch E-2 → minimal record (E-3, E-2)

	// Validator attests normally in epoch E-1.
	f.beacon.set(slotOf(E - 1))
	require.NoError(t, f.signAttestation(ctx, attData(slotOf(E-1), E-2, E-1, 0xA1)))

	// Epoch E: an EL block carries ValidatorRemoved + ValidatorAdded for this share (same keyshares
	// re-registered).
	f.beacon.set(slotOf(E))
	require.NoError(t, f.km.RemoveShare(ctx, f.pk))
	require.NoError(t, f.km.AddShare(ctx, f.enc, f.pk)) // bump → (E-1, E)

	// The rest of the block takes a while to process (e.g. many slow remote-signer imports); the
	// validator is still running and attests in epoch E+1.
	f.beacon.set(slotOf(E + 1))
	require.NoError(t, f.signAttestation(ctx, attData(slotOf(E+1), E, E+1, 0xB1)),
		"first attestation for target E+1 must sign")

	highest, found, err := f.km.RetrieveHighestAttestation(f.pk)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, E+1, highest.Target.Epoch, "record must stay at the signed target")

	// Second attestation for the SAME target epoch with a different root: a slashable double vote.
	require.ErrorContains(t, f.signAttestation(ctx, attData(slotOf(E+1), E, E+1, 0xB2)), "slashable", "double vote must be refused")
}

// TestRemoveAndReAdd_KeepsHigherProposalRecord: same shape on the proposal side, across a single
// slot boundary.
func TestRemoveAndReAdd_KeepsHigherProposalRecord(t *testing.T) {
	ctx := t.Context()
	const S = phase0.Slot(32_000)

	f := newFixture(t, S-10) // registered at S-10 → minimal proposal record S-10

	// Validator proposes at S-1.
	f.beacon.set(S - 1)
	require.NoError(t, f.signBlock(ctx, S-1, 0x01))

	// Slot S: block with ValidatorRemoved + ValidatorAdded.
	f.beacon.set(S)
	require.NoError(t, f.km.RemoveShare(ctx, f.pk))
	require.NoError(t, f.km.AddShare(ctx, f.enc, f.pk)) // bump → S

	// Next slot arrives while the block is still being processed; validator proposes at S+1.
	f.beacon.set(S + 1)
	require.NoError(t, f.signBlock(ctx, S+1, 0x02), "first block for S+1 must sign")

	highest, found, err := f.km.RetrieveHighestProposal(f.pk)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, S+1, highest, "record must stay at the signed slot")

	// A second, different block for S+1: a slashable double proposal.
	require.ErrorContains(t, f.signBlock(ctx, S+1, 0x03), "slashable", "double proposal must be refused")
}

// TestRemoveAndReAdd_ClockSteppedBack_RefusesDoubleVote: the wall clock steps back an epoch after the
// validator attests, and then the share is removed and re-added. The re-add's bump alone would only
// cover the earlier epoch, so the double vote is refused only if RemoveShare kept the record.
func TestRemoveAndReAdd_ClockSteppedBack_RefusesDoubleVote(t *testing.T) {
	ctx := t.Context()
	const E = phase0.Epoch(1000)

	f := newFixture(t, slotOf(E-1))
	f.beacon.set(slotOf(E))
	require.NoError(t, f.signAttestation(ctx, attData(slotOf(E), E-1, E, 0xA1)))

	f.beacon.set(slotOf(E - 1))
	require.NoError(t, f.km.RemoveShare(ctx, f.pk))
	require.NoError(t, f.km.AddShare(ctx, f.enc, f.pk)) // bump's minimal record: (E-2, E-1)

	highest, found, err := f.km.RetrieveHighestAttestation(f.pk)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, E, highest.Target.Epoch, "record must stay at the signed target")
	require.ErrorContains(t, f.signAttestation(ctx, attData(slotOf(E), E-1, E, 0xA2)), "slashable", "double vote must be refused")
}

// TestRemoveAndReAdd_ClockSteppedBack_RefusesDoubleProposal: the proposal-side counterpart, with the
// wall clock stepping back a slot.
func TestRemoveAndReAdd_ClockSteppedBack_RefusesDoubleProposal(t *testing.T) {
	ctx := t.Context()
	const S = phase0.Slot(32_000)

	f := newFixture(t, S-1)
	f.beacon.set(S)
	require.NoError(t, f.signBlock(ctx, S, 0x01))

	f.beacon.set(S - 1)
	require.NoError(t, f.km.RemoveShare(ctx, f.pk))
	require.NoError(t, f.km.AddShare(ctx, f.enc, f.pk)) // bump's minimal record: S-1

	highest, found, err := f.km.RetrieveHighestProposal(f.pk)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, S, highest, "record must stay at the signed slot")
	require.ErrorContains(t, f.signBlock(ctx, S, 0x02), "slashable", "double proposal must be refused")
}

// TestBumpOnExistingRecord_KeepsHigherRecord covers the ClusterReactivated-style bump on existing
// records above the bump's minimal ones: the wall clock steps back an epoch after the validator
// attests and proposes. The bump must leave both records as they are.
func TestBumpOnExistingRecord_KeepsHigherRecord(t *testing.T) {
	ctx := t.Context()
	const E = phase0.Epoch(1000)

	f := newFixture(t, slotOf(E-1))
	f.beacon.set(slotOf(E))
	require.NoError(t, f.signAttestation(ctx, attData(slotOf(E), E-1, E, 0xA1)))
	require.NoError(t, f.signBlock(ctx, slotOf(E), 0x01))

	f.beacon.set(slotOf(E - 1))
	require.NoError(t, f.km.BumpSlashingProtection(f.pk)) // minimal records: (E-2, E-1) and slotOf(E-1)

	highestAtt, found, err := f.km.RetrieveHighestAttestation(f.pk)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, E, highestAtt.Target.Epoch, "attestation record must stay at the signed target")
	highestProp, found, err := f.km.RetrieveHighestProposal(f.pk)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, slotOf(E), highestProp, "proposal record must stay at the signed slot")

	require.ErrorContains(t, f.signAttestation(ctx, attData(slotOf(E), E-1, E, 0xA2)), "slashable", "double vote must be refused")
	require.ErrorContains(t, f.signBlock(ctx, slotOf(E), 0x02), "slashable", "double proposal must be refused")
}
