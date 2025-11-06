package duties

import (
	"context"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"
)

// noopExecutor implements DutyExecutor but performs no action.
type noopExecutor struct{}

func NewNoopExecutor() *noopExecutor { return &noopExecutor{} }

func (n *noopExecutor) ExecuteDuty(ctx context.Context, logger *zap.Logger, duty *spectypes.ValidatorDuty) {
}

func (n *noopExecutor) ExecuteCommitteeDuty(ctx context.Context, logger *zap.Logger, _ spectypes.CommitteeID, _ *spectypes.CommitteeDuty) {
}

func (n *noopExecutor) ExecuteAggregatorCommitteeDuty(ctx context.Context, _ spectypes.CommitteeID, _ *spectypes.AggregatorCommitteeDuty) {
}

// Ensure interface conformance.
var _ DutyExecutor = (*noopExecutor)(nil)
