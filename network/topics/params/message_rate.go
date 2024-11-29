package params

import (
	"math"
	"time"

	networkconfig "github.com/ssvlabs/ssv/network/config"
	"github.com/ssvlabs/ssv/registry/storage"
)

// Ethereum parameters
const (
	SingleSCDutiesLimit = 0
)

type rateCalculator struct {
	netCfg                                                           networkconfig.NetworkConfig
	generatedExpectedNumberOfCommitteeDutiesPerEpochDueToAttestation []float64
	generatedExpectedSingleSCCommitteeDutiesPerEpoch                 []float64
}

func newRateCalculator(netCfg networkconfig.NetworkConfig) *rateCalculator {
	rc := &rateCalculator{
		netCfg: netCfg,
		generatedExpectedNumberOfCommitteeDutiesPerEpochDueToAttestation: []float64{},
		generatedExpectedSingleSCCommitteeDutiesPerEpoch:                 []float64{},
	}

	rc.generateCachedValues()

	return rc
}

// Calculates the message rate for a topic given its committees' configurations (number of operators and number of validators)
func (rc *rateCalculator) calculateMessageRateForTopic(committees []*storage.Committee) float64 {
	if len(committees) == 0 {
		return 0
	}

	totalMsgRate := 0.0

	for _, committee := range committees {
		committeeSize := len(committee.Operators)
		numValidators := len(committee.Validators)

		totalMsgRate += rc.expectedNumberOfCommitteeDutiesPerEpochDueToAttestationCached(numValidators) * float64(dutyWithoutPreConsensus(committeeSize))
		totalMsgRate += rc.expectedSingleSCCommitteeDutiesPerEpochCached(numValidators) * float64(dutyWithoutPreConsensus(committeeSize))
		totalMsgRate += float64(numValidators) * rc.AggregatorProbability() * float64(dutyWithPreConsensus(committeeSize))
		totalMsgRate += float64(numValidators) * float64(rc.netCfg.SlotsPerEpoch()) * rc.ProposalProbability() * float64(dutyWithPreConsensus(committeeSize))
		totalMsgRate += float64(numValidators) * float64(rc.netCfg.SlotsPerEpoch()) * rc.SyncCommitteeAggProb() * float64(dutyWithPreConsensus(committeeSize))
	}

	// Convert rate to seconds
	totalEpochSeconds := float64(rc.netCfg.EpochDuration() / time.Second)
	totalMsgRate = totalMsgRate / totalEpochSeconds

	return totalMsgRate
}

// Expected number of committee duties per epoch due to attestations
func (rc *rateCalculator) calcExpectedNumberOfCommitteeDutiesPerEpochDueToAttestation(numValidators int) float64 {
	k := float64(numValidators)
	n := float64(rc.netCfg.SlotsPerEpoch())

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
func (rc *rateCalculator) calcExpectedSingleSCCommitteeDutiesPerEpoch(numValidators int) float64 {
	// Probability that a validator is not in sync committee
	chanceOfNotBeingInSyncCommittee := 1.0 - rc.SyncCommitteeProbability()
	// Probability that all validators are not in sync committee
	chanceThatAllValidatorsAreNotInSyncCommittee := math.Pow(chanceOfNotBeingInSyncCommittee, float64(numValidators))
	// Probability that at least one validator is in sync committee
	chanceOfAtLeastOneValidatorBeingInSyncCommittee := 1.0 - chanceThatAllValidatorsAreNotInSyncCommittee

	// Expected number of slots with no attestation duty
	expectedSlotsWithNoDuty := 32.0 - rc.calcExpectedNumberOfCommitteeDutiesPerEpochDueToAttestation(numValidators)

	// Expected number of committee duties per epoch created due to only sync committee duties
	return chanceOfAtLeastOneValidatorBeingInSyncCommittee * expectedSlotsWithNoDuty
}

func (rc *rateCalculator) generateCachedValues() {
	// Cache costly calculations

	expectedCommNumber := make([]float64, 0, rc.netCfg.SSV.MaxValidatorsPerCommittee)
	expectedSingleSCC := make([]float64, 0, rc.netCfg.SSV.MaxValidatorsPerCommittee)

	for i := 0; i < rc.netCfg.SSV.MaxValidatorsPerCommittee; i++ {
		expectedCommNumber = append(expectedCommNumber, rc.calcExpectedNumberOfCommitteeDutiesPerEpochDueToAttestation(i))
		expectedSingleSCC = append(expectedSingleSCC, rc.calcExpectedSingleSCCommitteeDutiesPerEpoch(i))
	}

	rc.generatedExpectedNumberOfCommitteeDutiesPerEpochDueToAttestation = expectedCommNumber
	rc.generatedExpectedSingleSCCommitteeDutiesPerEpoch = expectedSingleSCC
}

func (rc *rateCalculator) expectedNumberOfCommitteeDutiesPerEpochDueToAttestationCached(numValidators int) float64 {
	// If the committee has more validators than our computed cache, we return the limit value
	if numValidators >= rc.netCfg.SSV.MaxValidatorsPerCommittee {
		return float64(rc.MaxAttestationDutiesPerEpochForCommittee())
	}

	return rc.generatedExpectedNumberOfCommitteeDutiesPerEpochDueToAttestation[numValidators]
}

func (rc *rateCalculator) expectedSingleSCCommitteeDutiesPerEpochCached(numValidators int) float64 {
	// If the committee has more validators than our computed cache, we return the limit value
	if numValidators >= rc.netCfg.SSV.MaxValidatorsPerCommittee {
		return SingleSCDutiesLimit
	}

	return rc.generatedExpectedSingleSCCommitteeDutiesPerEpoch[numValidators]
}

func (rc *rateCalculator) AggregatorProbability() float64 {
	return 16.0 / rc.EstimatedAttestationCommitteeSize()
}

func (rc *rateCalculator) ProposalProbability() float64 {
	return 1.0 / float64(rc.netCfg.TotalEthereumValidators)
}

func (rc *rateCalculator) SyncCommitteeProbability() float64 {
	return float64(rc.netCfg.SyncCommitteeSize) / float64(rc.netCfg.TotalEthereumValidators)
}

func (rc *rateCalculator) SyncCommitteeAggProb() float64 {
	return rc.SyncCommitteeProbability() * 16.0 / (float64(rc.netCfg.SyncCommitteeSize) / 4.0)
}

func (rc *rateCalculator) MaxAttestationDutiesPerEpochForCommittee() uint64 {
	return uint64(rc.netCfg.SlotsPerEpoch())
}

func (rc *rateCalculator) EstimatedAttestationCommitteeSize() float64 {
	return float64(rc.netCfg.TotalEthereumValidators) / 2048.0
}

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
