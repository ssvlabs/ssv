package metadata

import (
	"testing"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

func TestDetectValidatorStateChanges(t *testing.T) {
	testCases := []struct {
		name           string
		sharesBefore   []*ssvtypes.SSVShare
		sharesAfter    []*ssvtypes.SSVShare
		expectedAttest int
		expectedSlash  int
		expectedExit   int
	}{
		{
			name: "Validator becomes attesting",
			sharesBefore: []*ssvtypes.SSVShare{
				{
					Share:  spectypes.Share{Committee: []*spectypes.ShareMember{{Signer: 1}}, ValidatorPubKey: spectypes.ValidatorPK{0x01}},
					Status: eth2apiv1.ValidatorStateUnknown,
				},
			},
			sharesAfter: []*ssvtypes.SSVShare{
				{
					Share:  spectypes.Share{Committee: []*spectypes.ShareMember{{Signer: 1}}, ValidatorPubKey: spectypes.ValidatorPK{0x01}},
					Status: eth2apiv1.ValidatorStateActiveOngoing,
				},
			},
			expectedAttest: 1,
			expectedSlash:  0,
			expectedExit:   0,
		},
		{
			name: "Validator gets slashed",
			sharesBefore: []*ssvtypes.SSVShare{
				{
					Share:  spectypes.Share{Committee: []*spectypes.ShareMember{{Signer: 1}}, ValidatorPubKey: spectypes.ValidatorPK{0x02}},
					Status: eth2apiv1.ValidatorStateActiveOngoing,
				},
			},
			sharesAfter: []*ssvtypes.SSVShare{
				{
					Share:  spectypes.Share{Committee: []*spectypes.ShareMember{{Signer: 1}}, ValidatorPubKey: spectypes.ValidatorPK{0x02}},
					Status: eth2apiv1.ValidatorStateActiveSlashed,
				},
			},
			expectedAttest: 0,
			expectedSlash:  1,
			expectedExit:   0,
		},
		{
			name: "Validator exits",
			sharesBefore: []*ssvtypes.SSVShare{
				{
					Share:  spectypes.Share{Committee: []*spectypes.ShareMember{{Signer: 1}}, ValidatorPubKey: spectypes.ValidatorPK{0x03}},
					Status: eth2apiv1.ValidatorStateActiveOngoing,
				},
			},
			sharesAfter: []*ssvtypes.SSVShare{
				{
					Share:  spectypes.Share{Committee: []*spectypes.ShareMember{{Signer: 1}}, ValidatorPubKey: spectypes.ValidatorPK{0x03}},
					Status: eth2apiv1.ValidatorStateExitedUnslashed,
				},
			},
			expectedAttest: 0,
			expectedSlash:  0,
			expectedExit:   1,
		},
		{
			name: "No state change",
			sharesBefore: []*ssvtypes.SSVShare{
				{
					Share:  spectypes.Share{Committee: []*spectypes.ShareMember{{Signer: 1}}, ValidatorPubKey: spectypes.ValidatorPK{0x04}},
					Status: eth2apiv1.ValidatorStateActiveOngoing,
				},
			},
			sharesAfter: []*ssvtypes.SSVShare{
				{
					Share:  spectypes.Share{Committee: []*spectypes.ShareMember{{Signer: 1}}, ValidatorPubKey: spectypes.ValidatorPK{0x04}},
					Status: eth2apiv1.ValidatorStateActiveOngoing,
				},
			},
			expectedAttest: 0,
			expectedSlash:  0,
			expectedExit:   0,
		},
		{
			name: "multiple validators: all become attesting",
			sharesBefore: []*ssvtypes.SSVShare{
				{
					Share:  spectypes.Share{Committee: []*spectypes.ShareMember{{Signer: 1}}, ValidatorPubKey: spectypes.ValidatorPK{0x01}},
					Status: eth2apiv1.ValidatorStateUnknown,
				},
				{
					Share:  spectypes.Share{Committee: []*spectypes.ShareMember{{Signer: 1}}, ValidatorPubKey: spectypes.ValidatorPK{0x02}},
					Status: eth2apiv1.ValidatorStateUnknown,
				},
			},
			sharesAfter: []*ssvtypes.SSVShare{
				{
					Share:  spectypes.Share{Committee: []*spectypes.ShareMember{{Signer: 1}}, ValidatorPubKey: spectypes.ValidatorPK{0x01}},
					Status: eth2apiv1.ValidatorStateActiveOngoing,
				},
				{
					Share:  spectypes.Share{Committee: []*spectypes.ShareMember{{Signer: 1}}, ValidatorPubKey: spectypes.ValidatorPK{0x02}},
					Status: eth2apiv1.ValidatorStateActiveOngoing,
				},
			},
			expectedAttest: 2,
			expectedSlash:  0,
			expectedExit:   0,
		},
		{
			name: "multiple validators: one become attesting",
			sharesBefore: []*ssvtypes.SSVShare{
				{
					Share:  spectypes.Share{Committee: []*spectypes.ShareMember{{Signer: 1}}, ValidatorPubKey: spectypes.ValidatorPK{0x01}},
					Status: eth2apiv1.ValidatorStateUnknown,
				},
				{
					Share:  spectypes.Share{Committee: []*spectypes.ShareMember{{Signer: 1}}, ValidatorPubKey: spectypes.ValidatorPK{0x02}},
					Status: eth2apiv1.ValidatorStateUnknown,
				},
			},
			sharesAfter: []*ssvtypes.SSVShare{
				{
					Share:  spectypes.Share{Committee: []*spectypes.ShareMember{{Signer: 1}}, ValidatorPubKey: spectypes.ValidatorPK{0x01}},
					Status: eth2apiv1.ValidatorStateActiveOngoing,
				},
				{
					Share:  spectypes.Share{Committee: []*spectypes.ShareMember{{Signer: 1}}, ValidatorPubKey: spectypes.ValidatorPK{0x02}},
					Status: eth2apiv1.ValidatorStateUnknown,
				},
			},
			expectedAttest: 1,
			expectedSlash:  0,
			expectedExit:   0,
		},
	}

	operatorID := spectypes.OperatorID(1)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			syncBatch := SyncBatch{
				SharesBefore: tc.sharesBefore,
				SharesAfter:  tc.sharesAfter,
				Epoch:        phase0.Epoch(0),
			}

			attesting, slashed, exited := syncBatch.DetectValidatorStateChanges(operatorID)

			require.Len(t, attesting, tc.expectedAttest, "unexpected attesting count")
			require.Len(t, slashed, tc.expectedSlash, "unexpected slashed count")
			require.Len(t, exited, tc.expectedExit, "unexpected exited count")
		})
	}
}
