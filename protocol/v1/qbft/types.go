package qbft

import (
	"encoding/json"
	"go.uber.org/atomic"
)

// State holds an iBFT state, thread safe
type State struct {
	Stage atomic.Int32
	// lambda is an instance unique identifier, much like a block hash in a blockchain
	Identifier atomic.String
	// Height is an incremental number for each instance, much like a block number would be in a blockchain
	Height        atomic.Uint64
	InputValue    atomic.String
	Round         atomic.Uint64
	PreparedRound atomic.Uint64
	PreparedValue atomic.String
}

type unsafeState struct {
	Stage         int32
	Identifier    string
	Height        uint64
	InputValue    string
	Round         uint64
	PreparedRound uint64
	PreparedValue string
}

// MarshalJSON implements marshaling interface
func (s *State) MarshalJSON() ([]byte, error) {
	return json.Marshal(&unsafeState{
		Stage:         s.Stage.Load(),
		Identifier:    s.Identifier.Load(),
		Height:        s.Height.Load(),
		InputValue:    s.InputValue.Load(),
		Round:         s.Round.Load(),
		PreparedRound: s.PreparedRound.Load(),
		PreparedValue: s.PreparedValue.Load(),
	})
}

// UnmarshalJSON implements marshaling interface
func (s *State) UnmarshalJSON(data []byte) error {
	d := &unsafeState{}
	if err := json.Unmarshal(data, d); err != nil {
		return err
	}

	s.Stage.Store(d.Stage)
	s.Identifier.Store(d.Identifier)
	s.Height.Store(d.Height)
	s.InputValue.Store(d.InputValue)
	s.Round.Store(d.Round)
	s.PreparedRound.Store(d.PreparedRound)
	s.PreparedValue.Store(d.PreparedValue)

	return nil
}
