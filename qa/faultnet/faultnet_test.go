package faultnet

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/network"
	operatorvalidator "github.com/ssvlabs/ssv/operator/validator"
	"github.com/ssvlabs/ssv/qa/faults"
)

// Confirms the wrapper satisfies both the generic P2P network facade and the narrower interface
// the validator controller actually depends on — by embedding network.P2PNetwork rather than by
// hand-writing every passthrough method.
var (
	_ network.P2PNetwork           = (*Network)(nil)
	_ operatorvalidator.P2PNetwork = (*Network)(nil)
)

func TestWrapReturnsInnerUnchangedWhenNoFault(t *testing.T) {
	faults.SetForTest(t, faults.None)

	var inner network.P2PNetwork // the identity path never touches inner, so nil stands in for it
	out := Wrap(inner, nil, nil, nil)

	require.Nil(t, out)
	_, wrapped := out.(*Network)
	require.False(t, wrapped, "FAULT=none must return the raw inner network, not the decorator")
}

func TestWrapReturnsDecoratorWhenFaultActive(t *testing.T) {
	faults.SetForTest(t, faults.TwoEntries)

	var inner network.P2PNetwork
	out := Wrap(inner, nil, nil, nil)

	_, wrapped := out.(*Network)
	require.True(t, wrapped, "an active fault must produce the decorator")
}
