package types

import (
	"crypto/sha256"
	"encoding/json"

	"github.com/pkg/errors"
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
)

// State is copied from spec with changed Share
type State struct {
	Share                           *genesisspectypes.Share
	ID                              []byte // instance Identifier
	Round                           genesisspecqbft.Round
	Height                          genesisspecqbft.Height
	LastPreparedRound               genesisspecqbft.Round
	LastPreparedValue               []byte
	ProposalAcceptedForCurrentRound *genesisspecqbft.SignedMessage
	Decided                         bool
	DecidedValue                    []byte

	ProposeContainer     *genesisspecqbft.MsgContainer
	PrepareContainer     *genesisspecqbft.MsgContainer
	CommitContainer      *genesisspecqbft.MsgContainer
	RoundChangeContainer *genesisspecqbft.MsgContainer
}

// GetRoot returns the state's deterministic root
func (s *State) GetRoot() ([32]byte, error) {
	marshaledRoot, err := s.Encode()
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "could not encode state")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}

// Encode returns a msg encoded bytes or error
func (s *State) Encode() ([]byte, error) {
	return json.Marshal(s)
}

// Decode returns error if decoding failed
func (s *State) Decode(data []byte) error {
	return json.Unmarshal(data, &s)
}
