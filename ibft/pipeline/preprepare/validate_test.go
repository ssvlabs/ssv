package preprepare

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/ibft/proto"
	ibfttesting "github.com/bloxapp/ssv/ibft/spec_testing"
	"github.com/bloxapp/ssv/utils/dataval/bytesval"
)

func TestValidatePrePrepareValue(t *testing.T) {
	sks, nodes := ibfttesting.GenerateNodes(4)
	params := &proto.InstanceParams{
		ConsensusParams: proto.DefaultConsensusParams(),
		IbftCommittee:   nodes,
	}
	state := &proto.State{
		Round:         1,
		Lambda:        []byte("Lambda"),
		PreparedRound: 0,
		PreparedValue: nil,
	}
	consensus := bytesval.New([]byte(time.Now().Weekday().String()))

	// test no signer
	msg := &proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_PrePrepare,
			Round:  1,
			Lambda: []byte("Lambda"),
			Value:  []byte(time.Now().Weekday().String()),
		},
		Signature: []byte{},
		SignerIds: []uint64{},
	}
	err := ValidatePrePrepareMsg(consensus, state, params).Run(msg)
	require.EqualError(t, err, "invalid number of signers for pre-prepare message")

	// test > 1 signer
	msg = &proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_PrePrepare,
			Round:  1,
			Lambda: []byte("Lambda"),
			Value:  []byte(time.Now().Weekday().String()),
		},
		Signature: []byte{},
		SignerIds: []uint64{1, 2},
	}
	err = ValidatePrePrepareMsg(consensus, state, params).Run(msg)
	require.EqualError(t, err, "invalid number of signers for pre-prepare message")

	msg = ibfttesting.SignMsg(1, sks[1], &proto.Message{
		Type:   proto.RoundState_PrePrepare,
		Round:  1,
		Lambda: []byte("Lambda"),
		Value:  []byte("wrong value"),
	})
	err = ValidatePrePrepareMsg(consensus, state, params).Run(msg)
	require.EqualError(t, err, "msg value is wrong")

	msg = ibfttesting.SignMsg(2, sks[2], &proto.Message{
		Type:   proto.RoundState_PrePrepare,
		Round:  1,
		Lambda: []byte("Lambda"),
		Value:  []byte("wrong value"),
	})
	err = ValidatePrePrepareMsg(consensus, state, params).Run(msg)
	require.EqualError(t, err, "pre-prepare message sender is not the round's leader")

	msg = ibfttesting.SignMsg(1, sks[1], &proto.Message{
		Type:   proto.RoundState_PrePrepare,
		Round:  1,
		Lambda: []byte("Lambda"),
		Value:  []byte(time.Now().Weekday().String()),
	})
	err = ValidatePrePrepareMsg(consensus, state, params).Run(msg)
	require.NoError(t, err)
}
