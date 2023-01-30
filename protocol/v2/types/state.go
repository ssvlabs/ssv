package types

import (
	"crypto/sha256"
	"encoding/json"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
)

type State struct {
	Share                           *spectypes.Share
	ID                              []byte // instance Identifier
	Round                           specqbft.Round
	Height                          specqbft.Height
	LastPreparedRound               specqbft.Round
	LastPreparedValue               []byte
	ProposalAcceptedForCurrentRound *specqbft.SignedMessage
	Decided                         bool
	DecidedValue                    []byte

	ProposeContainer     *specqbft.MsgContainer
	PrepareContainer     *specqbft.MsgContainer
	CommitContainer      *specqbft.MsgContainer
	RoundChangeContainer *specqbft.MsgContainer
}

// GetRoot returns the state's deterministic root
func (s *State) GetRoot() ([]byte, error) {
	marshaledRoot, err := s.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "could not encode state")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret[:], nil
}

// Encode returns a msg encoded bytes or error
func (s *State) Encode() ([]byte, error) {
	return json.Marshal(s)
}

// Decode returns error if decoding failed
func (s *State) Decode(data []byte) error {
	return json.Unmarshal(data, &s)
}

// type ProposedValueCheckF func(data []byte) error
// type ProposerF func(state *qbft.State, round qbft.Round) types.OperatorID
