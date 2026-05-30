package operator

import (
	"context"

	"github.com/ssvlabs/ssv/beacon/goclient"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

// beaconClient is the beacon-node surface that assemble()/start() consume: the duty-call
// interface plus the Healthy probe used to register the client as a health-prober component.
// run() builds a *goclient.GoClient (which additionally exposes BeaconConfig(), used only by
// run() when constructing networkConfig) and passes it in as this interface, which lets an
// in-process smoke test inject a stub instead of a real beacon connection.
type beaconClient interface {
	beaconprotocol.BeaconNode
	Healthy(context.Context) error
}

// Compile-time assertion that the real beacon client satisfies the injected interface.
var _ beaconClient = (*goclient.GoClient)(nil)
