package params

import (
	"math"
)

const (
	EthereumValidators                       = 1000000.0 // TODO: get from network?
	SyncCommitteeSize                        = 512.0     // TODO: get from network?
	EstimatedAttestationCommitteeSize        = EthereumValidators / 2048.0
	AggregatorProbability                    = 16.0 / EstimatedAttestationCommitteeSize
	ProposalProbability                      = 1.0 / EthereumValidators
	SyncCommitteeProbability                 = SyncCommitteeSize / EthereumValidators
	SyncCommitteeAggProb                     = SyncCommitteeProbability * 16.0 / (SyncCommitteeSize / 4.0)
	MaxValidatorsPerCommittee                = 560.0
	SlotsPerEpoch                            = 32.0 // TODO: get from network?
	MaxAttestationDutiesPerEpochForCommittee = SlotsPerEpoch
	SingleSCDutiesLimit                      = 0
)

func consensusMessages(n int) int {
	return 1 + n + n + 2 // 1 Proposal + n Prepares + n Commits + 2 Decideds (average)
}

func partialSignatureMessages(n int) int {
	return n
}

func dutyWithPreConsensus(n int) int {
	// Pre-Consensus + Consensus + Post-Consensus
	return partialSignatureMessages(n) + consensusMessages(n) + partialSignatureMessages(n)
}

func dutyWithoutPreConsensus(n int) int {
	// Consensus + Post-Consensus
	return consensusMessages(n) + partialSignatureMessages(n)
}

func expectedCommitteeDutiesPerEpochDueToAttestation(numValidators int) float64 {
	k := float64(numValidators)
	n := SlotsPerEpoch
	return n * (1 - math.Pow((n-1)/n, k))
}

func expectedCommitteeDutiesPerEpochDueToAttestationCached(numValidators int) float64 {
	if numValidators >= MaxValidatorsPerCommittee {
		return MaxAttestationDutiesPerEpochForCommittee
	}

	return generatedExpectedCommitteeDutiesPerEpochDueToAttestation[numValidators]
}

var generatedExpectedCommitteeDutiesPerEpochDueToAttestation = generateCachedValues(expectedCommitteeDutiesPerEpochDueToAttestation, MaxValidatorsPerCommittee)

func expectedSingleSCCommitteeDutiesPerEpoch(numValidators int) float64 {
	chanceOfNotBeingInSyncCommittee := 1.0 - SyncCommitteeProbability
	chanceThatAllValidatorsAreNotInSyncCommittee := math.Pow(chanceOfNotBeingInSyncCommittee, float64(numValidators))
	chanceOfAtLeastOneValidatorBeingInSyncCommittee := 1.0 - chanceThatAllValidatorsAreNotInSyncCommittee

	expectedSlotsWithNoDuty := 32.0 - expectedCommitteeDutiesPerEpochDueToAttestationCached(numValidators)

	return chanceOfAtLeastOneValidatorBeingInSyncCommittee * expectedSlotsWithNoDuty
}

func expectedSingleSCCommitteeDutiesPerEpochCached(numValidators int) float64 {
	if numValidators >= MaxValidatorsPerCommittee {
		return SingleSCDutiesLimit
	}

	return generatedExpectedSingleSCCommitteeDutiesPerEpoch[numValidators]
}

var generatedExpectedSingleSCCommitteeDutiesPerEpoch = generateCachedValues(expectedSingleSCCommitteeDutiesPerEpoch, MaxValidatorsPerCommittee)

func generateCachedValues(generator func(int) float64, threshold int) []float64 {
	results := make([]float64, 0, threshold)

	for i := 0; i < threshold; i++ {
		results = append(results, generator(i))
	}

	return results
}

// message rate is the number of messages per epoch because of the context of gossipsub score
func calcMsgRateForTopic(committeeSizes []int, validatorCounts []int) float64 {
	if len(committeeSizes) != len(validatorCounts) {
		panic("committee sizes and validator counts are not equal")
	}

	totalMsgRate := 0.0

	for i, count := range validatorCounts {
		committeeSize := committeeSizes[i]

		totalMsgRate += expectedCommitteeDutiesPerEpochDueToAttestationCached(count) * float64(dutyWithoutPreConsensus(committeeSize))
		totalMsgRate += expectedSingleSCCommitteeDutiesPerEpochCached(count) * float64(dutyWithoutPreConsensus(committeeSize))
		totalMsgRate += float64(count) * AggregatorProbability * float64(dutyWithPreConsensus(committeeSize))
		totalMsgRate += float64(count) * SlotsPerEpoch * ProposalProbability * float64(dutyWithPreConsensus(committeeSize))
		totalMsgRate += float64(count) * SlotsPerEpoch * SyncCommitteeAggProb * float64(dutyWithPreConsensus(committeeSize))
	}

	return totalMsgRate
}
