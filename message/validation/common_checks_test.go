package validation

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// validateSlotTime's early bound is clockErrorTolerance plus earlyMessageMargin before the moment a message
// for the slot is expected: the slot's start for every role (issue #3026), and for proposer preferences the
// start of the epoch before the slot's — the proposer lookahead, the current epoch and the next.
func TestValidateSlotTime_Earliness(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	mv := &messageValidator{netCfg: netCfg}

	slot := phase0.Slot(1000)
	margin := clockErrorTolerance + earlyMessageMargin

	for _, role := range runnerRoles() {
		if role == spectypes.RoleProposerPreferences {
			continue // its lookahead window is covered below
		}
		t.Run(message.RunnerRoleToString(role), func(t *testing.T) {
			require.NoError(t, mv.validateSlotTime(slot, role, netCfg.SlotStartTime(slot).Add(-margin)), "at the margin")
			err := mv.validateSlotTime(slot, role, netCfg.SlotStartTime(slot).Add(-margin-time.Millisecond))
			require.ErrorIs(t, err, ErrEarlySlotMessage, "beyond the margin")
		})
	}

	t.Run("proposer preferences", func(t *testing.T) {
		role := spectypes.RoleProposerPreferences
		// slot 1000 sits in epoch 31; it is expected from the start of epoch 30 (slot 960) on.
		expectedFrom := netCfg.SlotStartTime(netCfg.FirstSlotAtEpoch(netCfg.EstimatedEpochAtSlot(slot) - 1))
		require.NoError(t, mv.validateSlotTime(slot, role, expectedFrom.Add(-margin)), "at the lookahead window")
		require.NoError(t, mv.validateSlotTime(slot, role, expectedFrom.Add(netCfg.SlotDuration)), "inside the window")
		err := mv.validateSlotTime(slot, role, expectedFrom.Add(-margin-time.Millisecond))
		require.ErrorIs(t, err, ErrEarlySlotMessage, "beyond the lookahead window")

		// From the first slot of epoch 30, the last slot of epoch 31 is in reach and the first of epoch 32 is
		// not — the lookahead is two epochs, not two epochs' worth of slots from the message.
		now := netCfg.SlotStartTime(netCfg.FirstSlotAtEpoch(30))
		require.NoError(t, mv.validateSlotTime(netCfg.FirstSlotAtEpoch(32)-1, role, now), "last slot of the next epoch")
		require.ErrorIs(t, mv.validateSlotTime(netCfg.FirstSlotAtEpoch(32), role, now), ErrEarlySlotMessage, "first slot of the epoch after")
	})
}

// TestCommitteeRole locks the publish/receive symmetry for committee-backed roles.
// Every role that p2pNetwork.BroadcastAtSlot routes to the committee topic
// (RoleCommittee, RoleAggregatorCommittee) must resolve to the committee lookup on
// receive, otherwise those messages would be rejected as unknown validators.
func TestCommitteeRole(t *testing.T) {
	mv := &messageValidator{}

	require.True(t, mv.committeeRole(spectypes.RoleCommittee))
	require.True(t, mv.committeeRole(spectypes.RoleAggregatorCommittee))

	require.False(t, mv.committeeRole(spectypes.RoleProposer))
	require.False(t, mv.committeeRole(ssvtypes.RoleAggregator))
}

// runnerRoles returns every role validRoleUnion admits, so tests ranging over it pick up a new role.
func runnerRoles() []spectypes.RunnerRole {
	mv := &messageValidator{}
	var roles []spectypes.RunnerRole
	for role := spectypes.RoleUnknown; role < 256; role++ {
		if mv.validRoleUnion(role) {
			roles = append(roles, role)
		}
	}
	return roles
}

type namedSlot struct {
	name string
	slot phase0.Slot
}

// extremeSlots returns slots a message may carry that SlotStartTime can't convert to a time.
func extremeSlots(beacon *networkconfig.Beacon) []namedSlot {
	return []namedSlot{
		{"first unrepresentable", phase0.Slot(math.MaxInt64/int64(beacon.SlotDuration)) + 1},
		{"max int64", math.MaxInt64}, // passes a bare slot > MaxInt64 check, but slot+ttl overflows
		{"2^63", 1 << 63},
		{"max uint64", math.MaxUint64},
	}
}

// validateSlotTime rejects an extreme slot as too far ahead rather than panicking, for every role, including
// before genesis, where the current slot can't be estimated.
func TestValidateSlotTime_ExtremeSlots(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	mv := &messageValidator{netCfg: netCfg}

	arrivals := map[string]time.Time{
		"after genesis":  netCfg.SlotStartTime(1000),
		"before genesis": netCfg.GenesisTime.Add(-time.Hour),
	}
	for _, role := range runnerRoles() {
		for name, receivedAt := range arrivals {
			t.Run(message.RunnerRoleToString(role)+"/"+name, func(t *testing.T) {
				for _, s := range extremeSlots(netCfg.Beacon) {
					require.NotPanics(t, func() {
						require.ErrorIs(t, mv.validateSlotTime(s.slot, role, receivedAt), ErrSlotTooFarAhead, s.name)
					})
				}
			})
		}
	}
}

// TestFurthestAcceptableSlot pins the bound at three epochs past the current slot, since the extreme-slot tests
// derive a slot from it and can't catch it moving, and checks it is never tighter than messageEarliness: every slot
// messageEarliness admits is within it, for every role, at arrivals across an epoch (including the worst case, the
// early-arrival margin before the next one), for slot durations and epoch lengths that meet the bound's precondition.
func TestFurthestAcceptableSlot(t *testing.T) {
	margin := clockErrorTolerance + earlyMessageMargin
	for _, slotDuration := range []time.Duration{12 * time.Second, 2 * time.Second, 500 * time.Millisecond} {
		for _, slotsPerEpoch := range []uint64{32, 8} {
			beacon := *networkconfig.TestNetwork.Beacon
			beacon.SlotDuration = slotDuration
			beacon.SlotsPerEpoch = slotsPerEpoch
			netCfg := &networkconfig.Network{Beacon: &beacon, SSV: networkconfig.TestNetwork.SSV}
			config := fmt.Sprintf("%v slots, %d per epoch", slotDuration, slotsPerEpoch)
			require.GreaterOrEqual(t, netCfg.EpochDuration(), margin, "%s: outside the bound's precondition", config)
			mv := &messageValidator{netCfg: netCfg}

			epochStart := netCfg.SlotStartTime(netCfg.FirstSlotAtEpoch(10))
			arrivals := []time.Time{
				epochStart,
				epochStart.Add(netCfg.EpochDuration() / 2),
				epochStart.Add(netCfg.EpochDuration() - margin), // the worst case
				epochStart.Add(netCfg.EpochDuration() - time.Millisecond),
			}
			for _, receivedAt := range arrivals {
				current := netCfg.EstimatedSlotAtTime(receivedAt)
				furthest := mv.furthestAcceptableSlot(receivedAt)
				require.Equal(t, current+phase0.Slot(3*slotsPerEpoch), furthest, "%s, received at %v", config, receivedAt)

				for _, role := range runnerRoles() {
					for slot := current; slot <= current+phase0.Slot(5*slotsPerEpoch); slot++ {
						if mv.messageEarliness(slot, role, receivedAt) > margin {
							continue
						}
						require.LessOrEqual(t, slot, furthest, "%s, %v role, received at %v: slot %d is not early but past the bound",
							config, message.RunnerRoleToString(role), receivedAt, slot)
					}
				}
			}
		}
	}
}
