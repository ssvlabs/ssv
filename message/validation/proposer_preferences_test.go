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
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/protocol/v2/types/ssvtestingutils"
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

// ProposerPreferences is exempt from the monotonic slot-advance rule (a signer holds its whole
// lookahead at once); other validator roles still enforce it.
func TestMonotonicSlotRole_ProposerPreferences(t *testing.T) {
	mv := &messageValidator{}
	require.False(t, mv.monotonicSlotRole(spectypes.RoleProposerPreferences))
	require.True(t, mv.monotonicSlotRole(spectypes.RoleProposer))
	require.True(t, mv.monotonicSlotRole(spectypes.RoleValidatorRegistration))
	require.False(t, mv.monotonicSlotRole(spectypes.RoleCommittee))
}

// Every proposal slot a preference is accepted for at one moment has its own ring slot. The widest moment is an
// epoch's last slot, where the early margin admits the epoch after next; with 4s slots the late and early margins
// together exceed a slot, so it still admits LateSlotAllowance slots back too — the case the spare slot is for.
func TestStoredSlotCount_ProposerPreferences(t *testing.T) {
	require.Equal(t, (&messageValidator{netCfg: networkconfig.TestNetwork}).maxStoredSlots(),
		(&messageValidator{netCfg: networkconfig.TestNetwork}).storedSlotCount(spectypes.RoleProposer))

	for _, slotDuration := range []time.Duration{12 * time.Second, 4 * time.Second} {
		t.Run(slotDuration.String(), func(t *testing.T) {
			beacon := *networkconfig.TestNetwork.Beacon
			beacon.SlotDuration = slotDuration
			netCfg := &networkconfig.Network{Beacon: &beacon, SSV: networkconfig.TestNetwork.SSV}
			mv := &messageValidator{netCfg: netCfg}
			role := spectypes.RoleProposerPreferences

			// Every moment of the two slots around the turn into epoch 11, against the slots of epochs 9 to 13.
			turn := netCfg.SlotStartTime(netCfg.FirstSlotAtEpoch(11))
			var widest uint64
			for at := turn.Add(-slotDuration); at.Before(turn.Add(slotDuration)); at = at.Add(10 * time.Millisecond) {
				var accepted uint64
				for slot := netCfg.FirstSlotAtEpoch(9); slot < netCfg.FirstSlotAtEpoch(14); slot++ {
					if mv.validateSlotTime(slot, role, at) == nil {
						accepted++
					}
				}
				widest = max(widest, accepted)
			}
			require.LessOrEqual(t, widest, mv.storedSlotCount(role))
		})
	}
}

// Proposer-preferences validation state is kept for the role's whole acceptance window (SIP #94 §7). A
// preference emitted early in an epoch for a slot late in the next stays acceptable for about two epochs,
// longer than the cache's default TTL, and dropping its state earlier would reopen its dedup and duty budgets.
func TestValidatorState_ProposerPreferencesOutlivesDefaultTTL(t *testing.T) {
	base := networkconfig.TestNetwork
	beacon := *base.Beacon
	beacon.SlotDuration = 10 * time.Millisecond // shrink time; the ratio is what matters
	netCfg := &networkconfig.Network{Beacon: &beacon, SSV: base.SSV}
	mv := New(netCfg, nil, nil, nil, nil).(*messageValidator)
	ci := CommitteeInfo{committee: []spectypes.OperatorID{1, 2, 3, 4}}

	const slot = phase0.Slot(100)
	root := [32]byte{1}
	recorded := newSignerState(slot, specqbft.FirstRound)
	recorded.SeenProposerPreferencesRoots.record(root)

	prefsKey := ssvtestingutils.NewMsgID(spectypes.DomainType{}, make([]byte, 48), spectypes.RoleProposerPreferences)
	proposerKey := ssvtestingutils.NewMsgID(spectypes.DomainType{}, make([]byte, 48), spectypes.RoleProposer)
	mv.validatorState(prefsKey, ci).OperatorState(0).SetSignerStateForSlot(slot, netCfg.EstimatedEpochAtSlot(slot), recorded)
	mv.validatorState(proposerKey, ci)

	// Idle past the default TTL (maxStoredSlots, 34 slots) but well inside the preferences state's (68 slots).
	time.Sleep(45 * beacon.SlotDuration)
	prefs := mv.states.Get(prefsKey)
	require.NotNil(t, prefs, "preferences state outlives the default TTL")
	require.Nil(t, mv.states.Get(proposerKey), "other roles keep the default TTL")

	// The retained state still IGNOREs a repeat of the recorded root.
	repeat := &spectypes.PartialSignatureMessages{
		Type:     spectypes.ProposerPreferencesPartialSig,
		Slot:     slot,
		Messages: []*spectypes.PartialSignatureMessage{{SigningRoot: root}},
	}
	err := validatePartialSignatureMessageLimit(repeat, "", prefs.Value().OperatorState(0).GetSignerStateForSlot(slot))
	require.ErrorIs(t, err, ErrTooManyPartialSigMessage)
	var valErr Error
	require.ErrorAs(t, err, &valErr)
	require.False(t, valErr.Reject())
}

// The duty-count ring spans every epoch a role's lateness window can still accept (SIP #94 §7): the
// proposer lookahead for preferences, three epochs for the roles with the epoch-long TTL, two for the rest.
func TestStoredEpochCount(t *testing.T) {
	mv := &messageValidator{netCfg: networkconfig.TestNetwork}
	require.Equal(t, uint64(3), mv.storedEpochCount(spectypes.RoleProposerPreferences))
	for _, role := range []spectypes.RunnerRole{spectypes.RoleCommittee, spectypes.RoleAggregatorCommittee, ssvtypes.RoleAggregator} {
		require.Equal(t, uint64(3), mv.storedEpochCount(role), role.String())
	}
	for _, role := range []spectypes.RunnerRole{spectypes.RoleProposer, ssvtypes.RoleSyncCommitteeContribution, spectypes.RolePTCAttester} {
		require.Equal(t, uint64(2), mv.storedEpochCount(role), role.String())
	}
}

// The per-epoch duty limit must count each epoch on its own when a lookahead epoch's preferences arrive
// before the current epoch's (SIP #94 §7). With only a current and a previous bucket, the previous epoch's
// tail would share a bucket with the current epoch once the lookahead epoch had been seen, so a busy
// cluster's epoch-N preferences inflated epoch N-1's count and honest preferences were IGNORE'd as over the
// limit. Three consecutive epochs are live at once: the previous epoch's tail, the current and the next.
func TestValidateDutyCount_ProposerPreferencesAcrossLookaheadEpochs(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	mv := &messageValidator{netCfg: netCfg}
	role := spectypes.RoleProposerPreferences
	msgID := ssvtestingutils.NewMsgID(spectypes.DomainType{}, make([]byte, 48), role)
	os := newOperatorState(mv.storedSlotCount(role), mv.storedEpochCount(role))

	const base = phase0.Epoch(100)
	firstSlot := func(epoch phase0.Epoch) phase0.Slot { return phase0.Slot(uint64(epoch) * netCfg.SlotsPerEpoch) }
	// accept mirrors what validation does for a duty's first accepted message: the count check, then the
	// slot's state recorded and counted.
	accept := func(slot phase0.Slot) {
		require.NoError(t, mv.validateDutyCount(msgID, slot, nil, os))
		os.SetSignerStateForSlot(slot, netCfg.EstimatedEpochAtSlot(slot), newSignerState(slot, specqbft.FirstRound))
	}

	// A cluster proposing in every slot: the lookahead epoch fills first, then the current epoch.
	for i := range netCfg.SlotsPerEpoch {
		accept(firstSlot(base+1) + phase0.Slot(i))
	}
	for i := range netCfg.SlotsPerEpoch {
		accept(firstSlot(base) + phase0.Slot(i))
	}
	require.Equal(t, netCfg.SlotsPerEpoch, os.DutyCount(base+1))
	require.Equal(t, netCfg.SlotsPerEpoch, os.DutyCount(base))

	// The tail of the previous epoch still counts on its own, leaving both later epochs' counts untouched.
	accept(firstSlot(base) - 1)
	require.Equal(t, uint64(1), os.DutyCount(base-1))
	require.Equal(t, netCfg.SlotsPerEpoch, os.DutyCount(base))
	require.Equal(t, netCfg.SlotsPerEpoch, os.DutyCount(base+1))
}

// Two proposal slots exactly one default-ring apart collide in the default ring but stay distinct in
// the lookahead-sized proposer-preferences ring, keeping per-slot dedup exact.
func TestProposerPreferencesRingAvoidsLookaheadCollision(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	mv := &messageValidator{netCfg: netCfg}

	slotA := phase0.Slot(1000)
	slotB := slotA + phase0.Slot(mv.maxStoredSlots()) // collides with slotA in the default ring

	osDefault := newOperatorState(mv.maxStoredSlots(), mv.storedEpochCount(spectypes.RoleProposer))
	osDefault.SetSignerStateForSlot(slotA, 0, &SignerStateForSlotRound{Slot: slotA})
	osDefault.SetSignerStateForSlot(slotB, 0, &SignerStateForSlotRound{Slot: slotB})
	require.Nil(t, osDefault.GetSignerStateForSlot(slotA), "default ring should drop slotA on collision")

	osPrefs := newOperatorState(mv.storedSlotCount(spectypes.RoleProposerPreferences), mv.storedEpochCount(spectypes.RoleProposerPreferences))
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

// Registrations end at the Gloas fork (SIP #94 §5): from the epoch after it every registration message is
// ignored, whatever its slot, since no honest node sends one any more and registrations have no lateness
// limit. The fork epoch itself still admits pre-fork registrations in flight, and a network without a
// scheduled fork is unaffected.
func TestRegistrationsRetiredAfterGloas(t *testing.T) {
	const gloasEpoch = 100
	netCfg := networkconfig.TestNetworkWithGloas(gloasEpoch)
	mv := &messageValidator{netCfg: netCfg}
	at := func(epoch phase0.Epoch) time.Time { return netCfg.SlotStartTime(netCfg.FirstSlotAtEpoch(epoch)) }

	require.False(t, mv.registrationsRetired(at(gloasEpoch-1)))
	require.False(t, mv.registrationsRetired(at(gloasEpoch)))
	require.True(t, mv.registrationsRetired(at(gloasEpoch+1)))
	require.True(t, mv.registrationsRetired(at(gloasEpoch+1000)))
	require.False(t, (&messageValidator{netCfg: networkconfig.TestNetwork}).registrationsRetired(at(gloasEpoch+1)))

	// Through the duty-logic checks: a registration stamped with a pre-fork slot passes during the fork epoch
	// and is ignored (not rejected: the condition comes from the local clock) from the epoch after.
	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.ValidatorRegistrationPartialSig,
		Slot:     netCfg.FirstSlotAtEpoch(gloasEpoch - 1),
		Messages: []*spectypes.PartialSignatureMessage{{Signer: 1, ValidatorIndex: 1}},
	}
	signed := &spectypes.SignedSSVMessage{
		OperatorIDs: []spectypes.OperatorID{1},
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   ssvtestingutils.NewMsgID(netCfg.DomainType, make([]byte, 48), spectypes.RoleValidatorRegistration),
		},
	}
	ci := newCommitteeInfo(spectypes.CommitteeID{}, []spectypes.OperatorID{1, 2, 3, 4}, []phase0.ValidatorIndex{1}, 0, 0)
	newState := func() *ValidatorState {
		return &ValidatorState{operators: make([]*OperatorState, 4), storedSlotCount: mv.maxStoredSlots(), storedEpochCount: 2}
	}

	require.NoError(t, mv.validatePartialSigMessagesByDutyLogic(signed, msgs, ci, "", at(gloasEpoch), newState()))

	err := mv.validatePartialSigMessagesByDutyLogic(signed, msgs, ci, "", at(gloasEpoch+1), newState())
	require.ErrorIs(t, err, ErrValidatorRegistrationRetired)
	var valErr Error
	require.ErrorAs(t, err, &valErr)
	require.False(t, valErr.Reject(), "IGNORE, not REJECT")
}

func TestDutyLimit_ProposerPreferences(t *testing.T) {
	mv := &messageValidator{netCfg: networkconfig.TestNetwork}
	msgID := ssvtestingutils.NewMsgID(spectypes.DomainType{}, make([]byte, 48), spectypes.RoleProposerPreferences)

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

// A signer's slot-round state tracks distinct ProposerPreferences signing roots (SIP #94 §5): recording
// is idempotent per root, and the set reflects the distinct roots.
func TestSlotRoundState_ProposerPreferencesRoots(t *testing.T) {
	s := &SignerStateForSlotRound{}
	r1 := [32]byte{1}
	r2 := [32]byte{2}

	require.Empty(t, s.SeenProposerPreferencesRoots)
	require.False(t, s.SeenProposerPreferencesRoots.has(r1))

	s.SeenProposerPreferencesRoots.record(r1)
	require.True(t, s.SeenProposerPreferencesRoots.has(r1))
	require.Len(t, s.SeenProposerPreferencesRoots, 1)

	// Recording an already-seen root is a no-op.
	s.SeenProposerPreferencesRoots.record(r1)
	require.Len(t, s.SeenProposerPreferencesRoots, 1)

	s.SeenProposerPreferencesRoots.record(r2)
	require.True(t, s.SeenProposerPreferencesRoots.has(r2))
	require.Len(t, s.SeenProposerPreferencesRoots, 2)
}

// ProposerPreferences pre-consensus admits up to maxProposerPreferencesDistinctRoots distinct signing
// roots per (slot, signer) — a dependent_root refresh re-emits under a new root (SIP #94 §5). A repeat
// of a recorded root, whichever peer relays it, and a distinct root past the cap are both IGNORE'd (§7).
func TestValidatePartialSignatureMessageLimit_ProposerPreferences(t *testing.T) {
	ppMsg := func(root [32]byte) *spectypes.PartialSignatureMessages {
		return &spectypes.PartialSignatureMessages{
			Type:     spectypes.ProposerPreferencesPartialSig,
			Slot:     1,
			Messages: []*spectypes.PartialSignatureMessage{{SigningRoot: root}},
		}
	}
	record := func(ss *SignerStateForSlotRound, root [32]byte) {
		ss.SeenProposerPreferencesRoots.record(root) // as updatePartialSignatureState records on ACCEPT
	}
	root := func(b byte) [32]byte { return [32]byte{b} }

	const peerA = peer.ID("A")
	const peerB = peer.ID("B")

	t.Run("distinct roots accepted up to the bound, then further distinct roots are ignored", func(t *testing.T) {
		ss := newSignerState(1, specqbft.FirstRound)
		for i := 0; i < maxProposerPreferencesDistinctRoots; i++ {
			r := root(byte(i + 1))
			require.NoError(t, validatePartialSignatureMessageLimit(ppMsg(r), peerA, ss))
			record(ss, r)
		}

		// A distinct root beyond the cap is rate-limited (IGNORE), not a provable violation (REJECT).
		var valErr Error
		err := validatePartialSignatureMessageLimit(ppMsg(root(99)), peerA, ss)
		require.ErrorIs(t, err, ErrTooManyPartialSigMessage)
		require.True(t, errors.As(err, &valErr))
		require.False(t, valErr.reject)
	})

	// The sender itself repeats a root after a restart, once the recipient's gossip duplicate cache
	// has expired (issue #3016); a relay repeats it whenever meshes overlap. Neither proves peer fault.
	t.Run("a repeated root is ignored whichever peer relays it", func(t *testing.T) {
		ss := newSignerState(1, specqbft.FirstRound)
		r := root(1)
		require.NoError(t, validatePartialSignatureMessageLimit(ppMsg(r), peerA, ss))
		record(ss, r)

		for _, from := range []peer.ID{peerA, peerB} {
			var valErr Error
			err := validatePartialSignatureMessageLimit(ppMsg(r), from, ss)
			require.ErrorIs(t, err, ErrTooManyPartialSigMessage)
			require.True(t, errors.As(err, &valErr))
			require.False(t, valErr.reject, "peer %s", from)
		}
	})

	t.Run("a fresh peer's new distinct root is ignored once the signer's budget is spent", func(t *testing.T) {
		ss := newSignerState(1, specqbft.FirstRound)
		for i := 0; i < maxProposerPreferencesDistinctRoots; i++ {
			record(ss, root(byte(i+1)))
		}

		var valErr Error
		err := validatePartialSignatureMessageLimit(ppMsg(root(99)), peerB, ss)
		require.ErrorIs(t, err, ErrTooManyPartialSigMessage)
		require.True(t, errors.As(err, &valErr))
		require.False(t, valErr.reject)
	})
}
