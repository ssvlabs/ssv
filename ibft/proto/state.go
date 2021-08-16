package proto

import (
	"encoding/json"
	"github.com/bloxapp/ssv/utils/threadsafe"
)

type State struct {
	Stage RoundState
	// lambda is an instance unique identifier, much like a block hash in a blockchain
	Lambda *threadsafe.SafeBytes
	// sequence number is an incremental number for each instance, much like a block number would be in a blockchain
	SeqNumber     *threadsafe.SafeUint64
	InputValue    *threadsafe.SafeBytes
	Round         uint64
	PreparedRound uint64
	PreparedValue *threadsafe.SafeBytes
}

type unsafeState struct {
	Stage         RoundState
	Lambda        []byte
	SeqNumber     uint64
	InputValue    []byte
	Round         uint64
	PreparedRound uint64
	PreparedValue []byte
}

func (s *State) MarshalJSON() ([]byte, error) {
	return json.Marshal(&unsafeState{
		Stage:         s.Stage,
		Lambda:        s.Lambda.Get(),
		SeqNumber:     s.SeqNumber.Get(),
		InputValue:    s.InputValue.Get(),
		Round:         s.Round,
		PreparedRound: s.PreparedRound,
		PreparedValue: s.PreparedValue.Get(),
	})
}

func (s *State) UnmarshalJSON(data []byte) error {
	d := &unsafeState{}
	if err := json.Unmarshal(data, d); err != nil {
		return err
	}

	s.Stage = d.Stage
	s.Lambda = threadsafe.Bytes(d.Lambda)
	s.SeqNumber = threadsafe.Uint64(d.SeqNumber)
	s.InputValue = threadsafe.Bytes(d.InputValue)
	s.Round = d.Round
	s.PreparedRound = d.PreparedRound
	s.PreparedValue = threadsafe.Bytes(d.PreparedValue)
	return nil
}
