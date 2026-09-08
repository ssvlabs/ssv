package ssv

import (
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// proposedBlockRetention bounds how many slots of §4 decisions to keep. The §6 envelope runner reads
// the decision for its own slot, written by the §4 proposer runner moments earlier, so a small window
// is plenty.
const proposedBlockRetention = 4

// ProposedBlock is the §4 decision the §6 envelope duty binds a disseminated envelope against (SIP #94
// §6): the decided block's root, its parent root, and the execution-requests root its bid commits to.
// ProducedLocally marks the operator whose own produceBlockV4 response is the decided block — the
// builder operator, the only one that disseminates and publishes — and ProducedEnvelope is the reveal
// data that response carried: the envelope, blobs, and KZG proofs of a self-build, nil for everyone else
// and for an external build.
type ProposedBlock struct {
	BlockRoot             phase0.Root
	ParentRoot            phase0.Root
	ExecutionRequestsRoot phase0.Root
	ProducedLocally       bool
	ProducedEnvelope      *gloas.ProducedEnvelope
}

// Binds reports whether a disseminated blinded envelope commits to this §4 decision — the four SIP #94
// §6 checks, none of which needs payload bytes: self-build builder index, the decided block root, the
// decided block's parent root, and the requests root the bid commits to. PayloadRoot is left unpinned:
// nothing local can check it, and validation admits one dissemination from any committee operator, so a
// faulty operator can race a differing PayloadRoot in and split the signing round, costing the slot's
// reveal (liveness only, non-slashable). Pinning it needs the §4 value to commit to the payload root, the
// direction under discussion on SIP #94, which also retires dissemination.
func (p ProposedBlock) Binds(envelope *gloas.BlindedExecutionPayloadEnvelope) bool {
	if envelope == nil || envelope.ExecutionRequests == nil {
		return false
	}
	// The spec's blinded envelope carries ssv-spec's BuilderIndex type; the sentinel value is the same.
	if uint64(envelope.BuilderIndex) != uint64(gloas.BuilderIndexSelfBuild) {
		return false
	}
	if envelope.BeaconBlockRoot != p.BlockRoot || envelope.ParentBeaconBlockRoot != p.ParentRoot {
		return false
	}
	requestsRoot, err := envelope.ExecutionRequests.HashTreeRoot()
	if err != nil {
		return false
	}
	return phase0.Root(requestsRoot) == p.ExecutionRequestsRoot
}

// ProposedBlocks is the §4→§6 linkage store: per slot, the block the proposer runner decided in §4, for
// the §6 envelope runner to bind disseminated envelopes against (SIP #94 §6). It is shared between a
// single validator's proposer (writer) and envelope (reader) runners; it lives in package ssv so both
// can use it without an import cycle. Safe for concurrent use.
type ProposedBlocks struct {
	mu     sync.Mutex
	blocks map[phase0.Slot]ProposedBlock
}

func NewProposedBlocks() *ProposedBlocks {
	return &ProposedBlocks{blocks: make(map[phase0.Slot]ProposedBlock)}
}

// Record stores the slot's §4 decision and evicts decisions older than the retention window.
func (s *ProposedBlocks) Record(slot phase0.Slot, block ProposedBlock) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blocks[slot] = block
	for sl := range s.blocks {
		if slot > proposedBlockRetention && sl < slot-proposedBlockRetention {
			delete(s.blocks, sl)
		}
	}
}

// Get returns the §4 decision recorded for the slot, if any.
func (s *ProposedBlocks) Get(slot phase0.Slot) (ProposedBlock, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	block, ok := s.blocks[slot]
	return block, ok
}
