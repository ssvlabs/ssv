package ssv

import (
	"fmt"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

// TestVoteCheckerSourceTargetEpoch pins the behavior of the source/target epoch check at
// value_check.go:47.
//
// The current implementation uses `source.Epoch >= target.Epoch` to reject the BeaconVote,
// which is stricter than the Ethereum consensus-specs validity rule for attestations. Per
// `process_attestation` in https://github.com/ethereum/consensus-specs/blob/master/specs/phase0/beacon-chain.md
// the only attestation-data validity constraints are:
//   - data.target.epoch ∈ {get_previous_epoch(state), get_current_epoch(state)}
//   - data.source == state.{current,previous}_justified_checkpoint (per target.epoch)
//
// There is no `source.epoch < target.epoch` rule at validity time; that inequality is the
// Casper FFG surround-vote *slashing* rule (consensus-specs phase0/beacon-chain.md, the
// "Attester Slashing" section), and SSV's slashing protection covers it independently a few
// lines below.
//
// The over-strict `>=` rejects the genesis-epoch case where both source and target legitimately
// equal 0 (because state.{current,previous}_justified_checkpoint == Checkpoint(0, ⊥) at genesis).
// This causes every fresh-genesis cross-client test to fail on every committee duty throughout
// epoch 0 — observed in the v2.4.3 ↔ Anchor v1.2.3 interop logs.
//
// The corresponding ssv-spec rule with the same bug is at ssv-spec v1.2.2 ssv/value_check.go:53;
// filed upstream at ssvlabs/ssv-spec#631. SSV-Go's one-character mirror fix (`>=` → `>`) is held
// back until spec-maintainer direction is confirmed on #631, to avoid SSV drifting from the
// canonical spec unilaterally.
//
// When the spec fix lands, the genesis sub-test's assertion needs to flip: the source/target
// gate should no longer fire on `(0, 0)`. The source-greater-than-target sub-test is unaffected
// by the fix.
func TestVoteCheckerSourceTargetEpoch(t *testing.T) {
	t.Run("genesis: source.Epoch == target.Epoch == 0 currently rejected (B1 bug)", func(t *testing.T) {
		checker := NewVoteChecker(nil, 0, []phase0.BLSPubKey{}, &spectypes.BeaconVote{})
		err := checker.CheckValue(encodeBeaconVote(t, 0, 0))
		require.ErrorContains(t, err, "source >= target")
		// TODO(B1 fix): once the fix lands, this assertion should flip — at minimum
		// the source/target gate should no longer fire on (0, 0).
	})

	t.Run("source.Epoch > target.Epoch is rejected (unchanged by the B1 fix)", func(t *testing.T) {
		checker := NewVoteChecker(nil, 0, []phase0.BLSPubKey{}, &spectypes.BeaconVote{})
		err := checker.CheckValue(encodeBeaconVote(t, 1, 0))
		require.ErrorContains(t, err, "source >= target")
	})
}

// encodeBeaconVote returns SSZ bytes for a BeaconVote with the requested source/target epochs
// and zero block root. Suitable for tests that exercise the source/target gate, which fires
// before any downstream check (slashing or expected-vote) so a nil signer/expectedVote on the
// voteChecker is fine.
func encodeBeaconVote(t *testing.T, sourceEpoch, targetEpoch phase0.Epoch) []byte {
	t.Helper()
	bv := &spectypes.BeaconVote{
		BlockRoot: phase0.Root{},
		Source:    &phase0.Checkpoint{Epoch: sourceEpoch},
		Target:    &phase0.Checkpoint{Epoch: targetEpoch},
	}
	data, err := bv.Encode()
	require.NoError(t, err)
	return data
}

// TestValidateNoDuplicateAggregatorCommittee covers the duplicate-rejection that closes the
// per-index partial-signature cap gap: a validator index may repeat across the two sets (an
// aggregator and a contributor for the same numeric index are distinct), but not within a set.
func TestValidateNoDuplicateAggregatorCommittee(t *testing.T) {
	agg := func(vi phase0.ValidatorIndex, ci uint64) spectypes.AssignedAggregator {
		return spectypes.AssignedAggregator{ValidatorIndex: vi, CommitteeIndex: ci}
	}

	t.Run("clean data passes", func(t *testing.T) {
		cd := &spectypes.AggregatorCommitteeConsensusData{
			Aggregators:  []spectypes.AssignedAggregator{agg(1, 0), agg(2, 0), agg(1, 3)},
			Contributors: []spectypes.AssignedAggregator{agg(1, 0), agg(1, 1), agg(2, 0)},
		}
		require.NoError(t, validateNoDuplicateAggregatorCommittee(cd))
	})

	t.Run("same (validator, index) across the two sets is allowed", func(t *testing.T) {
		cd := &spectypes.AggregatorCommitteeConsensusData{
			Aggregators:  []spectypes.AssignedAggregator{agg(7, 0)},
			Contributors: []spectypes.AssignedAggregator{agg(7, 0)},
		}
		require.NoError(t, validateNoDuplicateAggregatorCommittee(cd))
	})

	t.Run("duplicate aggregator rejected", func(t *testing.T) {
		cd := &spectypes.AggregatorCommitteeConsensusData{
			Aggregators: []spectypes.AssignedAggregator{agg(5, 2), agg(5, 2)},
		}
		require.ErrorContains(t, validateNoDuplicateAggregatorCommittee(cd), "duplicate aggregator")
	})

	t.Run("duplicate contributor rejected", func(t *testing.T) {
		cd := &spectypes.AggregatorCommitteeConsensusData{
			Contributors: []spectypes.AssignedAggregator{agg(9, 1), agg(9, 1)},
		}
		require.ErrorContains(t, validateNoDuplicateAggregatorCommittee(cd), "duplicate contributor")
	})
}

// fakeSlashingSigner implements ekm.BeaconSigner for value-check tests. Only IsAttestationSlashable is
// exercised; the embedded nil interface panics if any other method is called, which surfaces an
// unexpected dependency rather than hiding it.
type fakeSlashingSigner struct {
	ekm.BeaconSigner
	slashable error
}

func (f fakeSlashingSigner) IsAttestationSlashable(phase0.BLSPubKey, *phase0.AttestationData) error {
	return f.slashable
}

func (f fakeSlashingSigner) IsBeaconBlockSlashable(phase0.BLSPubKey, phase0.Slot) error {
	return f.slashable
}

func gloasVote(source, target phase0.Epoch, index phase0.CommitteeIndex) *gloas.GloasBeaconVote {
	return &gloas.GloasBeaconVote{
		BlockRoot:            phase0.Root{0x01},
		Source:               &phase0.Checkpoint{Epoch: source},
		Target:               &phase0.Checkpoint{Epoch: target},
		AttestationDataIndex: index,
	}
}

func encodeGloasVote(t *testing.T, v *gloas.GloasBeaconVote) []byte {
	t.Helper()
	b, err := v.Encode()
	require.NoError(t, err)
	return b
}

func newGloasChecker(signer ekm.BeaconSigner, expected *gloas.GloasBeaconVote) ValueChecker {
	return NewGloasVoteChecker(signer, 64, []phase0.BLSPubKey{{}}, expected)
}

// Both payload-status indices (0 = EMPTY, 1 = FULL) pass when source < target, the epochs match the
// expected vote, and the attestation is not slashable.
func TestGloasVoteChecker_Valid(t *testing.T) {
	for _, index := range []phase0.CommitteeIndex{0, 1} {
		expected := gloasVote(1, 2, index)
		checker := newGloasChecker(fakeSlashingSigner{}, expected)
		require.NoError(t, checker.CheckValue(encodeGloasVote(t, gloasVote(1, 2, index))))
	}
}

// The one Gloas-specific rule: AttestationDataIndex outside {0, 1} is rejected.
func TestGloasVoteChecker_IndexOutOfRange(t *testing.T) {
	expected := gloasVote(1, 2, 0)
	checker := newGloasChecker(fakeSlashingSigner{}, expected)
	require.Error(t, checker.CheckValue(encodeGloasVote(t, gloasVote(1, 2, 2))))
}

func TestGloasVoteChecker_SourceNotBeforeTarget(t *testing.T) {
	expected := gloasVote(2, 2, 0)
	checker := newGloasChecker(fakeSlashingSigner{}, expected)
	require.Error(t, checker.CheckValue(encodeGloasVote(t, gloasVote(2, 2, 0))))
}

// Epoch-only majority-fork protection: a vote whose target epoch differs from the operator's expected
// vote is rejected (the index, by contrast, is trusted from the leader and not compared).
func TestGloasVoteChecker_EpochMismatch(t *testing.T) {
	expected := gloasVote(1, 2, 0)
	checker := newGloasChecker(fakeSlashingSigner{}, expected)
	require.Error(t, checker.CheckValue(encodeGloasVote(t, gloasVote(1, 3, 0))))
}

func TestGloasVoteChecker_Slashable(t *testing.T) {
	expected := gloasVote(1, 2, 0)
	checker := newGloasChecker(fakeSlashingSigner{slashable: fmt.Errorf("slashable")}, expected)
	require.Error(t, checker.CheckValue(encodeGloasVote(t, gloasVote(1, 2, 0))))
}

func TestGloasVoteChecker_DecodeError(t *testing.T) {
	expected := gloasVote(1, 2, 0)
	checker := newGloasChecker(fakeSlashingSigner{}, expected)
	require.Error(t, checker.CheckValue([]byte{0x00, 0x01, 0x02})) // too short for a 120-byte vote
}

// --- proposer checker, Gloas (ePBS) ---

const gloasProposerSlot = phase0.Slot(8)

var gloasProposerPK = phase0.BLSPubKey{0x42}

func gloasProposerConsensusData(t *testing.T, dataSSZ []byte) []byte {
	t.Helper()
	cd := &spectypes.ProposerConsensusData{
		Duty: spectypes.ValidatorDuty{
			Type:           spectypes.BNRoleProposer,
			PubKey:         gloasProposerPK,
			ValidatorIndex: 7,
			Slot:           gloasProposerSlot,
		},
		Version: networkconfig.DataVersionGloas,
		DataSSZ: dataSSZ,
	}
	out, err := cd.Encode()
	require.NoError(t, err)
	return out
}

func gloasBlockSSZ(t *testing.T, slot phase0.Slot) []byte {
	t.Helper()
	dataSSZ, err := gloas.TestingBeaconBlock(slot).MarshalSSZ()
	require.NoError(t, err)
	return dataSSZ
}

func newGloasProposerChecker(signer ekm.BeaconSigner) ValueChecker {
	cfg := networkconfig.TestNetworkWithGloas(0)
	return NewProposerChecker(signer, cfg.Beacon, spectypes.ValidatorPK(gloasProposerPK), 7, phase0.BLSPubKey{})
}

// A Gloas proposer value validates via the node-side block decode (there is no spectypes Gloas block
// version); the decoded block's slot drives the slashing check.
func TestProposerChecker_GloasValid(t *testing.T) {
	checker := newGloasProposerChecker(fakeSlashingSigner{})
	require.NoError(t, checker.CheckValue(gloasProposerConsensusData(t, gloasBlockSSZ(t, gloasProposerSlot))))
}

func TestProposerChecker_GloasSlashable(t *testing.T) {
	checker := newGloasProposerChecker(fakeSlashingSigner{slashable: fmt.Errorf("slashable")})
	require.Error(t, checker.CheckValue(gloasProposerConsensusData(t, gloasBlockSSZ(t, gloasProposerSlot))))
}

// DataSSZ that is not a valid Gloas block fails the node-side validity check.
func TestProposerChecker_GloasDecodeError(t *testing.T) {
	checker := newGloasProposerChecker(fakeSlashingSigner{})
	require.Error(t, checker.CheckValue(gloasProposerConsensusData(t, []byte{0x00, 0x01, 0x02})))
}

// --- envelope checker, §6 ---

var envelopeValidatorPK = phase0.BLSPubKey{0x42}

func encodeEnvelopeValue(t *testing.T, slot phase0.Slot, valIdx phase0.ValidatorIndex, pk phase0.BLSPubKey, blockRoot phase0.Root, builderIndex uint64) []byte {
	t.Helper()
	blinded := &gloas.BlindedExecutionPayloadEnvelope{
		PayloadRoot:           phase0.Root{0x09},
		ExecutionRequests:     &electra.ExecutionRequests{},
		BuilderIndex:          builderIndex,
		BeaconBlockRoot:       blockRoot,
		ParentBeaconBlockRoot: phase0.Root{0x08},
	}
	dataSSZ, err := blinded.Encode()
	require.NoError(t, err)
	cd := &gloas.EnvelopeConsensusData{
		Duty: spectypes.ValidatorDuty{
			Type:           spectypes.BNRoleEnvelopeBuilder,
			Slot:           slot,
			ValidatorIndex: valIdx,
			PubKey:         pk,
		},
		DataSSZ: dataSSZ,
	}
	out, err := cd.Encode()
	require.NoError(t, err)
	return out
}

func newEnvelopeCheckerWithRoot(slot phase0.Slot, root phase0.Root) ValueChecker {
	store := NewProposedBlockRoots()
	store.Set(slot, root)
	return NewEnvelopeChecker(store, slot, spectypes.ValidatorPK(envelopeValidatorPK), 3)
}

// A self-build envelope whose BeaconBlockRoot matches the §4-decided root for the slot passes.
func TestEnvelopeChecker_Valid(t *testing.T) {
	root := phase0.Root{0xaa}
	checker := newEnvelopeCheckerWithRoot(7, root)
	require.NoError(t, checker.CheckValue(encodeEnvelopeValue(t, 7, 3, envelopeValidatorPK, root, uint64(gloas.BuilderIndexSelfBuild))))
}

func TestEnvelopeChecker_NotSelfBuild(t *testing.T) {
	root := phase0.Root{0xaa}
	checker := newEnvelopeCheckerWithRoot(7, root)
	require.Error(t, checker.CheckValue(encodeEnvelopeValue(t, 7, 3, envelopeValidatorPK, root, 5)))
}

func TestEnvelopeChecker_WrongBlockRoot(t *testing.T) {
	checker := newEnvelopeCheckerWithRoot(7, phase0.Root{0xaa})
	require.Error(t, checker.CheckValue(encodeEnvelopeValue(t, 7, 3, envelopeValidatorPK, phase0.Root{0xbb}, uint64(gloas.BuilderIndexSelfBuild))))
}

// The §4 root must be present — the proposer runner must have decided and recorded it.
func TestEnvelopeChecker_NoDecidedRoot(t *testing.T) {
	checker := NewEnvelopeChecker(NewProposedBlockRoots(), 7, spectypes.ValidatorPK(envelopeValidatorPK), 3)
	require.Error(t, checker.CheckValue(encodeEnvelopeValue(t, 7, 3, envelopeValidatorPK, phase0.Root{0xaa}, uint64(gloas.BuilderIndexSelfBuild))))
}

func TestEnvelopeChecker_WrongSlot(t *testing.T) {
	root := phase0.Root{0xaa}
	checker := newEnvelopeCheckerWithRoot(7, root)
	require.Error(t, checker.CheckValue(encodeEnvelopeValue(t, 8, 3, envelopeValidatorPK, root, uint64(gloas.BuilderIndexSelfBuild))))
}

func TestEnvelopeChecker_DecodeError(t *testing.T) {
	checker := newEnvelopeCheckerWithRoot(7, phase0.Root{0xaa})
	require.Error(t, checker.CheckValue([]byte{0x00, 0x01}))
}
