package validator

import (
	"context"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/ibft/proto"
	beacon2 "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
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
		role                        beacon2.RoleType
		expectedAttestationDataByts []byte
		overrideAttestationData     *spec.AttestationData
		expectedError               string
	}{
		{
			"valid consensus",
			true,
			3,
			beacon2.RoleTypeAttester,
			marshalInputValueStructForAttestation(t, refAttestationDataByts),
			nil,
			"",
		},
		{
			"not decided",
			false,
			3,
			beacon2.RoleTypeAttester,
			refAttestationDataByts,
			nil,
			"instance did not decide",
		},
		{
			"non supported role",
			false,
			3,
			beacon2.RoleTypeUnknown,
			refAttestationDataByts,
			nil,
			"no ibft for this role [UNKNOWN]",
		},
		{
			"non supported role",
			false,
			3,
			beacon2.RoleTypeUnknown,
			refAttestationDataByts,
			nil,
			"no ibft for this role [UNKNOWN]",
		},
		{
			"invalid value pre-check",
			false,
			3,
			beacon2.RoleTypeAttester,
			refAttestationDataByts,
			&spec.AttestationData{
				Slot: 100,
			},
			"input value failed pre-consensus check: TEST - failed on slot 100",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			identifier := _byteArray("6139636633363061613135666231643164333065653262353738646335383834383233633139363631383836616538623839323737356363623362643936623764373334353536396132616130623134653464303135633534613661306335345f4154544553544552")
			node := testingValidator(t, test.decided, test.signaturesCount, identifier)

			if test.overrideAttestationData != nil {
				node.beacon.(*testBeacon).refAttestationData = test.overrideAttestationData
			}

			duty := &beacon2.Duty{
				Type:                    test.role,
				PubKey:                  spec.BLSPubKey{},
				Slot:                    0,
				ValidatorIndex:          0,
				CommitteeIndex:          0,
				CommitteeLength:         0,
				CommitteesAtSlot:        0,
				ValidatorCommitteeIndex: 0,
			}

			signaturesCount, decidedByts, _, err := node.comeToConsensusOnInputValue(node.logger, duty)
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
			identifier := _byteArray("6139636633363061613135666231643164333065653262353738646335383834383233633139363631383836616538623839323737356363623362643936623764373334353536396132616130623134653464303135633534613661306335345f4154544553544552")
			validator := testingValidator(t, true, test.expectedSignaturesCount, identifier)
			// wait for for listeners to spin up
			time.Sleep(time.Millisecond * 100)

			duty := &beacon2.Duty{
				Type:                    beacon2.RoleTypeAttester,
				PubKey:                  spec.BLSPubKey{},
				Slot:                    0,
				ValidatorIndex:          0,
				CommitteeIndex:          0,
				CommitteeLength:         0,
				CommitteesAtSlot:        0,
				ValidatorCommitteeIndex: 0,
			}

			pk := &bls.PublicKey{}
			err := pk.Deserialize(refPk)
			require.NoError(t, err)

			// send sigs
			for index, sig := range test.sigs {
				err := validator.network.BroadcastSignature(nil, &proto.SignedMessage{
					Message: &proto.Message{
						Lambda:    validator.ibfts[beacon2.RoleTypeAttester].GetIdentifier(),
						SeqNumber: 0,
					},
					Signature: sig,
					SignerIds: []uint64{index},
				})
				require.NoError(t, err)
			}

			err = validator.postConsensusDutyExecution(context.Background(), validator.logger, 0, test.expectedAttestationDataByts, test.expectedSignaturesCount, duty)
			if len(test.expectedError) > 0 {
				require.EqualError(t, err, test.expectedError)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, test.expectedReconstructedSig, validator.beacon.(*testBeacon).LastSubmittedAttestation.Signature[:])
			}
		})
	}
}
