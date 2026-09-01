package exporter

import (
	"fmt"
	"math"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter/rolemask"
	estore "github.com/ssvlabs/ssv/exporter/store"
	"github.com/ssvlabs/ssv/exporter/traces"
	"github.com/ssvlabs/ssv/networkconfig"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// preBooleNetwork returns a *networkconfig.Network clone with the Boole fork
// disabled (never activates). It never mutates the shared TestNetwork.
func preBooleNetwork() *networkconfig.Network {
	ssvCopy := *networkconfig.TestNetwork.SSV
	ssvCopy.Forks.Boole = phase0.Epoch(^uint64(0))
	netCfg := *networkconfig.TestNetwork
	netCfg.SSV = &ssvCopy
	return &netCfg
}

// postBooleNetwork returns a *networkconfig.Network clone with the Boole fork
// active from genesis. It never mutates the shared TestNetwork.
func postBooleNetwork() *networkconfig.Network {
	ssvCopy := *networkconfig.TestNetwork.SSV
	ssvCopy.Forks.Boole = 0
	netCfg := *networkconfig.TestNetwork
	netCfg.SSV = &ssvCopy
	return &netCfg
}

// straddlingNetwork returns a *networkconfig.Network clone whose Boole fork
// activates one epoch after "now", so a slot range that starts before the
// fork epoch and ends after it genuinely straddles the fork boundary. It
// never mutates the shared TestNetwork.
func straddlingNetwork() *networkconfig.Network {
	ssvCopy := *networkconfig.TestNetwork.SSV
	ssvCopy.Forks.Boole = networkconfig.TestNetwork.EstimatedCurrentEpoch() + 1
	netCfg := *networkconfig.TestNetwork
	netCfg.SSV = &ssvCopy
	return &netCfg
}

func TestIsCommitteeDutyAtSlot(t *testing.T) {
	preFork := preBooleNetwork()
	postFork := postBooleNetwork()

	const slot = phase0.Slot(100)

	tests := []struct {
		name     string
		role     spectypes.BeaconRole
		netCfg   *networkconfig.Network
		expected bool
	}{
		{name: "ATTESTER is committee duty pre-fork", role: spectypes.BNRoleAttester, netCfg: preFork, expected: true},
		{name: "ATTESTER is committee duty post-fork", role: spectypes.BNRoleAttester, netCfg: postFork, expected: true},
		{name: "SYNC_COMMITTEE is committee duty pre-fork", role: spectypes.BNRoleSyncCommittee, netCfg: preFork, expected: true},
		{name: "SYNC_COMMITTEE is committee duty post-fork", role: spectypes.BNRoleSyncCommittee, netCfg: postFork, expected: true},
		{name: "AGGREGATOR is a validator duty pre-fork", role: spectypes.BNRoleAggregator, netCfg: preFork, expected: false},
		{name: "AGGREGATOR is a committee duty post-fork", role: spectypes.BNRoleAggregator, netCfg: postFork, expected: true},
		{name: "SYNC_COMMITTEE_CONTRIBUTION is a validator duty pre-fork", role: spectypes.BNRoleSyncCommitteeContribution, netCfg: preFork, expected: false},
		{name: "SYNC_COMMITTEE_CONTRIBUTION is a committee duty post-fork", role: spectypes.BNRoleSyncCommitteeContribution, netCfg: postFork, expected: true},
		{name: "PROPOSER is never a committee duty pre-fork", role: spectypes.BNRoleProposer, netCfg: preFork, expected: false},
		{name: "PROPOSER is never a committee duty post-fork", role: spectypes.BNRoleProposer, netCfg: postFork, expected: false},
		{name: "VALIDATOR_REGISTRATION is never a committee duty pre-fork", role: spectypes.BNRoleValidatorRegistration, netCfg: preFork, expected: false},
		{name: "VALIDATOR_REGISTRATION is never a committee duty post-fork", role: spectypes.BNRoleValidatorRegistration, netCfg: postFork, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Exporter{networkConfig: tt.netCfg}
			assert.Equal(t, tt.expected, e.isCommitteeDutyAtSlot(tt.role, slot))
		})
	}
}

func TestValidateValidatorRequest(t *testing.T) {
	preFork := preBooleNetwork()
	postFork := postBooleNetwork()
	straddle := straddlingNetwork()

	preForkSlot := uint64(preFork.FirstSlotAtEpoch(1))
	postForkSlot := uint64(postFork.FirstSlotAtEpoch(1))
	straddleBoundarySlot := uint64(straddle.FirstSlotAtEpoch(straddle.SSV.Forks.Boole))

	tests := []struct {
		name    string
		netCfg  *networkconfig.Network
		request *ValidatorTracesQuery
		wantErr bool
	}{
		{
			name:   "from greater than to is rejected",
			netCfg: preFork,
			request: &ValidatorTracesQuery{
				From:  10,
				To:    5,
				Roles: []spectypes.BeaconRole{spectypes.BNRoleProposer},
			},
			wantErr: true,
		},
		{
			name:    "no roles is rejected",
			netCfg:  preFork,
			request: &ValidatorTracesQuery{From: 1, To: 1, Roles: nil},
			wantErr: true,
		},
		{
			name:   "committee-backed role without filters is rejected",
			netCfg: preFork,
			request: &ValidatorTracesQuery{
				From:  preForkSlot,
				To:    preForkSlot,
				Roles: []spectypes.BeaconRole{spectypes.BNRoleAttester},
			},
			wantErr: true,
		},
		{
			name:   "committee-backed role with indices is accepted",
			netCfg: preFork,
			request: &ValidatorTracesQuery{
				From:    preForkSlot,
				To:      preForkSlot,
				Roles:   []spectypes.BeaconRole{spectypes.BNRoleAttester},
				Indices: []phase0.ValidatorIndex{1},
			},
			wantErr: false,
		},
		{
			name:   "committee-backed role with pubkeys is accepted",
			netCfg: preFork,
			request: &ValidatorTracesQuery{
				From:    preForkSlot,
				To:      preForkSlot,
				Roles:   []spectypes.BeaconRole{spectypes.BNRoleAttester},
				PubKeys: []spectypes.ValidatorPK{{0x01}},
			},
			wantErr: false,
		},
		{
			name:   "non-committee role without filters is accepted",
			netCfg: preFork,
			request: &ValidatorTracesQuery{
				From:  preForkSlot,
				To:    preForkSlot,
				Roles: []spectypes.BeaconRole{spectypes.BNRoleProposer},
			},
			wantErr: false,
		},
		{
			name:   "AGGREGATOR without filters is accepted pre-fork",
			netCfg: preFork,
			request: &ValidatorTracesQuery{
				From:  preForkSlot,
				To:    preForkSlot,
				Roles: []spectypes.BeaconRole{spectypes.BNRoleAggregator},
			},
			wantErr: false,
		},
		{
			name:   "AGGREGATOR without filters is rejected post-fork",
			netCfg: postFork,
			request: &ValidatorTracesQuery{
				From:  postForkSlot,
				To:    postForkSlot,
				Roles: []spectypes.BeaconRole{spectypes.BNRoleAggregator},
			},
			wantErr: true,
		},
		{
			name:   "range straddling the fork resolves on the 'from' bound: pre-fork 'to' is accepted",
			netCfg: preFork,
			request: &ValidatorTracesQuery{
				From:  1,
				To:    preForkSlot,
				Roles: []spectypes.BeaconRole{spectypes.BNRoleAggregator},
			},
			wantErr: false,
		},
		{
			name:   "range straddling the fork resolves on the 'from' bound: post-fork 'to' is rejected",
			netCfg: postFork,
			request: &ValidatorTracesQuery{
				From:  1,
				To:    postForkSlot,
				Roles: []spectypes.BeaconRole{spectypes.BNRoleAggregator},
			},
			wantErr: true,
		},
		{
			name:   "range genuinely straddling the fork boundary is accepted (gate evaluated at 'from', not 'to')",
			netCfg: straddle,
			request: &ValidatorTracesQuery{
				From:  straddleBoundarySlot - 1,
				To:    straddleBoundarySlot + 10,
				Roles: []spectypes.BeaconRole{spectypes.BNRoleAggregator},
			},
			wantErr: false,
		},
		{
			name:   "range whose 'from' is already post-fork is rejected even if narrower than 'to'",
			netCfg: straddle,
			request: &ValidatorTracesQuery{
				From:  straddleBoundarySlot,
				To:    straddleBoundarySlot + 10,
				Roles: []spectypes.BeaconRole{spectypes.BNRoleAggregator},
			},
			wantErr: true,
		},
		{
			// the inclusive per-slot loop would wrap its uint64 counter at the
			// maximum 'to' and never terminate; the lower-bound fork gate no
			// longer rejects this shape for unfiltered committee-duty roles,
			// so validation must.
			name:   "'to' of max uint64 is rejected regardless of role or filters",
			netCfg: preFork,
			request: &ValidatorTracesQuery{
				From:  1,
				To:    math.MaxUint64,
				Roles: []spectypes.BeaconRole{spectypes.BNRoleProposer},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Exporter{networkConfig: tt.netCfg}
			err := e.validateValidatorRequest(tt.request)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// mockCoreTraceStore is a minimal dutyTraceStore implementation for exercising
// ValidatorTracesCore end-to-end over a fork-straddling slot range.
type mockCoreTraceStore struct {
	dutyTraceStore
}

func (m *mockCoreTraceStore) GetValidatorDuties(_ spectypes.BeaconRole, _ phase0.Slot) ([]*traces.ValidatorDutyTrace, error) {
	return nil, nil
}

func (m *mockCoreTraceStore) GetScheduled(_ phase0.Slot) (map[phase0.ValidatorIndex]rolemask.Mask, error) {
	return map[phase0.ValidatorIndex]rolemask.Mask{}, nil
}

func TestValidatorTracesCore_StraddlingFork(t *testing.T) {
	straddle := straddlingNetwork()
	boundarySlot := straddle.FirstSlotAtEpoch(straddle.SSV.Forks.Boole)

	e := &Exporter{
		traceStore:    &mockCoreTraceStore{},
		logger:        zap.NewNop(),
		networkConfig: straddle,
	}

	request := &ValidatorTracesQuery{
		From:  uint64(boundarySlot) - 1,
		To:    uint64(boundarySlot) + 1,
		Roles: []spectypes.BeaconRole{spectypes.BNRoleAggregator},
	}

	result, errs := e.ValidatorTracesCore(request)
	require.NotNil(t, result)

	// no *ValidationError: the request is accepted despite its tail crossing Boole.
	for _, err := range errs.Errors {
		var valErr *ValidationError
		assert.NotErrorAs(t, err, &valErr)
	}

	// the two post-fork slots (boundarySlot, boundarySlot+1) are reported as a
	// single aggregated non-fatal note per role since no pubkeys/indices were
	// supplied to filter the now-committee-backed AGGREGATOR duty.
	require.Len(t, errs.Errors, 1)
	assert.Contains(t, errs.Errors[0].Error(), fmt.Sprintf("slots %d-%d", boundarySlot, boundarySlot+1))
	assert.Contains(t, errs.Errors[0].Error(), "committee duty")
}

// mockValidatorTraceStore is a minimal dutyTraceStore implementation for
// exercising getValidatorCommitteeDutiesForRoleAndSlot's signer-bucket gating.
type mockValidatorTraceStore struct {
	dutyTraceStore
	committeeID spectypes.CommitteeID
	duty        *traces.CommitteeDutyTrace
	getDutyErr  error
	getIDErr    error
}

func (m *mockValidatorTraceStore) GetCommitteeID(_ phase0.Slot, _ phase0.ValidatorIndex) (spectypes.CommitteeID, error) {
	if m.getIDErr != nil {
		return spectypes.CommitteeID{}, m.getIDErr
	}
	return m.committeeID, nil
}

func (m *mockValidatorTraceStore) GetCommitteeDuty(_ phase0.Slot, _ spectypes.CommitteeID, _ spectypes.RunnerRole) (*traces.CommitteeDutyTrace, error) {
	if m.getDutyErr != nil {
		return nil, m.getDutyErr
	}
	return m.duty, nil
}

func TestGetValidatorCommitteeDutiesForRoleAndSlot(t *testing.T) {
	const slot = phase0.Slot(50)
	committeeID := spectypes.CommitteeID{0x01}
	memberIdx := phase0.ValidatorIndex(7)
	nonMemberIdx := phase0.ValidatorIndex(8)

	duty := &traces.CommitteeDutyTrace{
		Slot:          slot,
		CommitteeID:   committeeID,
		Attester:      []*traces.SignerData{{Signer: 1, ValidatorIdx: []phase0.ValidatorIndex{memberIdx}}},
		SyncCommittee: []*traces.SignerData{{Signer: 2, ValidatorIdx: []phase0.ValidatorIndex{memberIdx}}},
	}

	tests := []struct {
		name        string
		role        spectypes.BeaconRole
		indices     []phase0.ValidatorIndex
		wantCount   int
		wantErr     bool
		errContains string
	}{
		{
			name:      "ATTESTER uses the Attester signer bucket",
			role:      spectypes.BNRoleAttester,
			indices:   []phase0.ValidatorIndex{memberIdx},
			wantCount: 1,
		},
		{
			name:      "AGGREGATOR maps to the Attester signer bucket",
			role:      spectypes.BNRoleAggregator,
			indices:   []phase0.ValidatorIndex{memberIdx},
			wantCount: 1,
		},
		{
			name:      "SYNC_COMMITTEE uses the SyncCommittee signer bucket",
			role:      spectypes.BNRoleSyncCommittee,
			indices:   []phase0.ValidatorIndex{memberIdx},
			wantCount: 1,
		},
		{
			name:      "SYNC_COMMITTEE_CONTRIBUTION maps to the SyncCommittee signer bucket",
			role:      spectypes.BNRoleSyncCommitteeContribution,
			indices:   []phase0.ValidatorIndex{memberIdx},
			wantCount: 1,
		},
		{
			name:      "index absent from the bucket is excluded",
			role:      spectypes.BNRoleAttester,
			indices:   []phase0.ValidatorIndex{nonMemberIdx},
			wantCount: 0,
		},
		{
			name:        "non-committee-backed role fails fast",
			role:        spectypes.BNRoleProposer,
			indices:     []phase0.ValidatorIndex{memberIdx},
			wantErr:     true,
			errContains: "unexpected committee-backed beacon role",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &mockValidatorTraceStore{committeeID: committeeID, duty: duty}
			e := &Exporter{traceStore: store, logger: zap.NewNop()}

			results, err := e.getValidatorCommitteeDutiesForRoleAndSlot(tt.role, slot, tt.indices)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}
			require.NoError(t, err)
			assert.Len(t, results, tt.wantCount)
		})
	}
}

func TestGetValidatorCommitteeDutiesForRoleAndSlot_PropagatesLookupErrors(t *testing.T) {
	const slot = phase0.Slot(50)
	idx := phase0.ValidatorIndex(7)

	t.Run("committee ID lookup error is collected, not fatal", func(t *testing.T) {
		store := &mockValidatorTraceStore{getIDErr: estore.ErrNotFound}
		e := &Exporter{traceStore: store, logger: zap.NewNop()}

		results, err := e.getValidatorCommitteeDutiesForRoleAndSlot(spectypes.BNRoleAttester, slot, []phase0.ValidatorIndex{idx})

		require.Error(t, err)
		assert.Empty(t, results)
	})

	t.Run("committee duty lookup error is collected, not fatal", func(t *testing.T) {
		store := &mockValidatorTraceStore{committeeID: spectypes.CommitteeID{0x01}, getDutyErr: estore.ErrNotFound}
		e := &Exporter{traceStore: store, logger: zap.NewNop()}

		results, err := e.getValidatorCommitteeDutiesForRoleAndSlot(spectypes.BNRoleAttester, slot, []phase0.ValidatorIndex{idx})

		require.Error(t, err)
		assert.Empty(t, results)
	})
}

// TestCommitteeSignerBucketForBeaconRole_ExhaustiveRoles is a lightweight guard
// that the beacon-role -> signer-bucket mapping used by
// getValidatorCommitteeDutiesForRoleAndSlot stays in sync with
// CommitteeRunnerRoleForBeaconRole: every committee-backed beacon role must
// also resolve to a signer bucket.
func TestCommitteeSignerBucketForBeaconRole_ExhaustiveRoles(t *testing.T) {
	roles := []spectypes.BeaconRole{
		spectypes.BNRoleAttester,
		spectypes.BNRoleAggregator,
		spectypes.BNRoleSyncCommittee,
		spectypes.BNRoleSyncCommitteeContribution,
	}

	for _, role := range roles {
		_, isCommitteeBacked := ssvtypes.CommitteeRunnerRoleForBeaconRole(role)
		require.True(t, isCommitteeBacked, "role %s expected to be committee-backed", role.String())

		_, hasBucket := ssvtypes.CommitteeSignerBucketForBeaconRole(role)
		require.True(t, hasBucket, "role %s expected to have a signer bucket", role.String())
	}
}
