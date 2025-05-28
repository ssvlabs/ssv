package metadata

import (
	"testing"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

func TestDetectValidatorStateChanges(t *testing.T) {
	testCases := []struct {
		name                    string
		metadataBefore          beacon.ValidatorMetadataMap
		metadataAfter           beacon.ValidatorMetadataMap
		expectedEligibleToStart int
		expectedSlashedCount    int
		expectedExitedCount     int
	}{
		{
			name: "Validator becomes eligible to start",
			metadataBefore: beacon.ValidatorMetadataMap{
				spectypes.ValidatorPK{0x01}: {
					Status: eth2apiv1.ValidatorStateUnknown,
				},
			},
			metadataAfter: beacon.ValidatorMetadataMap{
				spectypes.ValidatorPK{0x01}: {
					Index:  1,
					Status: eth2apiv1.ValidatorStateActiveOngoing,
				},
			},
			expectedEligibleToStart: 1,
			expectedSlashedCount:    0,
			expectedExitedCount:     0,
		},
		{
			name: "Validator gets slashed",
			metadataBefore: beacon.ValidatorMetadataMap{
				spectypes.ValidatorPK{0x02}: {
					Index:  2,
					Status: eth2apiv1.ValidatorStateActiveOngoing,
				},
			},
			metadataAfter: beacon.ValidatorMetadataMap{
				spectypes.ValidatorPK{0x02}: {
					Index:  2,
					Status: eth2apiv1.ValidatorStateActiveSlashed,
				},
			},
			expectedEligibleToStart: 0,
			expectedSlashedCount:    1,
			expectedExitedCount:     0,
		},
		{
			name: "Validator exits",
			metadataBefore: beacon.ValidatorMetadataMap{
				spectypes.ValidatorPK{0x03}: {
					Index:  3,
					Status: eth2apiv1.ValidatorStateActiveOngoing,
				},
			},
			metadataAfter: beacon.ValidatorMetadataMap{
				spectypes.ValidatorPK{0x03}: {
					Index:  3,
					Status: eth2apiv1.ValidatorStateExitedUnslashed,
				},
			},
			expectedEligibleToStart: 0,
			expectedSlashedCount:    0,
			expectedExitedCount:     1,
		},
		{
			name: "No state change",
			metadataBefore: beacon.ValidatorMetadataMap{
				spectypes.ValidatorPK{0x04}: {
					Index:  4,
					Status: eth2apiv1.ValidatorStateActiveOngoing,
				},
			},
			metadataAfter: beacon.ValidatorMetadataMap{
				spectypes.ValidatorPK{0x04}: {
					Index:  4,
					Status: eth2apiv1.ValidatorStateActiveOngoing,
				},
			},
			expectedEligibleToStart: 0,
			expectedSlashedCount:    0,
			expectedExitedCount:     0,
		},
		{
			name: "Validator transitions from pending to active",
			metadataBefore: beacon.ValidatorMetadataMap{
				spectypes.ValidatorPK{0x05}: {
					Index:  5,
					Status: eth2apiv1.ValidatorStatePendingQueued,
				},
			},
			metadataAfter: beacon.ValidatorMetadataMap{
				spectypes.ValidatorPK{0x05}: {
					Index:  5,
					Status: eth2apiv1.ValidatorStateActiveOngoing,
				},
			},
			expectedEligibleToStart: 0,
			expectedSlashedCount:    0,
			expectedExitedCount:     0,
		},

		{
			name: "multiple validators: all become eligible to start",
			metadataBefore: beacon.ValidatorMetadataMap{
				spectypes.ValidatorPK{0x01}: {
					Status: eth2apiv1.ValidatorStateUnknown,
				},
				spectypes.ValidatorPK{0x02}: {
					Status: eth2apiv1.ValidatorStateUnknown,
				},
			},
			metadataAfter: beacon.ValidatorMetadataMap{
				spectypes.ValidatorPK{0x01}: {
					Index:  1,
					Status: eth2apiv1.ValidatorStateActiveOngoing,
				},
				spectypes.ValidatorPK{0x02}: {
					Index:  2,
					Status: eth2apiv1.ValidatorStateActiveOngoing,
				},
			},
			expectedEligibleToStart: 2,
			expectedSlashedCount:    0,
			expectedExitedCount:     0,
		},
		{
			name: "multiple validators: one become eligible to start",
			metadataBefore: beacon.ValidatorMetadataMap{
				spectypes.ValidatorPK{0x01}: {
					Status: eth2apiv1.ValidatorStateUnknown,
				},
				spectypes.ValidatorPK{0x02}: {
					Status: eth2apiv1.ValidatorStateUnknown,
				},
			},
			metadataAfter: beacon.ValidatorMetadataMap{
				spectypes.ValidatorPK{0x01}: {
					Index:  1,
					Status: eth2apiv1.ValidatorStateActiveOngoing,
				},
				spectypes.ValidatorPK{0x02}: {
					Status: eth2apiv1.ValidatorStateUnknown,
				},
			},
			expectedEligibleToStart: 1,
			expectedSlashedCount:    0,
			expectedExitedCount:     0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			syncBatch := SyncBatch{
				Before: tc.metadataBefore,
				After:  tc.metadataAfter,
			}

			eligibleToStart, slashed, exited := syncBatch.DetectValidatorStateChanges()

			require.Len(t, eligibleToStart, tc.expectedEligibleToStart, "unexpected eligible to start count")
			require.Len(t, slashed, tc.expectedSlashedCount, "unexpected slashed count")
			require.Len(t, exited, tc.expectedExitedCount, "unexpected exited count")
		})
	}
}
