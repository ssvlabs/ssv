package operator

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
)

type nodeMetrics interface {
	SSVNodeHealthy()
	SSVNodeNotHealthy()
	OperatorPublicKey(spectypes.OperatorID, []byte)
}

type nopMetrics struct{}

func (n nopMetrics) SSVNodeHealthy()                                {}
func (n nopMetrics) SSVNodeNotHealthy()                             {}
func (n nopMetrics) OperatorPublicKey(spectypes.OperatorID, []byte) {}
