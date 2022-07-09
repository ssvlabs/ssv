package validator

import (
	"testing"
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
)

func marshalInputValueStructForAttestation(t *testing.T, attByts []byte) []byte {
	ret := &testBeacon{}
	ret.refAttestationData = &spec.AttestationData{}
	err := ret.refAttestationData.UnmarshalSSZ(attByts) // ignore error
	require.NoError(t, err)
	val, err := ret.refAttestationData.MarshalSSZ()
	require.NoError(t, err)
	return val
}

func TestConsensusOnInputValue(t *testing.T) {
	tests := []struct {
		name                        string
		decided                     bool
		signaturesCount             int
		role                        message.RoleType
		expectedAttestationDataByts []byte
		overrideAttestationData     *spec.AttestationData
		expectedError               string
	}{
		{
			"valid consensus",
			true,
			3,
			message.RoleTypeAttester,
			marshalInputValueStructForAttestation(t, refAttestationDataByts),
			nil,
			"",
		},
		{
			"not decided",
			false,
			3,
			message.RoleTypeAttester,
			refAttestationDataByts,
			nil,
			"instance did not decide",
		},
		{
			"non supported role",
			false,
			3,
			message.RoleTypeUnknown,
			refAttestationDataByts,
			nil,
			"no ibft for this role [UNKNOWN]",
		},
		{
			"non supported role",
			false,
			3,
			message.RoleTypeUnknown,
			refAttestationDataByts,
			nil,
			"no ibft for this role [UNKNOWN]",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			identifier := _byteArray("6139636633363061613135666231643164333065653262353738646335383834383233633139363631383836616538623839323737356363623362643936623764373334353536396132616130623134653464303135633534613661306335345f4154544553544552")
			node := testingValidator(t, test.decided, test.signaturesCount, identifier)

			if test.overrideAttestationData != nil {
				node.beacon.(*testBeacon).refAttestationData = test.overrideAttestationData
			}

			duty := &beacon.Duty{
				Type:                    test.role,
				PubKey:                  spec.BLSPubKey{},
				Slot:                    0,
				ValidatorIndex:          0,
				CommitteeIndex:          0,
				CommitteeLength:         0,
				CommitteesAtSlot:        0,
				ValidatorCommitteeIndex: 0,
			}

			_, signaturesCount, decidedByts, _, err := node.comeToConsensusOnInputValue(node.logger, duty)
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
		sigs                        map[message.OperatorID][]byte
		expectedSignaturesCount     int
		expectedAttestationDataByts []byte
		expectedReconstructedSig    []byte
		expectedRootHash            []byte
		expectedError               string
	}{
		//{
		//	"valid 4/4",
		//	map[message.OperatorID][]byte{
		//		1: refAttestationSplitSigs[0],
		//		2: refAttestationSplitSigs[1],
		//		3: refAttestationSplitSigs[2],
		//		4: refAttestationSplitSigs[3],
		//	},
		//	4,
		//	refAttestationDataByts,
		//	refAttestationSig,
		//	refSigRoot,
		//	"",
		//},
		{
			"valid 3/4",
			map[message.OperatorID][]byte{
				1: refAttestationSplitSigs[0],
				3: refAttestationSplitSigs[2],
				4: refAttestationSplitSigs[3],
			},
			3,
			refAttestationDataByts,
			refAttestationSig,
			refSigRoot,
			"",
		},
		//{
		//	"invalid 3/4",
		//	map[message.OperatorID][]byte{
		//		1: refAttestationSplitSigs[0],
		//		2: refAttestationSplitSigs[0],
		//		3: refAttestationSplitSigs[2],
		//	},
		//	3,
		//	refAttestationDataByts,
		//	refAttestationSig,
		//	"not enough post consensus signatures, received 2",
		//},
	}

	for _, test := range tests {
		t.Skip()
		t.Run(test.name, func(t *testing.T) {
			identifier := _byteArray("801721bc17ed1f0ac1b9b7fc1c45af611d3b3d019ab0f63a38b002650e7758959cfca5cdeb26fa6c6aefce99a73339c201000000")
			validator := testingValidator(t, true, test.expectedSignaturesCount, identifier)
			// wait for for listeners to spin up
			time.Sleep(time.Millisecond * 100)

			duty := &beacon.Duty{
				Type:                    message.RoleTypeAttester,
				PubKey:                  spec.BLSPubKey{},
				Slot:                    0,
				ValidatorIndex:          0,
				CommitteeIndex:          0,
				CommitteeLength:         0,
				CommitteesAtSlot:        0,
				ValidatorCommitteeIndex: 0,
			}

			// send sigs
			for index, sig := range test.sigs {
				signedMessage := &message.SignedMessage{
					Message: &message.ConsensusMessage{
						Identifier: validator.ibfts[message.RoleTypeAttester].GetIdentifier(),
						Height:     0,
					},
					Signature: sig,
					Signers:   []message.OperatorID{index},
				}

				encodedMsg, err := signedMessage.Encode()
				require.NoError(t, err)
				ssvMsg := message.SSVMessage{
					MsgType: message.SSVConsensusMsgType,
					ID:      identifier,
					Data:    encodedMsg,
				}

				require.NoError(t, validator.p2pNetwork.Broadcast(ssvMsg))
				for _, ibft := range validator.Ibfts() {
					require.NoError(t, ibft.ProcessMsg(&ssvMsg))
				}
			}

			// TODO: do for all ibfts
			for _, ibft := range validator.Ibfts() {
				err := ibft.PostConsensusDutyExecution(validator.logger, 0, test.expectedAttestationDataByts, test.expectedSignaturesCount, duty)
				if len(test.expectedError) > 0 {
					require.EqualError(t, err, test.expectedError)
				} else {
					require.NoError(t, err)
					require.EqualValues(t, test.expectedReconstructedSig, ibft.(*testIBFT).beacon.(*testBeacon).LastSubmittedAttestation.Signature[:])
				}
			}
		})
	}
}
