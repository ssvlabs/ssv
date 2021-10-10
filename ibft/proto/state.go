package proto

import (
	"encoding/json"
	"github.com/bloxapp/ssv/utils/threadsafe"
)

// state holds an iBFT state, thread safe
type State struct {
	Stage *threadsafe.SafeInt32
	// lambda is an instance unique identifier, much like a block hash in a blockchain
	Lambda *threadsafe.SafeBytes
	// sequence number is an incremental number for each instance, much like a block number would be in a blockchain
	SeqNumber     *threadsafe.SafeUint64
	InputValue    *threadsafe.SafeBytes
	Round         *threadsafe.SafeUint64
	PreparedRound *threadsafe.SafeUint64
	PreparedValue *threadsafe.SafeBytes
}

type unsafeState struct {
	Stage         int32
	Lambda        []byte
	SeqNumber     uint64
	InputValue    []byte
	Round         uint64
	PreparedRound uint64
	PreparedValue []byte
}

// MarshalJSON implements marshaling interface
func (s *State) MarshalJSON() ([]byte, error) {
	return json.Marshal(&unsafeState{
		Stage:         s.Stage.Get(),
		Lambda:        s.Lambda.Get(),
		SeqNumber:     s.SeqNumber.Get(),
		InputValue:    s.InputValue.Get(),
		Round:         s.Round.Get(),
		PreparedRound: s.PreparedRound.Get(),
		PreparedValue: s.PreparedValue.Get(),
	})
}

// UnmarshalJSON implements marshaling interface
func (s *State) UnmarshalJSON(data []byte) error {
	d := &unsafeState{}
	if err := json.Unmarshal(data, d); err != nil {
		return err
	}

	s.Stage = threadsafe.Int32(d.Stage)
	s.Lambda = threadsafe.Bytes(d.Lambda)
	s.SeqNumber = threadsafe.Uint64(d.SeqNumber)
	s.InputValue = threadsafe.Bytes(d.InputValue)
	s.Round = threadsafe.Uint64(d.Round)
	s.PreparedRound = threadsafe.Uint64(d.PreparedRound)
	s.PreparedValue = threadsafe.Bytes(d.PreparedValue)
	return nil
}
