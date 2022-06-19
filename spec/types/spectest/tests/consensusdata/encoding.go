package consensusdata

import (
	"github.com/bloxapp/ssv/spec/types"
	"github.com/bloxapp/ssv/spec/types/spectest/tests"
	"github.com/bloxapp/ssv/spec/types/testingutils"
)

// Encoding tests encoding of a ConsensusData struct
func Encoding() *tests.EncodingSpecTest {
	data := &types.ConsensusData{
		Duty:                   testingutils.TestingAttesterDuty,
		AttestationData:        testingutils.TestingAttestationData,
		BlockData:              testingutils.TestingBeaconBlock,
		AggregateAndProof:      testingutils.TestingAggregateAndProof,
		SyncCommitteeBlockRoot: testingutils.TestingSyncCommitteeBlockRoot,
		SyncCommitteeContribution: types.ContributionsMap{
			testingutils.TestingContributionProofsSigned[0]: testingutils.TestingSyncCommitteeContributions[0],
			testingutils.TestingContributionProofsSigned[1]: testingutils.TestingSyncCommitteeContributions[1],
			testingutils.TestingContributionProofsSigned[2]: testingutils.TestingSyncCommitteeContributions[2],
		},
	}

	byts, err := data.Encode()
	if err != nil {
		panic(err.Error())
	}

	return &tests.EncodingSpecTest{
		Name:     "encoding ConsensusData",
		DataType: tests.ConsensusDataType,
		Data:     byts,
	}
}
