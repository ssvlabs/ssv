package params

import (
	"math"

	"github.com/ssvlabs/ssv/registry/storage"
)

// Ethereum parameters
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

// Expected number of messages per duty step

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

// Expected number of committee duties per epoch due to attestations
func expectedNumberOfCommitteeDutiesPerEpochDueToAttestation(numValidators int) float64 {
	k := float64(numValidators)
	n := SlotsPerEpoch

	// Probability that all validators are not assigned to slot i
	probabilityAllNotOnSlotI := math.Pow((n-1)/n, k)
	// Probability that at least one validator is assigned to slot i
	probabilityAtLeastOneOnSlotI := 1 - probabilityAllNotOnSlotI
	// Expected value for duty existence ({0,1}) on slot i
	expectedDutyExistenceOnSlotI := 0*probabilityAllNotOnSlotI + 1*probabilityAtLeastOneOnSlotI
	// Expected number of duties per epoch
	expectedNumberOfDutiesPerEpoch := n * expectedDutyExistenceOnSlotI

	return expectedNumberOfDutiesPerEpoch
}

// Expected committee duties per epoch that are due to only sync committee beacon duties
func expectedSingleSCCommitteeDutiesPerEpoch(numValidators int) float64 {
	// Probability that a validator is not in sync committee
	chanceOfNotBeingInSyncCommittee := 1.0 - SyncCommitteeProbability
	// Probability that all validators are not in sync committee
	chanceThatAllValidatorsAreNotInSyncCommittee := math.Pow(chanceOfNotBeingInSyncCommittee, float64(numValidators))
	// Probability that at least one validator is in sync committee
	chanceOfAtLeastOneValidatorBeingInSyncCommittee := 1.0 - chanceThatAllValidatorsAreNotInSyncCommittee

	// Expected number of slots with no attestation duty
	expectedSlotsWithNoDuty := 32.0 - expectedNumberOfCommitteeDutiesPerEpochDueToAttestationCached(numValidators)

	// Expected number of committee duties per epoch created due to only sync committee duties
	return chanceOfAtLeastOneValidatorBeingInSyncCommittee * expectedSlotsWithNoDuty
}

// Cache costly calculations

func generateCachedValues(generator func(int) float64, threshold int) []float64 {
	results := make([]float64, 0, threshold)

	for i := 0; i < threshold; i++ {
		results = append(results, generator(i))
	}

	return results
}

var generatedExpectedNumberOfCommitteeDutiesPerEpochDueToAttestation = generateCachedValues(expectedNumberOfCommitteeDutiesPerEpochDueToAttestation, MaxValidatorsPerCommittee)

func expectedNumberOfCommitteeDutiesPerEpochDueToAttestationCached(numValidators int) float64 {
	// If the committee has more validators than our computed cache, we return the limit value
	if numValidators >= MaxValidatorsPerCommittee {
		return MaxAttestationDutiesPerEpochForCommittee
	}

	return generatedExpectedNumberOfCommitteeDutiesPerEpochDueToAttestation[numValidators]
}

var generatedExpectedSingleSCCommitteeDutiesPerEpoch = generateCachedValues(expectedSingleSCCommitteeDutiesPerEpoch, MaxValidatorsPerCommittee)

func expectedSingleSCCommitteeDutiesPerEpochCached(numValidators int) float64 {
	// If the committee has more validators than our computed cache, we return the limit value
	if numValidators >= MaxValidatorsPerCommittee {
		return SingleSCDutiesLimit
	}

	return generatedExpectedSingleSCCommitteeDutiesPerEpoch[numValidators]
}

// Calculates the message rate for a topic given its committees' configurations (number of operators and number of validators)
func calculateMessageRateForTopic(committees []*storage.Committee) float64 {
	if len(committees) == 0 {
		return 0
	}

	totalMsgRate := 0.0

	for _, committee := range committees {
		committeeSize := len(committee.Operators)
		numValidators := len(committee.Validators)

		totalMsgRate += expectedNumberOfCommitteeDutiesPerEpochDueToAttestationCached(numValidators) * float64(dutyWithoutPreConsensus(committeeSize))
		totalMsgRate += expectedSingleSCCommitteeDutiesPerEpochCached(numValidators) * float64(dutyWithoutPreConsensus(committeeSize))
		totalMsgRate += float64(numValidators) * AggregatorProbability * float64(dutyWithPreConsensus(committeeSize))
		totalMsgRate += float64(numValidators) * SlotsPerEpoch * ProposalProbability * float64(dutyWithPreConsensus(committeeSize))
		totalMsgRate += float64(numValidators) * SlotsPerEpoch * SyncCommitteeAggProb * float64(dutyWithPreConsensus(committeeSize))
	}

	// Convert rate to seconds
	totalEpochSeconds := float64(SlotsPerEpoch * 12)
	totalMsgRate = totalMsgRate / totalEpochSeconds

	return totalMsgRate
}
