package proto

import (
	"encoding/json"
	"github.com/bloxapp/ssv/utils/threadsafe"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMarshaling(t *testing.T) {
	state := &State{
		Stage:         threadsafe.Int32(int32(RoundState_Decided)),
		Lambda:        threadsafe.BytesS("lambda"),
		SeqNumber:     threadsafe.Uint64(1),
		InputValue:    threadsafe.BytesS("input"),
		Round:         threadsafe.Uint64(3),
		PreparedRound: threadsafe.Uint64(2),
		PreparedValue: threadsafe.BytesS("prepared value"),
	}

	byts, err := json.Marshal(state)
	require.NoError(t, err)

	unmarshaledState := &State{}
	require.NoError(t, json.Unmarshal(byts, unmarshaledState))

	require.EqualValues(t, RoundState_Decided, unmarshaledState.Stage.Get())
	require.EqualValues(t, []byte("lambda"), unmarshaledState.Lambda.Get())
	require.EqualValues(t, 1, unmarshaledState.SeqNumber.Get())
	require.EqualValues(t, []byte("input"), unmarshaledState.InputValue.Get())
	require.EqualValues(t, 3, unmarshaledState.Round.Get())
	require.EqualValues(t, 2, unmarshaledState.PreparedRound.Get())
	require.EqualValues(t, []byte("prepared value"), unmarshaledState.PreparedValue.Get())
}
