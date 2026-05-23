package obft

import (
	"container/list"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// MaxPendingPerSlot caps the number of envelopes buffered per slot before
// the instance starts. Above this, additional envelopes are dropped.
const MaxPendingPerSlot = 64

// MaxPendingSlots caps the total number of distinct slots with non-empty
// buffers. Bounds memory under an attacker that emits envelopes targeting
// many distinct slot numbers. When exceeded, the oldest slot entry is
// evicted (FIFO).
const MaxPendingSlots = 256

// MaxEndedSlots caps how many recently-ended slots the controller remembers
// for the BufferEnvelope post-teardown gate. Sized to comfortably exceed the
// validation slot-window (obftAllowedPastSlots + obftAllowedFutureSlots ≈ 6)
// so any envelope that could pass slot-window admission is checked against the
// ended-set.
const MaxEndedSlots = 64

// pendingBuffer buffers envelopes that arrived before a slot's instance
// started (e.g., faster peers send during the slow operator's pre-consensus).
// Drained after StartNewInstance so early arrivals replay into the now-active
// instance. Capped per-slot to MaxPendingPerSlot and globally to
// MaxPendingSlots (FIFO slot eviction) to bound memory under abuse.
//
// Not thread-safe: the owning Controller serializes all access under its mutex
// (the buffer participates in cross-concern checks against the ended-ring under
// that single lock).
type pendingBuffer struct {
	bySlot map[phase0.Slot][]PendingEnvelope
	order  *list.List                    // *list.Element holds phase0.Slot (FIFO)
	elem   map[phase0.Slot]*list.Element // O(1) lookup for drain/forget
}

func newPendingBuffer() *pendingBuffer {
	return &pendingBuffer{
		bySlot: make(map[phase0.Slot][]PendingEnvelope),
		order:  list.New(),
		elem:   make(map[phase0.Slot]*list.Element),
	}
}

// add appends env to slot's buffer, enforcing the per-slot and global caps.
// A full per-slot buffer drops the envelope; exceeding MaxPendingSlots evicts
// the oldest slot (FIFO) before inserting the new one. O(1) amortized.
func (p *pendingBuffer) add(slot phase0.Slot, env PendingEnvelope) {
	if len(p.bySlot[slot]) >= MaxPendingPerSlot {
		return
	}
	if _, exists := p.bySlot[slot]; !exists {
		if len(p.bySlot) >= MaxPendingSlots {
			if front := p.order.Front(); front != nil {
				oldest := front.Value.(phase0.Slot)
				p.order.Remove(front)
				delete(p.elem, oldest)
				delete(p.bySlot, oldest)
			}
		}
		p.elem[slot] = p.order.PushBack(slot)
	}
	p.bySlot[slot] = append(p.bySlot[slot], env)
}

// drain removes and returns all buffered envelopes for slot.
func (p *pendingBuffer) drain(slot phase0.Slot) []PendingEnvelope {
	out := p.bySlot[slot]
	p.forget(slot)
	return out
}

// forget removes slot from all three internal structures, keeping them in sync.
func (p *pendingBuffer) forget(slot phase0.Slot) {
	if elem, ok := p.elem[slot]; ok {
		p.order.Remove(elem)
		delete(p.elem, slot)
	}
	delete(p.bySlot, slot)
}

// endedRing is a small FIFO-bounded set of recently-ended slots. BufferEnvelope
// consults it to refuse post-teardown buffering: a late peer broadcast (after
// EndInstance cleared the slot's pending) would otherwise re-buffer an envelope
// that has nowhere to drain into. Eviction is FIFO at MaxEndedSlots; slots that
// fall out of the ring can re-accept pending entries (which then sit until LRU
// eviction at MaxPendingSlots), but the ring is sized generously enough to
// cover the validation slot-window so that's effectively dead code.
//
// Not thread-safe: serialized by the owning Controller's mutex.
type endedRing struct {
	set   map[phase0.Slot]struct{}
	order *list.List // *list.Element holds phase0.Slot (FIFO)
}

func newEndedRing() *endedRing {
	return &endedRing{
		set:   make(map[phase0.Slot]struct{}),
		order: list.New(),
	}
}

// has reports whether slot is currently in the ring.
func (e *endedRing) has(slot phase0.Slot) bool {
	_, ok := e.set[slot]
	return ok
}

// mark records slot in the ring (idempotent), evicting the oldest entry FIFO
// once MaxEndedSlots is reached.
func (e *endedRing) mark(slot phase0.Slot) {
	if _, exists := e.set[slot]; exists {
		return
	}
	if e.order.Len() >= MaxEndedSlots {
		if front := e.order.Front(); front != nil {
			oldest := front.Value.(phase0.Slot)
			e.order.Remove(front)
			delete(e.set, oldest)
		}
	}
	e.set[slot] = struct{}{}
	e.order.PushBack(slot)
}
