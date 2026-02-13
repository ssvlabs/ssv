package validation

// signer_state.go describes state of a signer.

import (
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
}
