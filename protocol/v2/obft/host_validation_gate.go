package obft

// ValidationRequest is a request from an Instance to its runner asking the
// host application to validate a value V at a particular layer. Emitted on the
// gate's channel when a V is first observed without an existing host verdict
// (the peer-reflood-V path). The runner dispatches validation against its host
// hook and calls back via ApplyHostValidity.
type ValidationRequest struct {
	Layer int
	Value Value
}

// HostValidationGate is the per-Instance channel + dedup plumbing through
// which the protocol asks its runner to host-validate a value first observed
// without a verdict. It is pure mechanism (shared by both OBFT protocols); the
// "already validated?" policy is supplied by the caller, since it reads the
// per-protocol hostVerdict state.
//
// Not thread-safe; the owning Instance serializes access (the runner adapter
// runs each Instance behind its own mutex).
type HostValidationGate struct {
	ch      chan ValidationRequest
	pending map[int]map[[32]byte]bool
}

// NewHostValidationGate returns a gate whose request channel is buffered to
// `buffer` (typically K). A full buffer makes Request a no-op (with rollback)
// rather than blocking.
func NewHostValidationGate(buffer int) *HostValidationGate {
	return &HostValidationGate{
		ch:      make(chan ValidationRequest, buffer),
		pending: make(map[int]map[[32]byte]bool, buffer),
	}
}

// Channel returns the receive end the runner drains.
func (g *HostValidationGate) Channel() <-chan ValidationRequest { return g.ch }

// Close closes the request channel. Called once at Instance.Finalize so a
// runner draining via range/select observes end-of-slot. Must not be called
// concurrently with Request (the Instance's single-goroutine contract).
func (g *HostValidationGate) Close() { close(g.ch) }

// Request enqueues a host-validation request for (layer, value), unless:
// `ended` is set, layer is outside [0, k), value is empty, the value is
// already validated (per alreadyValidated), or a request for the same
// (layer, root) is already in flight. Non-blocking: on a full buffer it rolls
// back the in-flight flag so a later call can retry.
//
// `alreadyValidated`, if non-nil, reports whether the host has already
// recorded a verdict for (layer, root) — the per-protocol hostVerdict check.
func (g *HostValidationGate) Request(layer int, value Value, k int, ended bool, alreadyValidated func(layer int, root [32]byte) bool) {
	if ended || layer < 0 || layer >= k || len(value) == 0 {
		return
	}
	root := ValueRoot(value)
	if alreadyValidated != nil && alreadyValidated(layer, root) {
		return
	}
	bucket := g.pending[layer]
	if bucket == nil {
		bucket = make(map[[32]byte]bool)
		g.pending[layer] = bucket
	}
	if bucket[root] {
		return
	}
	bucket[root] = true
	select {
	case g.ch <- ValidationRequest{Layer: layer, Value: append(Value{}, value...)}:
	default:
		// Buffer full — runner not draining. Roll back so a later call retries.
		delete(bucket, root)
	}
}

// ClearPending drops the in-flight flag for (layer, root) once a verdict is
// recorded, keeping the pending set memory-bounded. (base never calls this —
// its pending set is monotonic-after-rollback; twoab clears on ApplyHostValidity.)
func (g *HostValidationGate) ClearPending(layer int, root [32]byte) {
	if bucket := g.pending[layer]; bucket != nil {
		delete(bucket, root)
	}
}

// PendingCount returns the total number of in-flight (layer, root) requests,
// summed across layers. Used by Instance.Stats.
func (g *HostValidationGate) PendingCount() int {
	n := 0
	for _, b := range g.pending {
		n += len(b)
	}
	return n
}
