package validator

import (
	"context"
	"encoding/json"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/herumi/bls-eth-go-binary/bls"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func marshalInputValueStructForAttestation(t *testing.T, attByts []byte) []byte {
	attData := &ethpb.AttestationData{}
	require.NoError(t, attData.Unmarshal(attByts))

	iv := &proto.InputValue_Attestation{Attestation: &ethpb.Attestation{
		Data: attData,
	}}
	ret, err := json.Marshal(iv)
	require.NoError(t, err)
	return ret
}

func TestConsensusOnInputValue(t *testing.T) {
	tests := []struct {
		name                        string
		decided                     bool
		signaturesCount             int
		role                        beacon.Role
		expectedAttestationDataByts []byte
		expectedError               string
	}{
		{
			"valid consensus",
			true,
			3,
			beacon.RoleAttester,
			marshalInputValueStructForAttestation(t, refAttestationDataByts),
			"",
		},
		{
			"not decided",
			false,
			3,
			beacon.RoleAttester,
			refAttestationDataByts,
			"ibft did not decide, not executing role",
		},
		{
			"non supported role",
			false,
			3,
			beacon.RoleAggregator,
			refAttestationDataByts,
			"unknown role: AGGREGATOR",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := testingValidator(t, test.decided, test.signaturesCount)
			duty := &ethpb.DutiesResponse_Duty{
				Committee:      nil,
				CommitteeIndex: 0,
				AttesterSlot:   0,
				ProposerSlots:  nil,
				PublicKey:      nil,
				Status:         0,
				ValidatorIndex: 0,
			}

			signaturesCount, decidedByts, _, err := node.comeToConsensusOnInputValue(context.Background(), node.logger, 0, test.role, duty)
			if !test.decided {
				require.EqualError(t, err, test.expectedError)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, 3, signaturesCount)
			require.NotNil(t, decidedByts)

			require.EqualValues(t, test.expectedAttestationDataByts, decidedByts)
		})
	}
}

func TestPostConsensusSignatureAndAggregation(t *testing.T) {
	tests := []struct {
		name                        string
		sigs                        map[uint64][]byte
		expectedSignaturesCount     int
		expectedAttestationDataByts []byte
		expectedReconstructedSig    []byte
		expectedError               string
	}{
		{
			"valid 4/4",
			map[uint64][]byte{
				1: refAttestationSplitSigs[0],
				2: refAttestationSplitSigs[1],
				3: refAttestationSplitSigs[2],
				4: refAttestationSplitSigs[3],
			},
			4,
			refAttestationDataByts,
			refAttestationSig,
			"",
		},
		{
			"valid 3/4",
			map[uint64][]byte{
				1: refAttestationSplitSigs[0],
				2: refAttestationSplitSigs[1],
				3: refAttestationSplitSigs[2],
			},
			3,
			refAttestationDataByts,
			refAttestationSig,
			"",
		},
		{
			"invalid 3/4",
			map[uint64][]byte{
				1: refAttestationSplitSigs[0],
				2: refAttestationSplitSigs[0],
				3: refAttestationSplitSigs[2],
			},
			3,
			refAttestationDataByts,
			refAttestationSig,
			"timed out waiting for post consensus signatures, received 2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			validator := testingValidator(t, true, test.expectedSignaturesCount)
			// wait for for listeners to spin up
			time.Sleep(time.Millisecond * 100)

			// construct value
			attData := &ethpb.AttestationData{}
			require.NoError(t, attData.Unmarshal(test.expectedAttestationDataByts))

			// marshal to decidedValue
			d := &proto.InputValue_Attestation{
				Attestation: &ethpb.Attestation{
					Data: attData,
				},
			}
			decidedValue, err := json.Marshal(d)
			require.NoError(t, err)

			duty := &ethpb.DutiesResponse_Duty{
				Committee:      nil,
				CommitteeIndex: 0,
				AttesterSlot:   0,
				ProposerSlots:  nil,
				PublicKey:      nil,
				Status:         0,
				ValidatorIndex: 0,
			}

			pk := &bls.PublicKey{}
			err = pk.Deserialize(refPk)
			require.NoError(t, err)

			// send sigs
			for index, sig := range test.sigs {
				err := validator.network.BroadcastSignature(&proto.SignedMessage{
					Message: &proto.Message{
						Lambda:      []byte("id"),
						ValidatorPk: pk.Serialize(),
					},
					Signature: sig,
					SignerIds: []uint64{index},
				})
				require.NoError(t, err)
			}

			err = validator.postConsensusDutyExecution(context.Background(), validator.logger, []byte("id"), decidedValue, test.expectedSignaturesCount, beacon.RoleAttester, duty)
			if len(test.expectedError) > 0 {
				require.EqualError(t, err, test.expectedError)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, test.expectedReconstructedSig, validator.beacon.(*testBeacon).LastSubmittedAttestation.GetSignature())
			}
		})
	}
}
