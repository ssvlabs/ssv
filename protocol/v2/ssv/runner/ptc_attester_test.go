package runner

import (
	"context"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// ptcTestBeacon embeds the spec testing beacon (so DomainData resolves) while stubbing the PTC surface: the
// observed payload-attestation data and a capture of the submitted messages.
type ptcTestBeacon struct {
	beacon.BeaconNode
	data      *gloas.PayloadAttestationData
	submitted []*gloas.PayloadAttestationMessage
}

func (b *ptcTestBeacon) PayloadAttestationData(context.Context, phase0.Slot) (*gloas.PayloadAttestationData, error) {
	return b.data, nil
}

func (b *ptcTestBeacon) SubmitPayloadAttestationMessages(_ context.Context, msgs []*gloas.PayloadAttestationMessage) error {
	b.submitted = append(b.submitted, msgs...)
	return nil
}

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
	require.Equal(t, []spectypes.HashRoot{data}, roots)
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

// A duplicate or past duty is rejected before it touches the running duty: the running duty's frozen
// observation stays, so the peers' partials for it still reconstruct.
func TestPTCAttesterRunner_RejectedDutyKeepsObservation(t *testing.T) {
	data := &gloas.PayloadAttestationData{BeaconBlockRoot: phase0.Root{0x01}, Slot: 9}
	r := &PTCAttesterRunner{
		BaseRunner:             &BaseRunner{RunnerRoleType: spectypes.RolePTCAttester},
		payloadAttestationData: data,
	}
	r.State = NewRunnerState(1, &spectypes.ValidatorDuty{Type: spectypes.BNRolePTCAttester, Slot: 9})

	for _, slot := range []phase0.Slot{9, 8} {
		duty := &spectypes.ValidatorDuty{Type: spectypes.BNRolePTCAttester, Slot: slot}
		require.Error(t, r.StartNewDuty(context.Background(), zap.NewNop(), duty, 1), "slot %d", slot)
		require.Same(t, data, r.payloadAttestationData, "slot %d", slot)
	}
}
