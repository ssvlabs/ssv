package validation

import (
	"errors"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
)

func TestPartialSignatureTypeMatchesRole_ProposerPreferences(t *testing.T) {
	mv := &messageValidator{}
	require.True(t, mv.partialSignatureTypeMatchesRole(spectypes.ProposerPreferencesPartialSig, spectypes.RoleProposerPreferences))
	require.False(t, mv.partialSignatureTypeMatchesRole(spectypes.PostConsensusPartialSig, spectypes.RoleProposerPreferences))
	require.False(t, mv.partialSignatureTypeMatchesRole(spectypes.ProposerPreferencesPartialSig, spectypes.RolePTCAttester))
}

func TestValidPartialSigMsgType_ProposerPreferences(t *testing.T) {
	mv := &messageValidator{}
	require.True(t, mv.validPartialSigMsgType(spectypes.ProposerPreferencesPartialSig))
}

// ProposerPreferences partial sigs ride the future proposal slot, so validateSlotTime must allow them
// up to the proposer-lookahead window early — but no other role, and not beyond the window.
func TestValidateSlotTime_ProposerPreferencesEarliness(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	mv := &messageValidator{netCfg: netCfg}

	slot := phase0.Slot(1000)
	allowance := time.Duration(proposerPreferencesEarlyEpochs*netCfg.SlotsPerEpoch) * netCfg.SlotDuration

	tt := []struct {
		name     string
		role     spectypes.RunnerRole
		earlyBy  time.Duration
		accepted bool
	}{
		{"preferences within the lookahead window", spectypes.RoleProposerPreferences, allowance - time.Second, true},
		{"preferences beyond the lookahead window", spectypes.RoleProposerPreferences, allowance + time.Minute, false},
		{"another role gets no early allowance", spectypes.RoleProposer, allowance - time.Second, false},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			receivedAt := netCfg.SlotStartTime(slot).Add(-tc.earlyBy)
			err := mv.validateSlotTime(slot, tc.role, receivedAt)
			if tc.accepted {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrEarlySlotMessage)
			}
		})
	}
}

// ProposerPreferences is exempt from the monotonic slot-advance rule (a signer holds its whole
// lookahead at once); other validator roles still enforce it.
func TestMonotonicSlotRole_ProposerPreferences(t *testing.T) {
	mv := &messageValidator{}
	require.False(t, mv.monotonicSlotRole(spectypes.RoleProposerPreferences))
	require.True(t, mv.monotonicSlotRole(spectypes.RoleProposer))
	require.True(t, mv.monotonicSlotRole(spectypes.RoleValidatorRegistration))
	require.False(t, mv.monotonicSlotRole(spectypes.RoleCommittee))
}

func TestStoredSlotCount_ProposerPreferences(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	mv := &messageValidator{netCfg: netCfg}

	require.Equal(t, mv.maxStoredSlots(), mv.storedSlotCount(spectypes.RoleProposer))
	require.Equal(t,
		proposerPreferencesEarlyEpochs*netCfg.SlotsPerEpoch+mv.maxStoredSlots(),
		mv.storedSlotCount(spectypes.RoleProposerPreferences))
}

// Two proposal slots exactly one default-ring apart collide in the default ring but stay distinct in
// the lookahead-sized proposer-preferences ring, keeping per-slot dedup exact.
func TestProposerPreferencesRingAvoidsLookaheadCollision(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	mv := &messageValidator{netCfg: netCfg}

	slotA := phase0.Slot(1000)
	slotB := slotA + phase0.Slot(mv.maxStoredSlots()) // collides with slotA in the default ring

	osDefault := newOperatorState(mv.maxStoredSlots())
	osDefault.SetSignerStateForSlot(slotA, 0, &SignerStateForSlotRound{Slot: slotA})
	osDefault.SetSignerStateForSlot(slotB, 0, &SignerStateForSlotRound{Slot: slotB})
	require.Nil(t, osDefault.GetSignerStateForSlot(slotA), "default ring should drop slotA on collision")

	osPrefs := newOperatorState(mv.storedSlotCount(spectypes.RoleProposerPreferences))
	osPrefs.SetSignerStateForSlot(slotA, 0, &SignerStateForSlotRound{Slot: slotA})
	osPrefs.SetSignerStateForSlot(slotB, 0, &SignerStateForSlotRound{Slot: slotB})
	require.NotNil(t, osPrefs.GetSignerStateForSlot(slotA))
	require.NotNil(t, osPrefs.GetSignerStateForSlot(slotB))
}

// With the monotonic check skipped, lateness is the role's replay bound: a preference around its
// proposal slot is fine, one for a slot well behind is late.
func TestMessageLateness_ProposerPreferences(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	mv := &messageValidator{netCfg: netCfg}
	slot := phase0.Slot(1000)

	notLate := mv.messageLateness(slot, spectypes.RoleProposerPreferences, netCfg.SlotStartTime(slot))
	require.LessOrEqual(t, notLate, time.Duration(0))

	late := mv.messageLateness(slot, spectypes.RoleProposerPreferences, netCfg.SlotStartTime(slot+100))
	require.Greater(t, late, time.Duration(0))
}

// ValidatorRegistration is deprecated at the Gloas fork — valid pre-Gloas, rejected for Gloas slots.
func TestValidRoleAtSlot_ValidatorRegistrationDeprecatedAtGloas(t *testing.T) {
	const gloasEpoch = 100
	netCfg := networkconfig.TestNetworkWithGloas(gloasEpoch)
	mv := &messageValidator{netCfg: netCfg}

	preGloasSlot := phase0.Slot(uint64(gloasEpoch-1) * netCfg.SlotsPerEpoch)
	gloasSlot := phase0.Slot(uint64(gloasEpoch) * netCfg.SlotsPerEpoch)

	require.True(t, mv.validRoleAtSlot(spectypes.RoleValidatorRegistration, preGloasSlot))
	require.False(t, mv.validRoleAtSlot(spectypes.RoleValidatorRegistration, gloasSlot))
}

func TestDutyLimit_ProposerPreferences(t *testing.T) {
	mv := &messageValidator{netCfg: networkconfig.TestNetwork}
	msgID := spectypes.NewMsgID(spectypes.DomainType{}, make([]byte, 48), spectypes.RoleProposerPreferences)

	limit, ok := mv.dutyLimit(msgID, 0, nil)
	require.True(t, ok)
	require.Equal(t, mv.netCfg.SlotsPerEpoch, limit)
}

// A proposer-preferences message must reference a real proposal slot for the validator once the
// slot's epoch is fetched AND fresh; an unfetched epoch is tolerated (the duty fetch may be in
// flight), and so is a stale one (fetched before the latest indices change — rejecting on it would
// permanently starve a just-added validator's one-shot partials).
func TestValidateBeaconDuty_ProposerPreferencesRequiresAssignment(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	const epoch = phase0.Epoch(5)
	idx := phase0.ValidatorIndex(7)
	slot := phase0.Slot(uint64(epoch)*netCfg.SlotsPerEpoch + 3)

	ds := dutystore.New()
	assigned := []dutystore.StoreDuty[eth2apiv1.ProposerDuty]{
		{Slot: slot, ValidatorIndex: idx, Duty: &eth2apiv1.ProposerDuty{Slot: slot, ValidatorIndex: idx}, InCommittee: true},
	}
	ds.Proposer.Set(epoch, assigned)
	mv := &messageValidator{netCfg: netCfg, dutyStore: ds}

	indices := []phase0.ValidatorIndex{idx}
	// Assigned proposal slot → accepted.
	require.NoError(t, mv.validateBeaconDuty(spectypes.RoleProposerPreferences, slot, indices, false))
	// Same (fetched) epoch, unassigned slot → rejected.
	require.ErrorIs(t, mv.validateBeaconDuty(spectypes.RoleProposerPreferences, slot+1, indices, false), ErrNoDuty)
	// Unfetched epoch → tolerated.
	unfetched := phase0.Slot(uint64(epoch+10) * netCfg.SlotsPerEpoch)
	require.NoError(t, mv.validateBeaconDuty(spectypes.RoleProposerPreferences, unfetched, indices, false))
	// Stale epoch (fetched before the latest indices change) → tolerated like an unfetched one,
	// until a refetch restores enforcement.
	ds.Proposer.MarkEpochsStale(epoch)
	require.NoError(t, mv.validateBeaconDuty(spectypes.RoleProposerPreferences, slot+1, indices, false))
	ds.Proposer.Set(epoch, assigned)
	require.ErrorIs(t, mv.validateBeaconDuty(spectypes.RoleProposerPreferences, slot+1, indices, false), ErrNoDuty)
}

// SignerState tracks distinct ProposerPreferences signing roots (SIP #94 §5): recording is idempotent
// per root, and has/count reflect the distinct set.
func TestSignerState_ProposerPreferencesRoots(t *testing.T) {
	s := &SignerState{}
	r1 := [32]byte{1}
	r2 := [32]byte{2}

	require.Equal(t, 0, s.proposerPreferencesRootCount())
	require.False(t, s.hasProposerPreferencesRoot(r1))

	s.recordProposerPreferencesRoot(r1)
	require.True(t, s.hasProposerPreferencesRoot(r1))
	require.Equal(t, 1, s.proposerPreferencesRootCount())

	// Recording an already-seen root is a no-op.
	s.recordProposerPreferencesRoot(r1)
	require.Equal(t, 1, s.proposerPreferencesRootCount())

	s.recordProposerPreferencesRoot(r2)
	require.True(t, s.hasProposerPreferencesRoot(r2))
	require.Equal(t, 2, s.proposerPreferencesRootCount())
}

// ProposerPreferences pre-consensus admits up to maxProposerPreferencesDistinctRoots distinct signing
// roots per (slot, signer) — a dependent_root refresh re-emits under a new root (SIP #94 §5). Only a
// same-peer repeat of a seen root is REJECT'd; a relayed repeat or a distinct root past the cap is IGNORE'd.
func TestValidatePartialSignatureMessageLimit_ProposerPreferences(t *testing.T) {
	ppMsg := func(root [32]byte) *spectypes.PartialSignatureMessages {
		return &spectypes.PartialSignatureMessages{
			Type:     spectypes.ProposerPreferencesPartialSig,
			Slot:     1,
			Messages: []*spectypes.PartialSignatureMessage{{SigningRoot: root}},
		}
	}
	record := func(ss *SignerStateForSlotRound, from peer.ID, root [32]byte) {
		ss.Peer(from).recordProposerPreferencesRoot(root)
		ss.World.recordProposerPreferencesRoot(root)
	}
	root := func(b byte) [32]byte { return [32]byte{b} }

	const peerA = peer.ID("A")
	const peerB = peer.ID("B")

	t.Run("distinct roots accepted up to the bound, then further distinct roots are ignored", func(t *testing.T) {
		ss := newSignerState(1, specqbft.FirstRound)
		for i := 0; i < maxProposerPreferencesDistinctRoots; i++ {
			r := root(byte(i + 1))
			require.NoError(t, validatePartialSignatureMessageLimit(ppMsg(r), peerA, ss))
			record(ss, peerA, r)
		}

		// A distinct root beyond the cap is rate-limited (IGNORE), not a provable violation (REJECT).
		var valErr Error
		err := validatePartialSignatureMessageLimit(ppMsg(root(99)), peerA, ss)
		require.ErrorIs(t, err, ErrTooManyPartialSigMessage)
		require.True(t, errors.As(err, &valErr))
		require.False(t, valErr.reject)
	})

	t.Run("same-peer duplicate root is rejected, a relayed duplicate is ignored", func(t *testing.T) {
		ss := newSignerState(1, specqbft.FirstRound)
		r := root(1)
		require.NoError(t, validatePartialSignatureMessageLimit(ppMsg(r), peerA, ss))
		record(ss, peerA, r)

		var valErr Error
		err := validatePartialSignatureMessageLimit(ppMsg(r), peerA, ss)
		require.ErrorIs(t, err, ErrTooManyPartialSigMessage)
		require.True(t, errors.As(err, &valErr))
		require.True(t, valErr.reject)

		err = validatePartialSignatureMessageLimit(ppMsg(r), peerB, ss)
		require.ErrorIs(t, err, ErrTooManyPartialSigMessage)
		require.True(t, errors.As(err, &valErr))
		require.False(t, valErr.reject)
	})

	t.Run("a fresh peer's new distinct root is ignored once the world budget is spent", func(t *testing.T) {
		ss := newSignerState(1, specqbft.FirstRound)
		for i := 0; i < maxProposerPreferencesDistinctRoots; i++ {
			record(ss, peerA, root(byte(i+1)))
		}

		var valErr Error
		err := validatePartialSignatureMessageLimit(ppMsg(root(99)), peerB, ss)
		require.ErrorIs(t, err, ErrTooManyPartialSigMessage)
		require.True(t, errors.As(err, &valErr))
		require.False(t, valErr.reject)
	})
}
