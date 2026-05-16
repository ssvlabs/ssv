package consensustest

import (
	"fmt"
	"math"
	mrand "math/rand"
	"regexp"
	"strconv"
	"time"
)

// DeliveryMode selects the framework's transport model for a sim. DeliveryDirect
// preserves the original full-fanout semantics — every emitted message
// schedules one arrival per cluster peer with per-pair Network delays + byz
// per-(from, to) primitives. DeliveryMesh replaces fanout with a small-world
// mesh: cluster peers + relay-only peers connected at a fixed degree, with
// dedup + reflood at each hop. Per-(from, to) byz primitives apply only at
// the publish step in mesh mode — reflood through neighbors will still
// deliver to a wire-suppressed receiver one hop later. Adversarial scenarios
// should stay on DeliveryDirect; the catalog opt-in defaults reflect that.
type DeliveryMode int

const (
	DeliveryDirect DeliveryMode = iota
	DeliveryMesh
)

// MsgID is the per-sim unique identifier used for mesh-mode dedup. Assigned
// at publish time from a per-sim atomic counter on MeshTopology; uniqueness
// is required only within a single sim (the counter resets per sim).
type MsgID uint64

// MeshNode is a 0-based mesh-node index. The first n indices are protocol
// peers (cluster operators); the remaining r are relay peers (forward-only,
// modeling non-cluster libp2p nodes participating in the same subnet).
type MeshNode int

// MeshConfig carries the per-sim mesh tunables. Wiring constants
// (ProtocolMeshDegree=1, RelayMeshDegreePerOp=2, RelayPoolSize=N, plus the
// relay-relay edge count chosen to give every node total degree 3) are
// hard-coded in NewMeshTopology — not exposed because the user is not
// expected to fiddle with them.
type MeshConfig struct {
	// HopDelay samples a per-hop propagation delay for one mesh edge.
	// Calibration target ("mesh as realism"): the convolution over the
	// average mesh diameter ≈ today's cluster-wide BTT envelope. Called
	// with mesh-endpoint OperatorIDs — cluster ops use their real IDs,
	// relay peers use synthetic IDs allocated from RelayEndpointBase
	// onward (so stateful NetworkModel impls key per-link across the
	// full mesh without collapsing relay endpoints to a sentinel).
	HopDelay NetworkModel
	// ValidateDelay is the per-hop dedup+validate cost — added on top of
	// the HopDelay sample at each forwarding hop. Default 0 (mesh is
	// modeled as fast-forwarding once a message clears dedup).
	ValidateDelay time.Duration
	// Gossip configures the lazy-push IHAVE/IWANT backstop layered on
	// top of the eager-push mesh. When Gossip.Enabled is false (default),
	// no heartbeat ticks fire and mcache state is unused — mesh behaves
	// purely as eager-push with no recovery for mesh misses (the
	// pre-Phase-B behavior). Set Gossip.Enabled = true to model real
	// libp2p's full gossip protocol; defaults match SSV's
	// network/topics/params/gossipsub.go overrides.
	Gossip MeshGossipConfig
}

// MeshGossipConfig parameterizes the lazy-push IHAVE/IWANT backstop.
// Defaults match the SSV operator's libp2p configuration in
// network/topics/params/gossipsub.go — heartbeat 700ms,
// HistoryLength=6 (≈4.2s mcache window), HistoryGossip=4 (≈2.8s IHAVE
// window). Dlazy / GossipFactor are unchanged from upstream gossipsub
// defaults (SSV does not override them).
//
// The mid lifecycle:
//
//  1. Publisher inserts msgID into its own mcache at emit time.
//  2. Each per-node heartbeat tick rotates its mcache (advances head,
//     evicts oldest slot) and emits IHAVE(mids from last HistoryGossip
//     slots) to Dlazy / GossipFactor random non-mesh peers.
//  3. An IHAVE recipient that hasn't seen any of the advertised mids
//     sends IWANT(unseen mids) back to the advertiser.
//  4. The advertiser looks each requested mid up in its mcache; for
//     hits, the entry's Reinject closure schedules a fresh
//     evtMeshArrival from advertiser to requester carrying the body.
//  5. The requester's MarkSeen gate de-dupes against any copy that
//     arrived via mesh in the meantime; if first-time, the body is
//     also inserted into the requester's mcache, propagating the
//     gossip cascade.
type MeshGossipConfig struct {
	// Enabled gates the entire gossip layer. Default false — existing
	// mesh-mode tests don't pick up gossip implicitly.
	Enabled bool

	// HeartbeatInterval between gossip ticks per node. Sim default
	// matches SSV's `HeartbeatInterval` = 700ms.
	HeartbeatInterval time.Duration

	// HistoryLength is the per-node mcache slot count. A mid stays
	// answerable to IWANT for HistoryLength × HeartbeatInterval. Sim
	// default matches SSV's `gsMcacheLen` = 6 (≈4.2s window).
	HistoryLength int

	// HistoryGossip is the IHAVE advertise-window slot count (must be
	// ≤ HistoryLength). Mids inserted within the last HistoryGossip
	// heartbeats are advertised; older mids stay answerable to IWANT
	// but aren't pre-advertised. Sim default matches SSV's
	// `gsMcacheGossip` = 4 (≈2.8s window).
	HistoryGossip int

	// Dlazy is the minimum IHAVE recipient count per heartbeat.
	// libp2p default; SSV does not override.
	Dlazy int

	// GossipFactor scales recipient count with the non-mesh peer pool:
	// IHAVE goes to max(Dlazy, ceil(GossipFactor × |non-mesh peers|)).
	// libp2p default; SSV does not override.
	GossipFactor float64
}

// WithDefaults returns a copy of g with any zero fields filled in to
// SSV-matched defaults. The full canonical numbers live here so a
// caller that sets, say, only HeartbeatInterval gets the rest from
// the SSV configuration baseline.
func (g MeshGossipConfig) WithDefaults() MeshGossipConfig {
	if g.HeartbeatInterval == 0 {
		g.HeartbeatInterval = 700 * time.Millisecond
	}
	if g.HistoryLength == 0 {
		g.HistoryLength = 6
	}
	if g.HistoryGossip == 0 {
		g.HistoryGossip = 4
	}
	if g.Dlazy == 0 {
		g.Dlazy = 6
	}
	if g.GossipFactor == 0 {
		g.GossipFactor = 0.25
	}
	return g
}

// Gossip-RPC wire-byte constants. IHAVE / IWANT both carry an msgID
// list; the body bytes are reused for the message itself when a peer
// answers IWANT (no separate constant). 32 B per mid mirrors real
// gossipsub's mid-as-hash framing; 32 B base covers the RPC envelope.
const (
	GossipBaseBytes   int64 = 32
	GossipPerMidBytes int64 = 32
)

// GossipRPCSize is the wire-byte count for an IHAVE / IWANT carrying
// `numMids` msg-IDs. Used by gossip event handlers for bandwidth
// accounting.
func GossipRPCSize(numMids int) int64 {
	return GossipBaseBytes + GossipPerMidBytes*int64(numMids)
}

// MCacheReinjectFunc is the per-mid reschedule callback stored in an
// MCacheEntry. Called by the IWANT handler when a peer's mcache hits
// a requested mid: schedules a fresh evtMeshArrival from the
// insertion node (= the cache owner) to `requester` (the IWANT
// sender), preserving the original publisher and body. The insertion
// node is captured by the closure at MCacheInsert time, so the
// signature carries only the requester.
//
// mesh.go can't construct the adapter-specific event itself; each
// adapter's emit-side and arrival-side close over the typed builder
// + arrival struct and stash a typed reinject closure here.
type MCacheReinjectFunc func(requester MeshNode)

// MCacheEntry is one mid's mcache record — kept for HistoryLength
// heartbeats after insert. Kind / Bytes are retained only for trace
// and bandwidth-debug visibility; the Reinject closure carries the
// rest of the payload internally.
type MCacheEntry struct {
	Kind     MsgKind
	Bytes    int64
	Reinject MCacheReinjectFunc
}

// nodeMcache is the per-node sliding-window cache used by the gossip
// layer. Slots are heartbeat-indexed; head advances with each
// MCacheRotate. The entries map is the lookup surface for IWANT
// responses (faster than walking slots); the slots list is the
// manifest used to evict on rotation and to enumerate the IHAVE
// advertise window.
type nodeMcache struct {
	entries map[MsgID]MCacheEntry
	slots   [][]MsgID
	head    int
}

// RelayEndpointBase is the first synthetic OperatorID allocated to a
// mesh relay peer. Reserved high enough that it never collides with a
// real cluster OperatorID (MakeOperators allocates 1..N; SSV-supported
// cluster sizes top out at 13). Stateful NetworkModel implementations
// keyed on (from, to) endpoint pairs (LossyNetwork's linkKey,
// CorrelatedLinkDelay, MarkovianSlownessDelay) thus see distinct
// per-relay-link keys when consulted from the mesh transport, instead
// of the previous behavior where every relay endpoint mapped to a
// shared sentinel-0 and collapsed link state across unrelated edges.
const RelayEndpointBase OperatorID = 1_000_000

// MeshTopology is the per-sim mesh state: peer-to-peer graph, per-node
// seen-MsgID sets for dedup, hop-delay sampler, and the MsgID counter.
// Built once at sim start via NewMeshTopology; the DES schedules
// per-adapter evtMeshArrival events that consult MarkSeen + Neighbors +
// HopDelaySample to forward.
//
// Concurrency: not safe for parallel sims. The framework runs each sim
// on a dedicated goroutine (RunBatch.runOneSim) and MeshTopology is
// per-sim, so single-goroutine access is the only supported pattern.
type MeshTopology struct {
	n         int          // cluster size
	r         int          // relay count (= n by default)
	cluster   []OperatorID // index [0, n) → cluster op IDs (1..n typically)
	opIndex   map[OperatorID]int
	neighbors [][]MeshNode
	isProto   []bool
	// endpoints[node] is the OperatorID-typed identifier passed to
	// NetworkModel.Delay for that node. For cluster nodes it equals
	// cluster[i]; for relays it equals RelayEndpointBase + (i - n) so
	// every relay has a distinct synthetic ID. This lets stateful
	// network models key per-mesh-edge without collapsing all relay
	// endpoints to the same sentinel.
	endpoints []OperatorID
	seen      []map[MsgID]struct{}
	hopDelay  NetworkModel
	valDelay  time.Duration
	nextMsgID MsgID
	// gossip is the captured-at-construction MeshGossipConfig, snapshotted
	// from MeshConfig.Gossip and run through WithDefaults so adapters and
	// gossip-event handlers read the same canonical values without
	// recomputing defaults on every access.
	gossip MeshGossipConfig
	// mcache is allocated lazily on first MCacheInsert call. Until then,
	// the gossip layer is dormant (existing eager-mesh tests don't pay
	// for unused memory). `mcacheHistory` records the HistoryLength
	// chosen on first init; subsequent inserts ignore the requested
	// history to keep per-node ring sizes uniform.
	mcache        []nodeMcache
	mcacheHistory int
}

// NewMeshTopology builds the per-sim mesh deterministically from `seed`.
// Topology at n=4: 4 protocol peers paired as P_0↔P_1 and P_2↔P_3, plus
// 4 relay peers. Each protocol peer has degree 3 (1 in-cluster + 2
// relays); each relay has degree 3 (mix of protocol attachments + relay-
// to-relay edges). For odd n, the unpaired protocol peer gets 3 relay
// neighbors and 0 protocol neighbors (modeling "no co-cluster op in my
// mesh today" — a real libp2p outcome at low cluster density). Generic
// n uses the same pairing-by-index rule plus a deterministic relay
// wiring; for n in {4, 7, 10, 13} (the SSV-supported set) this produces
// a connected graph — assertion-enforced.
//
// Panics on disconnected graph: the constraints should always yield a
// connected mesh given valid inputs (n ≥ 2, HopDelay set). A panic here
// means the wiring algorithm has a bug, not user error.
func NewMeshTopology(seed int64, cfg MeshConfig, cluster []OperatorID) *MeshTopology {
	n := len(cluster)
	if n < 2 {
		panic(fmt.Sprintf("consensustest: mesh requires >= 2 cluster operators, got %d", n))
	}
	if cfg.HopDelay == nil {
		panic("consensustest: MeshConfig.HopDelay must be set for DeliveryMesh")
	}
	r := n // RelayPoolSize = N
	total := n + r
	// Salted seed so the mesh's randomness is decorrelated from the
	// adapter-DES RNG stream (which is seeded off the same cfg.Seed).
	// Cross-correlated streams would make per-sim variance harder to
	// reason about; salting keeps them independent.
	rng := mrand.New(mrand.NewSource(seed ^ 0x6d6573682d76310a)) // "mesh-v1\n"

	m := &MeshTopology{
		n:         n,
		r:         r,
		cluster:   append([]OperatorID(nil), cluster...),
		opIndex:   make(map[OperatorID]int, n),
		neighbors: make([][]MeshNode, total),
		isProto:   make([]bool, total),
		endpoints: make([]OperatorID, total),
		seen:      make([]map[MsgID]struct{}, total),
		hopDelay:  cfg.HopDelay,
		valDelay:  cfg.ValidateDelay,
		gossip:    cfg.Gossip.WithDefaults(),
		mcache:    make([]nodeMcache, total),
	}
	for i, op := range cluster {
		m.opIndex[op] = i
		m.isProto[i] = true
		m.endpoints[i] = op
	}
	// Allocate distinct synthetic endpoint IDs for the relays, packed
	// contiguously from RelayEndpointBase. With base = 1_000_000 the
	// relay range can never collide with a cluster OperatorID
	// (MakeOperators returns 1..n with n ≤ 13 for SSV cluster sizes).
	for i := 0; i < r; i++ {
		m.endpoints[n+i] = RelayEndpointBase + OperatorID(i)
	}
	for i := 0; i < total; i++ {
		m.seen[i] = make(map[MsgID]struct{})
	}

	// Phase 1: pair protocol peers by index. P_{2k} ↔ P_{2k+1}.
	for i := 0; i+1 < n; i += 2 {
		m.addEdge(MeshNode(i), MeshNode(i+1))
	}
	// For odd n the trailing protocol peer is unpaired; it gets an extra
	// relay slot in Phase 2 (3 relays instead of 2) so its total degree
	// stays at 3.
	extraRelaySlot := make([]int, n)
	if n%2 == 1 {
		extraRelaySlot[n-1] = 1
	}

	// Phase 2: protocol → relay edges. Assign deterministically by
	// walking a shuffled relay list so each relay accumulates roughly
	// (2n + odd-correction) / r ≈ 2 protocol attachments on average.
	relays := make([]MeshNode, r)
	for i := 0; i < r; i++ {
		relays[i] = MeshNode(n + i)
	}
	rng.Shuffle(r, func(i, j int) { relays[i], relays[j] = relays[j], relays[i] })
	cursor := 0
	for i := 0; i < n; i++ {
		slots := 2 + extraRelaySlot[i]
		taken := make(map[MeshNode]struct{}, slots)
		for s := 0; s < slots; s++ {
			// Skip duplicates (this protocol peer already has this
			// relay neighbor) by advancing cursor until we find a
			// fresh one. Upper bound on the loop = r, beyond which
			// we're trying to assign more relay slots than exist.
			for tries := 0; tries < r; tries++ {
				cand := relays[cursor%len(relays)]
				cursor++
				if _, dup := taken[cand]; dup {
					continue
				}
				taken[cand] = struct{}{}
				m.addEdge(MeshNode(i), cand)
				break
			}
		}
	}

	// Phase 3: relay → relay edges. Top each relay up to degree 3.
	// Since relay protocol-attachment counts vary, the per-relay R-R
	// edge count is `max(0, 3 - protocolAttachments[r])`. For the
	// common case (r=n, average 2 attachments) this is ~1 R-R edge per
	// relay; relays that happened to absorb 3 protocol edges add none.
	for i := 0; i < r; i++ {
		node := MeshNode(n + i)
		// At this point neighbors[node] contains only protocol
		// attachments (from Phase 2).
		for len(m.neighbors[node]) < 3 {
			// Pick a random other relay we don't already neighbor.
			// Bounded retries — if we can't find a fresh relay
			// (would happen only at extremely small r), give up to
			// avoid an infinite loop; the connectivity check at the
			// end will panic if the result is disconnected.
			added := false
			for tries := 0; tries < r*2; tries++ {
				idx := rng.Intn(r)
				other := MeshNode(n + idx)
				if other == node {
					continue
				}
				if containsNode(m.neighbors[node], other) {
					continue
				}
				m.addEdge(node, other)
				added = true
				break
			}
			if !added {
				break
			}
		}
	}

	if !m.isConnected() {
		panic(fmt.Sprintf("consensustest: mesh topology disconnected at n=%d (seed=%d) — wiring bug", n, seed))
	}

	return m
}

// addEdge inserts an undirected edge {a, b}. Idempotent — duplicate calls
// are no-ops, so callers don't have to track which edges they already
// added.
func (m *MeshTopology) addEdge(a, b MeshNode) {
	if a == b {
		return
	}
	if !containsNode(m.neighbors[a], b) {
		m.neighbors[a] = append(m.neighbors[a], b)
	}
	if !containsNode(m.neighbors[b], a) {
		m.neighbors[b] = append(m.neighbors[b], a)
	}
}

func containsNode(s []MeshNode, x MeshNode) bool {
	for _, v := range s {
		if v == x {
			return true
		}
	}
	return false
}

// isConnected verifies that every node is reachable from node 0 via BFS.
// Used as a build-time sanity check; reachability holds for the SSV
// cluster sizes {4, 7, 10, 13} with the wiring rule above, so a false
// here is a programmer error.
func (m *MeshTopology) isConnected() bool {
	if len(m.neighbors) == 0 {
		return false
	}
	visited := make([]bool, len(m.neighbors))
	queue := []MeshNode{0}
	visited[0] = true
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		for _, nb := range m.neighbors[node] {
			if !visited[nb] {
				visited[nb] = true
				queue = append(queue, nb)
			}
		}
	}
	for _, v := range visited {
		if !v {
			return false
		}
	}
	return true
}

// NewMsgID returns the next per-sim MsgID. Sequential counter, monotonic
// within a sim; values are not stable across sims (each sim has its own
// MeshTopology with its own counter).
func (m *MeshTopology) NewMsgID() MsgID {
	m.nextMsgID++
	return m.nextMsgID
}

// MarkSeen records `id` against `node`'s seen-set. Returns true on the
// first call (the message has not been seen at this node before) and
// false on every subsequent call (duplicate — caller should drop).
func (m *MeshTopology) MarkSeen(node MeshNode, id MsgID) bool {
	s := m.seen[node]
	if _, ok := s[id]; ok {
		return false
	}
	s[id] = struct{}{}
	return true
}

// IsSeen reports whether `node` has `id` in its seen-set, without
// mutating state. Used by gossip IHAVE-receiver logic to decide
// whether to send IWANT for a mid (must NOT consume the dedup slot
// preemptively — a real-libp2p IHAVE doesn't mark, only the body
// arrival does).
func (m *MeshTopology) IsSeen(node MeshNode, id MsgID) bool {
	_, ok := m.seen[node][id]
	return ok
}

// Neighbors returns `node`'s mesh peers. Caller must NOT mutate the
// returned slice — it aliases internal state.
func (m *MeshTopology) Neighbors(node MeshNode) []MeshNode {
	return m.neighbors[node]
}

// TotalNodes returns the mesh's node count (cluster ops + relays).
// Used by the gossip layer to enumerate heartbeat targets and to
// sample non-mesh peers.
func (m *MeshTopology) TotalNodes() int { return m.n + m.r }

// Gossip returns the (defaults-filled) gossip configuration captured
// at construction time. Adapters consult this to decide whether to
// schedule heartbeats and what cadence to use.
func (m *MeshTopology) Gossip() MeshGossipConfig { return m.gossip }

// IsProtocol reports whether `node` is a cluster operator (true) or a
// forward-only relay peer (false).
func (m *MeshTopology) IsProtocol(node MeshNode) bool {
	return m.isProto[node]
}

// NodeForOperator returns the MeshNode index for cluster operator `op`.
// Panics if `op` is not in the cluster — every cluster operator passed
// to NewMeshTopology has an entry, so a miss is a programmer error.
func (m *MeshTopology) NodeForOperator(op OperatorID) MeshNode {
	idx, ok := m.opIndex[op]
	if !ok {
		panic(fmt.Sprintf("consensustest: mesh: operator %d not in cluster", op))
	}
	return MeshNode(idx)
}

// OperatorForNode returns the cluster OperatorID for protocol node
// `node`. Panics if `node` is a relay peer — callers must check
// IsProtocol first.
func (m *MeshTopology) OperatorForNode(node MeshNode) OperatorID {
	if int(node) < 0 || int(node) >= m.n {
		panic(fmt.Sprintf("consensustest: mesh: node %d is not a protocol peer (n=%d)", node, m.n))
	}
	return m.cluster[node]
}

// ValidateDelay returns the per-hop validate cost.
func (m *MeshTopology) ValidateDelay() time.Duration { return m.valDelay }

// SampleHopDelay draws one per-hop delay sample over the mesh edge
// (`fromEP`, `toEP`). Endpoint IDs are framework-typed OperatorIDs:
// cluster ops use their real IDs (1..N), relays use synthetic IDs
// allocated from RelayEndpointBase. Stateful NetworkModel implementations
// can key per-edge on these IDs without collapsing distinct relay edges.
func (m *MeshTopology) SampleHopDelay(rng *mrand.Rand, fromEP, toEP OperatorID, kind MsgKind) time.Duration {
	return m.hopDelay.Delay(rng, fromEP, toEP, kind)
}

// EndpointFor returns the OperatorID-typed endpoint identifier for
// `node` used by SampleHopDelay and any other per-edge keying. Cluster
// nodes resolve to their real OperatorID (1..N); relay nodes resolve to
// a synthetic ID in [RelayEndpointBase, RelayEndpointBase + r).
func (m *MeshTopology) EndpointFor(node MeshNode) OperatorID {
	return m.endpoints[node]
}

// RecordMeshHop dispatches one mesh-edge bandwidth observation to the
// correct BandwidthReport method based on whether each endpoint is a
// cluster op or a relay. Centralizing the four-way branch (proto→proto,
// proto→relay, relay→proto, relay→relay) keeps every adapter's emit
// and forward paths consistent: cluster's PerOperator* metrics only
// see cluster ops, while TotalBytes captures every wire hop including
// relay-side reflood.
func (m *MeshTopology) RecordMeshHop(b *BandwidthReport, from, to MeshNode, kind MsgKind, layer int, bytes int64) {
	if b == nil || bytes == 0 {
		return
	}
	fp := m.IsProtocol(from)
	tp := m.IsProtocol(to)
	switch {
	case fp && tp:
		b.Emission(m.cluster[from], m.cluster[to], kind, layer, bytes)
	case fp:
		b.EmissionToRelay(m.cluster[from], kind, layer, bytes)
	case tp:
		b.EmissionFromRelay(m.cluster[to], kind, layer, bytes)
	default:
		b.EmissionRelayToRelay(kind, layer, bytes)
	}
}

// ---- Gossip-layer helpers (MeshGossipConfig path) -----------------------

// initMcache lazily allocates per-node mcache rings the first time
// MCacheInsert is called. Subsequent calls with a different history
// length are silently ignored — the first writer wins, since changing
// ring size mid-sim would corrupt slot indexing.
func (m *MeshTopology) initMcache(history int) {
	if m.mcacheHistory != 0 {
		return
	}
	if history <= 0 {
		return
	}
	m.mcacheHistory = history
	for i := range m.mcache {
		m.mcache[i].entries = make(map[MsgID]MCacheEntry)
		m.mcache[i].slots = make([][]MsgID, history)
	}
}

// MCacheInsert records (msgID, entry) in `node`'s current heartbeat
// slot. Idempotent in msgID: a second insert at the same node is a
// no-op, so emit-side + arrival-side inserts of the same message can
// coexist (the first insert wins; subsequent reach the dedup early-
// return). `history` selects the ring size on first call; subsequent
// calls' values are ignored.
func (m *MeshTopology) MCacheInsert(node MeshNode, msgID MsgID, entry MCacheEntry, history int) {
	m.initMcache(history)
	if m.mcacheHistory == 0 {
		return
	}
	c := &m.mcache[node]
	if _, ok := c.entries[msgID]; ok {
		return
	}
	c.entries[msgID] = entry
	c.slots[c.head] = append(c.slots[c.head], msgID)
}

// MCacheLookup returns `node`'s entry for `msgID` if still in cache
// (within the last HistoryLength heartbeats). Used by the IWANT
// handler to find the reinject closure for each requested mid.
func (m *MeshTopology) MCacheLookup(node MeshNode, msgID MsgID) (MCacheEntry, bool) {
	if m.mcacheHistory == 0 {
		return MCacheEntry{}, false
	}
	e, ok := m.mcache[node].entries[msgID]
	return e, ok
}

// MCacheRotate advances `node`'s head one slot forward, evicting the
// mids in the new head (= oldest) slot from the entries map. Called
// by the heartbeat handler before collecting the IHAVE window so that
// rotation precedes the read.
func (m *MeshTopology) MCacheRotate(node MeshNode) {
	if m.mcacheHistory == 0 {
		return
	}
	c := &m.mcache[node]
	c.head = (c.head + 1) % m.mcacheHistory
	for _, mid := range c.slots[c.head] {
		delete(c.entries, mid)
	}
	c.slots[c.head] = c.slots[c.head][:0]
}

// MCacheGossipMids returns the union of mids in `node`'s last
// `windowSlots` heartbeat slots (inclusive of the current head).
// Caller may consume the returned slice freely — it's a freshly
// allocated copy. Returns nil when the mcache hasn't been initialized
// or when windowSlots ≤ 0. Larger windowSlots than HistoryLength is
// clamped silently.
func (m *MeshTopology) MCacheGossipMids(node MeshNode, windowSlots int) []MsgID {
	if m.mcacheHistory == 0 || windowSlots <= 0 {
		return nil
	}
	if windowSlots > m.mcacheHistory {
		windowSlots = m.mcacheHistory
	}
	c := &m.mcache[node]
	var mids []MsgID
	for i := 0; i < windowSlots; i++ {
		idx := (c.head - i + m.mcacheHistory) % m.mcacheHistory
		mids = append(mids, c.slots[idx]...)
	}
	return mids
}

// NonMeshPeers returns the set of mesh nodes that are NOT direct
// mesh-neighbors of `node` (and not `node` itself). Real gossipsub
// gossips IHAVE to topic peers outside the eager-push mesh; this
// helper bounds the recipient pool the same way. Result is freshly
// allocated.
func (m *MeshTopology) NonMeshPeers(node MeshNode) []MeshNode {
	total := MeshNode(m.TotalNodes())
	nbrs := m.neighbors[node]
	nbrSet := make(map[MeshNode]struct{}, len(nbrs))
	for _, nbr := range nbrs {
		nbrSet[nbr] = struct{}{}
	}
	out := make([]MeshNode, 0, int(total)-len(nbrSet)-1)
	for i := MeshNode(0); i < total; i++ {
		if i == node {
			continue
		}
		if _, isNbr := nbrSet[i]; isNbr {
			continue
		}
		out = append(out, i)
	}
	return out
}

// PickGossipRecipients samples IHAVE recipients via real gossipsub's
// formula: max(Dlazy, ceil(GossipFactor × |non-mesh peers|)) up to
// the pool size. Returns a freshly allocated, partially shuffled
// slice. Deterministic for a fixed (rng, node, Dlazy, factor).
//
// At small cluster sizes (n=4 → 7 non-mesh peers) Dlazy=6 will
// usually saturate the pool; GossipFactor only kicks in for larger
// non-mesh-peer counts (n=13 + relays). Per the real-libp2p formula
// in handleGetGossipPeers.
func (m *MeshTopology) PickGossipRecipients(rng *mrand.Rand, node MeshNode, dlazy int, factor float64) []MeshNode {
	pool := m.NonMeshPeers(node)
	if len(pool) == 0 {
		return nil
	}
	want := dlazy
	if scaled := int(math.Ceil(factor * float64(len(pool)))); scaled > want {
		want = scaled
	}
	if want > len(pool) {
		want = len(pool)
	}
	rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	return pool[:want]
}

// FormatMeshArrival returns the canonical "MeshArrival[...]" string
// used by every adapter's evtMeshArrival.describe(). Single-sourcing
// the format here keeps the producer side and ParseMeshArrivalTrace's
// regex from drifting; any layout change is one edit + the regex
// update right below.
func FormatMeshArrival(from, to, publisher MeshNode, msgID MsgID, kind MsgKind) string {
	return fmt.Sprintf("MeshArrival[from=%d to=%d publisher=%d msg=%d kind=%s]",
		from, to, publisher, msgID, kind)
}

// meshArrivalRE matches the FormatMeshArrival output. Internal-by-
// convention API for trace-consuming tests; kept package-private so
// callers go through ParseMeshArrivalTrace instead of duplicating the
// regex.
var meshArrivalRE = regexp.MustCompile(
	`^MeshArrival\[from=(\d+) to=(\d+) publisher=(\d+) msg=\d+ kind=\S+\]$`,
)

// ParseMeshArrivalTrace extracts (from, to, publisher) from one trace
// entry's Event string. Returns ok=false if `entry` isn't a MeshArrival
// line, so callers can scan a whole trace by filtering on the ok bool:
//
//	for _, e := range out.Trace {
//	    from, to, publisher, ok := ct.ParseMeshArrivalTrace(e.Event)
//	    if !ok { continue }
//	    // ...
//	}
//
// Used by the per-adapter TestMeshArrival_NoRefloodToPublisher
// regression tests, which assert that no scheduled arrival has
// to == publisher (the gossipsub publisher-exclusion contract).
func ParseMeshArrivalTrace(entry string) (from, to, publisher MeshNode, ok bool) {
	m := meshArrivalRE.FindStringSubmatch(entry)
	if m == nil {
		return 0, 0, 0, false
	}
	// Each capture group is `(\d+)`, so Atoi never fails on a match;
	// the underscored errors are unreachable.
	fromI, _ := strconv.Atoi(m[1])
	toI, _ := strconv.Atoi(m[2])
	pubI, _ := strconv.Atoi(m[3])
	return MeshNode(fromI), MeshNode(toI), MeshNode(pubI), true
}
