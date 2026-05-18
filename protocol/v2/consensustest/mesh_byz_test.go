package consensustest_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
)

// TestMeshByz_PublishTimeOnly_RefloodBypassesByzGate pins the mesh
// transport's publish-time-only byz semantic: per-(from, to) byz
// primitives apply only at the publisher's emit step, not at reflood
// hops. A receiver the byz publisher refuses to deliver to directly
// still gets the message via a neighbor's unconditional reflood one
// extra hop later.
//
// This is why the catalog opts adversarial scenarios (HV1SelectiveDelivery,
// LateLeaderBroadcast, MeshFlakiness, …) into DeliveryDirect — their
// invariants rely on per-(from, to) suppression staying intact, which
// reflood would undo. A future change that started honoring byz gates
// at reflood hops would silently make those scenarios meaningful under
// mesh and break the catalog's transport-mode contract. We sense this
// here by running a per-(from, to)-suppressing byz pattern under
// DeliveryMesh with tracing on and asserting at least one MeshArrival
// to a byz-suppressed receiver came from a non-publisher source.
func TestMeshByz_PublishTimeOnly_RefloodBypassesByzGate(t *testing.T) {
	btt := 200 * time.Millisecond
	cfg := ct.DefaultProposerDutyConfig(btt)
	cfg.Byz = ct.ByzPattern{
		Kind:         ct.ByzHV1SelectiveDelivery,
		ByzOperators: []ct.OperatorID{1},
		Recipients:   []ct.OperatorID{2}, // op1's direct V delivery: only op2; op3/op4 byz-suppressed
	}
	cfg.Delivery = ct.DeliveryMesh
	cfg.TraceEnabled = true

	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)

	// MeshNode indices: op_i ↔ MeshNode(i-1). Suppressed receivers
	// op3, op4 → MeshNode 2, 3. Publisher op1 → MeshNode 0. A reflood
	// arrival to a suppressed receiver has `to ∈ {2, 3}` AND `from != publisher`.
	var refloodToSuppressed bool
	for _, e := range out.Trace {
		from, to, publisher, ok := ct.ParseMeshArrivalTrace(e.Event)
		if !ok {
			continue
		}
		if from != publisher && (to == 2 || to == 3) {
			refloodToSuppressed = true
			break
		}
	}
	require.True(t, refloodToSuppressed,
		"DeliveryMesh: byz-suppressed receivers (op3, op4) must receive at least one MeshArrival via reflood (from != publisher) — pins the publish-time-only byz semantic")
}
