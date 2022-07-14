package validator

import (
	"testing"
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/protocol/v1/message"
)

func marshalInputValueStructForAttestation(t *testing.T, attByts []byte) []byte {
	ret := &TestBeacon{}
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
		role                        spectypes.BeaconRole
		expectedAttestationDataByts []byte
		overrideAttestationData     *spec.AttestationData
		expectedError               string
	}{
		{
			"valid consensus",
			true,
			3,
			spectypes.BNRoleAttester,
			marshalInputValueStructForAttestation(t, refAttestationDataByts),
			nil,
			"",
		},
		{
			"not decided",
			false,
			3,
			spectypes.BNRoleAttester,
			refAttestationDataByts,
			nil,
			"instance did not decide",
		},
		{
			"non supported role",
			false,
			3,
			spectypes.BeaconRole(-1),
			refAttestationDataByts,
			nil,
			"no ibft for this role [UNDEFINED]",
		},
		{
			"non supported role",
			false,
			3,
			spectypes.BeaconRole(-1),
			refAttestationDataByts,
			nil,
			"no ibft for this role [UNDEFINED]",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			identifier := _byteArray("6139636633363061613135666231643164333065653262353738646335383834383233633139363631383836616538623839323737356363623362643936623764373334353536396132616130623134653464303135633534613661306335345f4154544553544552")
			node := testingValidator(t, test.decided, test.signaturesCount, identifier)

			if test.overrideAttestationData != nil {
				node.beacon.(*TestBeacon).refAttestationData = test.overrideAttestationData
			}

			duty := &spectypes.Duty{
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
			consensusData := &spectypes.ConsensusData{}
			require.NoError(t, consensusData.Decode(decidedByts))
			// TODO(olegshmuelov): use SSZ decoding
			//require.EqualValues(t, test.expectedAttestationDataByts, consensusData.AttestationData.MarshalSSZ())
		})
	}
}

func TestPostConsensusSignatureAndAggregation(t *testing.T) {
	tests := []struct {
		name                        string
		sigs                        map[spectypes.OperatorID][]byte
		expectedSignaturesCount     int
		expectedAttestationDataByts []byte
		expectedReconstructedSig    []byte
		expectedError               string
	}{
		{
			"valid 4/4",
			map[spectypes.OperatorID][]byte{
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
			map[spectypes.OperatorID][]byte{
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
			map[spectypes.OperatorID][]byte{
				1: refAttestationSplitSigs[0],
				2: refAttestationSplitSigs[0],
				3: refAttestationSplitSigs[2],
			},
			3,
			refAttestationDataByts,
			refAttestationSig,
			"not enough post consensus signatures, received 2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			identifier := _byteArray("6139636633363061613135666231643164333065653262353738646335383834383233633139363631383836616538623839323737356363623362643936623764373334353536396132616130623134653464303135633534613661306335345f4154544553544552")
			validator := testingValidator(t, true, test.expectedSignaturesCount, identifier)
			// wait for for listeners to spin up
			time.Sleep(time.Millisecond * 100)

			duty := &spectypes.Duty{
				Type:                    spectypes.BNRoleAttester,
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
				signedMessage := &specqbft.SignedMessage{
					Message: &specqbft.Message{
						Identifier: validator.ibfts[spectypes.BNRoleAttester].GetIdentifier(),
						Height:     0,
					},
					Signature: sig,
					Signers:   []spectypes.OperatorID{index},
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
					require.EqualValues(t, test.expectedReconstructedSig, ibft.(*testIBFT).beacon.(*TestBeacon).LastSubmittedAttestation.Signature[:])
				}
			}
		})
	}
}
