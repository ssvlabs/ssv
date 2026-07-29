package validation

// signer_state.go describes state of a signer.

import (
	"slices"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/libp2p/go-libp2p/core/peer"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
)

// SignerStateForSlotRound is a SignerState bundled with some target slot+round.
type SignerStateForSlotRound struct {
	// Slot records current slot of the signer.
	Slot phase0.Slot
	// Round records current QBFT round (relevant for duties that have QBFT consensus phase) of the signer.
	Round specqbft.Round

	// Peers maps peer IDs to their respective peer-states, peer-state is constructed from all the messages our
	// operator received from this particular peer. It is an additional measure used to track misbehavior from
	// specific peers.
	Peers map[peer.ID]*SignerState
	// World is the world-state, it's the aggregate state across all peers our operator received messages from.
	// It is used to ensure the logical integrity of the ssv-protocol.
	World SignerState
}

func (s *SignerStateForSlotRound) Peer(peerID peer.ID) *SignerState {
	state := s.Peers[peerID]
	if state == nil {
		s.Peers[peerID] = &SignerState{}
	}
	return s.Peers[peerID]
}

func newSignerState(slot phase0.Slot, round specqbft.Round) *SignerStateForSlotRound {
	s := &SignerStateForSlotRound{}
	s.Reset(slot, round)
	return s
}

// Reset resets the state's round, message counts, and proposal data to the given values.
// It also updates the start time to the current time.
func (s *SignerStateForSlotRound) Reset(slot phase0.Slot, round specqbft.Round) {
	s.Slot = slot
	s.Round = round

	s.Peers = make(map[peer.ID]*SignerState, 16) // 16 is just a guesstimate

	s.World.SeenMsgTypes = SeenMsgTypes{}
	s.World.HashedProposalData = nil
	s.World.SeenDecidedMsgSignersCount = 0
	s.World.SeenProposerPreferencesRoots = nil
	s.World.SeenRequestAuthRoots = nil
}

// SignerState represents the state of a signer (an Operator running a Runner that performs partial-signing for
// duties of some type: proposer, committee, etc.).
type SignerState struct {
	// SeenMsgTypes tracks what messages we've seen from this signer so far.
	SeenMsgTypes SeenMsgTypes

	// HashedProposalData records the 1st proposal we've seen from this signer.
	// Storing a pointer to byte array instead of slice to reduce memory consumption when we don't need the hash.
	// A nil slice could be an alternative, but it'd consume more memory, and we'd need to cast [32]byte returned by sha256.Sum256() to slice.
	HashedProposalData *[32]byte

	// SeenDecidedMsgSignersCount records the max number of signers we've seen with a decided message.
	SeenDecidedMsgSignersCount int

	// SeenProposerPreferencesRoots records the distinct ProposerPreferences signing roots seen from this
	// signer (SIP #94 §5): that type is capped by distinct root (up to maxProposerPreferencesDistinctRoots),
	// not by the single pre-consensus bit in SeenMsgTypes. nil until the first such message.
	SeenProposerPreferencesRoots [][32]byte

	// SeenRequestAuthRoots records the distinct RequestAuthV1 signing roots seen from this signer
	// (issue #2962) — root-capped like the §5 preference roots above, up to
	// maxRequestAuthDistinctRoots. nil until the first such message.
	SeenRequestAuthRoots [][32]byte
}

// hasProposerPreferencesRoot reports whether root has already been seen from this signer.
func (s *SignerState) hasProposerPreferencesRoot(root [32]byte) bool {
	return slices.Contains(s.SeenProposerPreferencesRoots, root)
}

// proposerPreferencesRootCount returns the number of distinct roots seen from this signer.
func (s *SignerState) proposerPreferencesRootCount() int {
	return len(s.SeenProposerPreferencesRoots)
}

// recordProposerPreferencesRoot adds root to the seen set, skipping roots already present.
func (s *SignerState) recordProposerPreferencesRoot(root [32]byte) {
	if slices.Contains(s.SeenProposerPreferencesRoots, root) {
		return
	}
	s.SeenProposerPreferencesRoots = append(s.SeenProposerPreferencesRoots, root)
}

// hasRequestAuthRoot reports whether the request-auth root has already been seen from this signer.
func (s *SignerState) hasRequestAuthRoot(root [32]byte) bool {
	return slices.Contains(s.SeenRequestAuthRoots, root)
}

// requestAuthRootCount returns the number of distinct request-auth roots seen from this signer.
func (s *SignerState) requestAuthRootCount() int {
	return len(s.SeenRequestAuthRoots)
}

// recordRequestAuthRoot adds the request-auth root to the seen set, skipping roots already present.
func (s *SignerState) recordRequestAuthRoot(root [32]byte) {
	if slices.Contains(s.SeenRequestAuthRoots, root) {
		return
	}
	s.SeenRequestAuthRoots = append(s.SeenRequestAuthRoots, root)
}
