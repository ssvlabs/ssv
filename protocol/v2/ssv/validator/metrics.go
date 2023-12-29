package validator

import (
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v2/queue/worker"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
)

type Metrics interface {
	ValidatorInactive(publicKey []byte)
	ValidatorNoIndex(publicKey []byte)
	ValidatorError(publicKey []byte)
	ValidatorReady(publicKey []byte)
	ValidatorNotActivated(publicKey []byte)
	ValidatorExiting(publicKey []byte)
	ValidatorSlashed(publicKey []byte)
	ValidatorNotFound(publicKey []byte)
	ValidatorPending(publicKey []byte)
	ValidatorRemoved(publicKey []byte)
	ValidatorUnknown(publicKey []byte)

	queue.Metrics
	worker.Metrics
	runner.Metrics
	instance.Metrics
}

type NopMetrics struct{}

func (n NopMetrics) ValidatorInactive([]byte)                                                  {}
func (n NopMetrics) ValidatorNoIndex([]byte)                                                   {}
func (n NopMetrics) ValidatorError([]byte)                                                     {}
func (n NopMetrics) ValidatorReady([]byte)                                                     {}
func (n NopMetrics) ValidatorNotActivated([]byte)                                              {}
func (n NopMetrics) ValidatorExiting([]byte)                                                   {}
func (n NopMetrics) ValidatorSlashed([]byte)                                                   {}
func (n NopMetrics) ValidatorNotFound([]byte)                                                  {}
func (n NopMetrics) ValidatorPending([]byte)                                                   {}
func (n NopMetrics) ValidatorRemoved([]byte)                                                   {}
func (n NopMetrics) ValidatorUnknown([]byte)                                                   {}
func (n NopMetrics) DroppedQueueMessage(spectypes.MessageID)                                   {}
func (n NopMetrics) WorkerProcessedMessage(string)                                             {}
func (n NopMetrics) ConsensusDutySubmission(role spectypes.BeaconRole, duration time.Duration) {}
func (n NopMetrics) ConsensusDuration(role spectypes.BeaconRole, duration time.Duration)       {}
func (n NopMetrics) PreConsensusDuration(role spectypes.BeaconRole, duration time.Duration)    {}
func (n NopMetrics) PostConsensusDuration(role spectypes.BeaconRole, duration time.Duration)   {}
func (n NopMetrics) DutyFullFlowDuration(role spectypes.BeaconRole, duration time.Duration)    {}
func (n NopMetrics) DutyFullFlowFirstRoundDuration(role spectypes.BeaconRole, duration time.Duration) {
}
func (n NopMetrics) RoleSubmitted(role spectypes.BeaconRole)                   {}
func (n NopMetrics) RoleSubmissionFailure(role spectypes.BeaconRole)           {}
func (n NopMetrics) InstanceStarted(role spectypes.BeaconRole)                 {}
func (n NopMetrics) InstanceDecided(role spectypes.BeaconRole)                 {}
func (n NopMetrics) QBFTProposalDuration(duration time.Duration)               {}
func (n NopMetrics) QBFTPrepareDuration(duration time.Duration)                {}
func (n NopMetrics) QBFTCommitDuration(duration time.Duration)                 {}
func (n NopMetrics) QBFTRound(msgID spectypes.MessageID, round specqbft.Round) {}
