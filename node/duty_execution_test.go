package node

import (
	"context"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft/proto"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestConsensusOnInputValue(t *testing.T) {
	tests := []struct{
		name string
		decided bool
		signaturesCount int
		expectedAttestationDataByts []byte
		expectedError string
	}{
		{
			"valid consensus",
			true,
			3,
			refAttestationDataByts,
			"",
		},
		{
			"not decided",
			false,
			3,
			refAttestationDataByts,
			"ibft did not decide, not executing role",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := testingSSVNode(test.decided, test.signaturesCount)
			signaturesCount, inputValue, err := node.comeToConsensusOnInputValue(context.Background(), node.logger, []byte("id"), 0, beacon.RoleAttester, &ethpb.DutiesResponse_Duty{
				Committee:            nil,
				CommitteeIndex:       0,
				AttesterSlot:         0,
				ProposerSlots:        nil,
				PublicKey:            nil,
				Status:               0,
				ValidatorIndex:       0,
			})
			if !test.decided {
				require.EqualError(t, err, test.expectedError)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, 3, signaturesCount)
			require.NotNil(t, inputValue)

			byts, err := inputValue.GetAttestationData().Marshal()
			require.NoError(t, err)
			require.EqualValues(t, test.expectedAttestationDataByts, byts)
		})
	}
}

func TestPostConsensusSignatureAndAggregation(t *testing.T) {
	tests := []struct{
		name string
		sigs [][]byte
		expectedAttestationDataByts []byte
		expectedError string
	}{
		{

			"valid",
			refAttestationSigs,
			refAttestationDataByts,
			"",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			signaturesCount := len(test.sigs)
			node := testingSSVNode(true, signaturesCount)


			// construct value
			attData := &ethpb.AttestationData{}
			require.NoError(t, attData.Unmarshal(test.expectedAttestationDataByts))
			inputValue := &proto.InputValue{
				Data:       &proto.InputValue_AttestationData{
					AttestationData: attData,
				},
				SignedData: nil,
			}

			duty := &ethpb.DutiesResponse_Duty{
				Committee:            nil,
				CommitteeIndex:       0,
				AttesterSlot:         0,
				ProposerSlots:        nil,
				PublicKey:            nil,
				Status:               0,
				ValidatorIndex:       0,
			}

			// received sigs
			go func() {
				ticker := time.NewTicker(300 * time.Millisecond)
				<-ticker.C
				for index, sig := range test.sigs {
					node.network.BroadcastSignature([]byte{}, map[uint64][]byte{
						uint64(index+1): sig,
					})
				}
			}()

			require.NoError(t, node.postConsensusDutyExecution(context.Background(), node.logger, []byte("id"), inputValue, signaturesCount, beacon.RoleAttester, duty))
			require.EqualValues(t, refAttestationSig, node.beacon.(*testBeacon).LastSubmittedAttestation.GetSignature())
		})
	}
}