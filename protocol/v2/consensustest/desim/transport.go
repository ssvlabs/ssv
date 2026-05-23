package desim

import (
	"fmt"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// Mesh / gossip transport — shared by every protocol adapter.
//
// These four events plus two helpers model the eager-mesh fan-out and the
// gossipsub lazy-push (IHAVE/IWANT) backstop over a ct.MeshTopology. They are
// protocol-agnostic: the only protocol hook is Builder, a closure that
// constructs the protocol's arrival event for a recipient operator. Adapters
// publish via their own (byz-aware) emitMesh, which mints the first MeshArrival
// and calls CacheArrivalForGossip; the fan-out and gossip cadence run here.
//
// Layer is the per-layer bandwidth bucket (an OBFT layer or a QBFT round); it
// is -1 for messages with no layer (PSigs payloads, gossip control RPCs).

// MeshArrival is one (publisher, msgID, hop) eager-mesh delivery. On first
// arrival (deduped by MarkSeen) it delivers to the local protocol via Builder
// when To is a cluster op (relays only forward), then schedules a forward to
// every other neighbor — skipping the immediate sender and the original
// publisher, matching go-libp2p-pubsub's Publish (the dedup cache catches any
// remaining cycles).
type MeshArrival struct {
	From, To  ct.MeshNode
	Publisher ct.MeshNode
	MsgID     ct.MsgID
	Kind      ct.MsgKind
	Layer     int
	Bytes     int64
	Builder   func(to ct.OperatorID) Event
}

func (e *MeshArrival) Describe() string {
	return ct.FormatMeshArrival(e.From, e.To, e.Publisher, e.MsgID, e.Kind)
}

func (e *MeshArrival) Handle(h Host) []Scheduled {
	mesh := h.Mesh()
	// Dedup: MarkSeen returns false on a duplicate; libp2p drops it at this
	// point without forwarding (the dedup-cache hit short-circuits propagation).
	if !mesh.MarkSeen(e.To, e.MsgID) {
		return nil
	}
	// Stash a reinject closure on this receiver's mcache so it can answer
	// future IWANTs for this body (no-op when gossip is disabled).
	CacheArrivalForGossip(h, e.To, e.Publisher, e.MsgID, e.Kind, e.Layer, e.Bytes, e.Builder)
	var out []Scheduled
	if mesh.IsProtocol(e.To) {
		out = append(out, Scheduled{
			When: h.Now() + mesh.ValidateDelay(),
			Ev:   e.Builder(mesh.OperatorForNode(e.To)),
		})
	}
	fromEP := mesh.EndpointFor(e.To)
	for _, neighbor := range mesh.Neighbors(e.To) {
		if neighbor == e.From || neighbor == e.Publisher {
			continue
		}
		delay := mesh.SampleHopDelay(h.Rng(), fromEP, mesh.EndpointFor(neighbor), e.Kind)
		mesh.RecordMeshHop(h.Bandwidth(), e.To, neighbor, e.Kind, e.Layer, e.Bytes)
		out = append(out, Scheduled{
			When: h.Now() + mesh.ValidateDelay() + delay,
			Ev: &MeshArrival{
				From:      e.To,
				To:        neighbor,
				Publisher: e.Publisher,
				MsgID:     e.MsgID,
				Kind:      e.Kind,
				Layer:     e.Layer,
				Bytes:     e.Bytes,
				Builder:   e.Builder,
			},
		})
	}
	return out
}

// Gossipsub lazy-push backstop on top of the eager mesh:
//   - Each Mesh.Gossip().HeartbeatInterval, every node rotates its mcache and
//     sends a MeshIHave (advertising its recent mids) to a stable-random subset
//     of peers.
//   - A MeshIHave with unseen mids produces one MeshIWant back for them.
//   - A MeshIWant looks each mid up in the mcache and reinjects a fresh
//     MeshArrival for hits. IHAVE/IWANT ride single direct hops (not the mesh
//     chain), as real gossipsub does for control RPCs.

type MeshHeartbeat struct{ Node ct.MeshNode }

func (e *MeshHeartbeat) Describe() string {
	return fmt.Sprintf("MeshHeartbeat[node=%d]", e.Node)
}

func (e *MeshHeartbeat) Handle(h Host) []Scheduled {
	mesh := h.Mesh()
	g := mesh.Gossip()
	// Rotate first so the evicted slot is the oldest and the IHAVE window read
	// picks up only mids still within the HistoryGossip retention budget.
	mesh.MCacheRotate(e.Node)
	mids := mesh.MCacheGossipMids(e.Node, g.HistoryGossip)
	if len(mids) == 0 {
		return nil
	}
	var out []Scheduled
	fromEP := mesh.EndpointFor(e.Node)
	for _, to := range mesh.PickGossipRecipients(h.Rng(), e.Node, g.Dlazy, g.GossipFactor) {
		toEP := mesh.EndpointFor(to)
		delay := h.Network().Delay(h.Rng(), fromEP, toEP, ct.KindGossipIHave)
		bytes := ct.GossipRPCSize(len(mids))
		mesh.RecordMeshHop(h.Bandwidth(), e.Node, to, ct.KindGossipIHave, -1, bytes)
		// Defensive copy: PickGossipRecipients reuses its pool slice across
		// iterations; hold a stable view of what was advertised at heartbeat time.
		advertised := append([]ct.MsgID(nil), mids...)
		out = append(out, Scheduled{
			When: h.Now() + delay,
			Ev:   &MeshIHave{From: e.Node, To: to, Mids: advertised},
		})
	}
	return out
}

type MeshIHave struct {
	From, To ct.MeshNode
	Mids     []ct.MsgID
}

func (e *MeshIHave) Describe() string {
	return fmt.Sprintf("MeshIHave[from=%d to=%d mids=%d]", e.From, e.To, len(e.Mids))
}

func (e *MeshIHave) Handle(h Host) []Scheduled {
	mesh := h.Mesh()
	var want []ct.MsgID
	for _, mid := range e.Mids {
		if mesh.IsSeen(e.To, mid) {
			continue
		}
		want = append(want, mid)
	}
	if len(want) == 0 {
		return nil
	}
	fromEP := mesh.EndpointFor(e.To)
	toEP := mesh.EndpointFor(e.From)
	delay := h.Network().Delay(h.Rng(), fromEP, toEP, ct.KindGossipIWant)
	bytes := ct.GossipRPCSize(len(want))
	mesh.RecordMeshHop(h.Bandwidth(), e.To, e.From, ct.KindGossipIWant, -1, bytes)
	return []Scheduled{{
		When: h.Now() + delay,
		Ev:   &MeshIWant{From: e.To, To: e.From, Mids: want},
	}}
}

type MeshIWant struct {
	From, To ct.MeshNode
	Mids     []ct.MsgID
}

func (e *MeshIWant) Describe() string {
	return fmt.Sprintf("MeshIWant[from=%d to=%d mids=%d]", e.From, e.To, len(e.Mids))
}

func (e *MeshIWant) Handle(h Host) []Scheduled {
	mesh := h.Mesh()
	for _, mid := range e.Mids {
		entry, ok := mesh.MCacheLookup(e.To, mid)
		if !ok {
			// Real gossipsub silently drops requests for missing mids (the
			// requester hears the mid from another peer next heartbeat).
			continue
		}
		// The reinject closure schedules a fresh MeshArrival directly on the
		// engine via the captured Host; we return no events ourselves.
		entry.Reinject(e.From)
	}
	return nil
}

// CacheArrivalForGossip stashes a reinject closure on cacheOwner's mcache so it
// can answer IWANT for msgID until eviction. Called by an adapter's emitMesh at
// the publisher (cacheOwner == publisher) and by MeshArrival.Handle at each
// receiver (publisher carried over). No-op when gossip is disabled — the
// closure allocation is skipped entirely on the eager-only path. The closure
// preserves publisher + payload so an IWANT-response MeshArrival is
// indistinguishable from a fresh eager-mesh hop downstream.
func CacheArrivalForGossip(h Host, cacheOwner, publisher ct.MeshNode, msgID ct.MsgID, kind ct.MsgKind, layer int, bytes int64, builder func(to ct.OperatorID) Event) {
	mesh := h.Mesh()
	g := mesh.Gossip()
	if !g.Enabled {
		return
	}
	mesh.MCacheInsert(cacheOwner, msgID, ct.MCacheEntry{
		Kind:  kind,
		Bytes: bytes,
		Reinject: func(requester ct.MeshNode) {
			respEP := mesh.EndpointFor(cacheOwner)
			reqEP := mesh.EndpointFor(requester)
			delay := h.Network().Delay(h.Rng(), respEP, reqEP, kind)
			mesh.RecordMeshHop(h.Bandwidth(), cacheOwner, requester, kind, layer, bytes)
			h.Schedule(h.Now()+delay, &MeshArrival{
				From:      cacheOwner,
				To:        requester,
				Publisher: publisher,
				MsgID:     msgID,
				Kind:      kind,
				Layer:     layer,
				Bytes:     bytes,
				Builder:   builder,
			})
		},
	}, g.HistoryLength)
}

// ScheduleInitialHeartbeats pre-schedules MeshHeartbeat events across every mesh
// node when gossip is enabled, anchored at sim time 0 with a per-node phase
// offset (node × HeartbeatInterval / TotalNodes) so heartbeats don't all fire
// at once. Ticks past relayCutoff are skipped (the slot's decision is moot by
// then). No-op when gossip is disabled or in DeliveryDirect (mesh == nil).
// Pre-scheduling (vs self-rescheduling) keeps the queue finite-by-construction.
func ScheduleInitialHeartbeats(h Host, relayCutoff time.Duration) {
	mesh := h.Mesh()
	if mesh == nil {
		return
	}
	g := mesh.Gossip()
	if !g.Enabled || g.HeartbeatInterval <= 0 {
		return
	}
	total := mesh.TotalNodes()
	if total <= 0 {
		return
	}
	phase := g.HeartbeatInterval / time.Duration(total)
	for i := 0; i < total; i++ {
		nodeOffset := time.Duration(i) * phase
		for tick := time.Duration(0); ; tick += g.HeartbeatInterval {
			at := nodeOffset + tick
			if at > relayCutoff {
				break
			}
			h.Schedule(at, &MeshHeartbeat{Node: ct.MeshNode(i)})
		}
	}
}
