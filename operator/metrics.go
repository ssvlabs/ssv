package operator

import (
	"time"

	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/operator/duties"
)

type nodeMetrics interface {
	SSVNodeHealthy()
	SSVNodeNotHealthy()
	OperatorPublicKey(spectypes.OperatorID, []byte)
	duties.Metrics
}

type nopMetrics struct{}

func (n nopMetrics) SSVNodeHealthy()                                {}
func (n nopMetrics) SSVNodeNotHealthy()                             {}
func (n nopMetrics) OperatorPublicKey(spectypes.OperatorID, []byte) {}
func (n nopMetrics) SlotDelay(time.Duration)                        {}
