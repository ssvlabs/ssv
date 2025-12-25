package validation

// signer_state.go describes state of a signer.

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/libp2p/go-libp2p/core/peer"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
)

// SignerState represents the current state of a signer (an Operator running a Runner that performs partial-signing
// for duties of some type: proposer, committee, etc.) for a particular slot.
type SignerState struct {
	// Slot records current slot of the signer.
	Slot phase0.Slot

	// Round records current QBFT round (relevant for duties that have QBFT consensus phase) of the signer.
	Round specqbft.Round

	// SeenMsgTypes tracks what messages we've seen from this signer so far.
	SeenMsgTypes SeenMsgTypes

	// HashedProposalData records the 1st proposal we've seen from this signer.
	// Storing a pointer to byte array instead of slice to reduce memory consumption when we don't need the hash.
	// A nil slice could be an alternative, but it'd consume more memory, and we'd need to cast [32]byte returned by sha256.Sum256() to slice.
	HashedProposalData *[32]byte

	// Max possible map size for committee sizes:
	//  4 (f=1): C(4,3)+C(4,4)=5
	//  7 (f=2): C(7,5)+C(7,6)+C(7,7)=29
	// 10 (f=3): C(10,7)+C(10,8)+C(10,9)+C(10,10)=176
	// 13 (f=4): C(13,9)+C(13,10)+C(13,11)+C(13,12)+C(13,13)=1093
	SeenSigners map[SignersBitMask]struct{}

	// SeenViolations keeps track of validation violations by peers we get messages for this signer from detected
	// so far (mapping peer-id -> error-text).
	SeenViolations map[peer.ID]map[string]struct{}
}

func newSignerState(slot phase0.Slot, round specqbft.Round) *SignerState {
	s := &SignerState{}
	s.Reset(slot, round)
	return s
}

// Reset resets the state's round, message counts, and proposal data to the given values.
// It also updates the start time to the current time.
func (s *SignerState) Reset(slot phase0.Slot, round specqbft.Round) {
	s.Slot = slot
	s.Round = round
	s.SeenMsgTypes = SeenMsgTypes{}
	s.HashedProposalData = nil
	s.SeenSigners = nil    // lazy init on demand to reduce mem consumption
	s.SeenViolations = nil // lazy init on demand to reduce mem consumption
}

// IgnoreOrReject decides between returning an ignoreErr or rejectErr depending on whether this violation
// (this particular error type) has already been seen from the very same peer.
// 1-time violations are always allowed (ignored) because it's computationally expensive to indentify it, and
// the rest must be rejected since it is easily detectable.
func (s *SignerState) IgnoreOrReject(ignoreErr, rejectErr Error, receivedFrom peer.ID) (chosenErr Error) {
	if s.SeenViolations == nil {
		s.SeenViolations = make(map[peer.ID]map[string]struct{})
	}
	if s.SeenViolations[receivedFrom] == nil {
		s.SeenViolations[receivedFrom] = make(map[string]struct{})
	}
	_, seen := s.SeenViolations[receivedFrom][ignoreErr.text]
	if seen {
		return rejectErr
	}
	s.SeenViolations[receivedFrom][ignoreErr.text] = struct{}{}
	return ignoreErr
}
