package ssv

import (
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// proposedBlockRootRetention bounds how many slots of decided block roots to keep. The §6 envelope
// runner reads the root for its own slot, written by the §4 proposer runner moments earlier, so a
// small window is plenty.
const proposedBlockRootRetention = 4

// ProposedBlockRoots records, per slot, the block root the proposer runner decided in §4 so the §6
// envelope runner (and its value-check) can read the same root — the envelope's BeaconBlockRoot must
// match the §4-decided block (SIP #94 §6). It is shared between a single validator's proposer and
// envelope runners; lives in package ssv so both the runner and value-check can use it without an
// import cycle. Safe for concurrent use.
type ProposedBlockRoots struct {
	mu    sync.Mutex
	roots map[phase0.Slot]phase0.Root
}

func NewProposedBlockRoots() *ProposedBlockRoots {
	return &ProposedBlockRoots{roots: make(map[phase0.Slot]phase0.Root)}
}

// Set records the §4-decided block root for the slot and evicts roots older than the retention window.
func (s *ProposedBlockRoots) Set(slot phase0.Slot, root phase0.Root) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.roots[slot] = root
	for sl := range s.roots {
		if slot > proposedBlockRootRetention && sl < slot-proposedBlockRootRetention {
			delete(s.roots, sl)
		}
	}
}

// Get returns the §4-decided block root recorded for the slot, if any.
func (s *ProposedBlockRoots) Get(slot phase0.Slot) (phase0.Root, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, ok := s.roots[slot]
	return root, ok
}
