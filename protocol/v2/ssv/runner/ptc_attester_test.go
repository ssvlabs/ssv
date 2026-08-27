package runner

import (
	"context"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

func TestNewPTCAttesterRunner_RequiresSingleShare(t *testing.T) {
	_, err := NewPTCAttesterRunner(PTCAttesterRunnerOptions{})
	require.Error(t, err)

	r, err := NewPTCAttesterRunner(PTCAttesterRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{
			Share: map[phase0.ValidatorIndex]*spectypes.Share{0: {}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, spectypes.RolePTCAttester, r.(*PTCAttesterRunner).RunnerRoleType)
}

// The runner validates and aggregates incoming partial signatures against its own frozen
// observation: there is no expected root before executeDuty has observed and frozen one, and
// afterwards it is exactly that observation's root under DomainPTCAttester.
func TestPTCAttesterRunner_ExpectedPreConsensusRootsAndDomain(t *testing.T) {
	r := &PTCAttesterRunner{}

	_, _, err := r.expectedPreConsensusRootsAndDomain()
	require.Error(t, err)

	data := &gloas.PayloadAttestationData{BeaconBlockRoot: phase0.Root{0x01}, Slot: 5, PayloadPresent: true}
	r.payloadAttestationData = data
	roots, domain, err := r.expectedPreConsensusRootsAndDomain()
	require.NoError(t, err)
	require.Equal(t, []ssz.HashRoot{data}, roots)
	require.Equal(t, phase0.DomainType(spectypes.DomainPTCAttester), domain)
}

// PTC has no consensus or post-consensus phase; those entry points must reject.
func TestPTCAttesterRunner_NoConsensusPhases(t *testing.T) {
	r := &PTCAttesterRunner{}
	require.Error(t, r.ProcessConsensus(context.Background(), zap.NewNop(), nil))
	require.Error(t, r.ProcessPostConsensus(context.Background(), zap.NewNop(), nil))
}

// executeDuty abstains (markDutyNotRequired, no observation frozen, no signing) when the beacon node
// reports no block for the slot — surfaced either as nil data (a 204 No Content) or, defensively, a
// 200 with an all-zero BeaconBlockRoot.
func TestPTCAttesterRunner_ExecuteDutyAbstains(t *testing.T) {
	for _, tc := range []struct {
		name string
		data *gloas.PayloadAttestationData
	}{
		{"nil data (204 no block)", nil},
		{"zero beacon block root", &gloas.PayloadAttestationData{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			bn := beacon.NewMockBeaconNode(ctrl)
			bn.EXPECT().PayloadAttestationData(gomock.Any(), phase0.Slot(9)).Return(tc.data, nil)

			r := &PTCAttesterRunner{
				BaseRunner: &BaseRunner{RunnerRoleType: spectypes.RolePTCAttester},
				beacon:     bn,
			}
			duty := &spectypes.ValidatorDuty{Type: spectypes.BNRolePTCAttester, Slot: 9}
			r.State = NewRunnerState(1, duty)

			require.NoError(t, r.executeDuty(context.Background(), zap.NewNop(), duty))
			require.True(t, r.State.Succeeded, "abstains via markDutyNotRequired")
			require.Nil(t, r.payloadAttestationData, "abstaining freezes no observation")
		})
	}
}
