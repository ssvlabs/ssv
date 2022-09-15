package commit

import (
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
)

func TestValidateCommitData(t *testing.T) {
	validData, err := (&specqbft.CommitData{Data: []byte("data1")}).Encode()
	require.NoError(t, err)

	invalidData := []byte("data2")

	noData, err := (&specqbft.CommitData{Data: []byte("")}).Encode()
	require.NoError(t, err)

	tests := []struct {
		name string
		msg  *specqbft.SignedMessage
		err  string
	}{
		{
			"valid data",
			&specqbft.SignedMessage{
				Message: &specqbft.Message{
					MsgType:    specqbft.CommitMsgType,
					Round:      1,
					Identifier: []byte("Identifier"),
					Data:       validData,
				},
				Signature: []byte{},
				Signers:   []spectypes.OperatorID{},
			},
			"",
		},
		{
			"invalid data",
			&specqbft.SignedMessage{
				Message: &specqbft.Message{
					MsgType:    specqbft.CommitMsgType,
					Round:      1,
					Identifier: []byte("Identifier"),
					Data:       invalidData,
				},
				Signature: []byte{},
				Signers:   []spectypes.OperatorID{},
			},
			"could not get msg commit data: could not decode commit data from message: invalid character 'd' looking for beginning of value",
		},
		{
			"no data",
			&specqbft.SignedMessage{
				Message: &specqbft.Message{
					MsgType:    specqbft.CommitMsgType,
					Round:      1,
					Identifier: []byte("Identifier"),
					Data:       noData,
				},
				Signature: []byte{},
				Signers:   []spectypes.OperatorID{},
			},
			"msgCommitData invalid: CommitData data is invalid",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateCommitData().Run(tc.msg)
			if len(tc.err) > 0 {
				require.EqualError(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateMsgSigners(t *testing.T) {
	tests := []struct {
		name string
		msg  *specqbft.SignedMessage
		err  string
	}{
		{
			"one signer",
			&specqbft.SignedMessage{
				Signers: []spectypes.OperatorID{1},
			},
			"",
		},
		{
			"no signers",
			&specqbft.SignedMessage{
				Signers: []spectypes.OperatorID{},
			},
			ErrInvalidSignersNum.Error(),
		},
		{
			"two signers",
			&specqbft.SignedMessage{
				Signers: []spectypes.OperatorID{1, 2},
			},
			ErrInvalidSignersNum.Error(),
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMsgSigners().Run(tc.msg)
			if len(tc.err) > 0 {
				require.EqualError(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateProposal(t *testing.T) {
	data1, err := (&specqbft.CommitData{Data: []byte("data1")}).Encode()
	require.NoError(t, err)

	data2, err := (&specqbft.CommitData{Data: []byte("data2")}).Encode()
	require.NoError(t, err)

	tests := []struct {
		name                            string
		msg                             *specqbft.SignedMessage
		proposalAcceptedForCurrentRound *specqbft.SignedMessage
		err                             string
	}{
		{
			"proposal has same value",
			&specqbft.SignedMessage{
				Message: &specqbft.Message{
					MsgType:    specqbft.CommitMsgType,
					Round:      1,
					Identifier: []byte("Identifier"),
					Data:       data1,
				},
				Signature: []byte{},
				Signers:   []spectypes.OperatorID{},
			},
			&specqbft.SignedMessage{
				Message: &specqbft.Message{
					MsgType:    specqbft.CommitMsgType,
					Round:      1,
					Identifier: []byte("Identifier"),
					Data:       data1,
				},
				Signature: []byte{},
				Signers:   []spectypes.OperatorID{},
			},
			"",
		},
		{
			"proposal has different value",
			&specqbft.SignedMessage{
				Message: &specqbft.Message{
					MsgType:    specqbft.CommitMsgType,
					Round:      1,
					Identifier: []byte("Identifier"),
					Data:       data1,
				},
				Signature: []byte{},
				Signers:   []spectypes.OperatorID{},
			},
			&specqbft.SignedMessage{
				Message: &specqbft.Message{
					MsgType:    specqbft.CommitMsgType,
					Round:      1,
					Identifier: []byte("Identifier"),
					Data:       data2,
				},
				Signature: []byte{},
				Signers:   []spectypes.OperatorID{},
			},
			"message data is different from proposed data",
		},
		{
			"no proposal",
			&specqbft.SignedMessage{
				Message: &specqbft.Message{
					MsgType:    specqbft.CommitMsgType,
					Round:      1,
					Identifier: []byte("Identifier"),
					Data:       data1,
				},
				Signature: []byte{},
				Signers:   []spectypes.OperatorID{},
			},
			nil,
			"did not receive proposal for this round",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			state := &qbft.State{}
			state.ProposalAcceptedForCurrentRound.Store(tc.proposalAcceptedForCurrentRound)

			err := ValidateProposal(state).Run(tc.msg)
			if len(tc.err) > 0 {
				require.EqualError(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateDecidedQuorum(t *testing.T) {
	tests := []struct {
		name  string
		msg   *specqbft.SignedMessage
		share *beacon.Share
		err   string
	}{
		{
			"has quorum",
			&specqbft.SignedMessage{
				Signers: []spectypes.OperatorID{1, 2, 3},
			},
			&beacon.Share{
				Committee: map[spectypes.OperatorID]*beacon.Node{
					1: nil,
					2: nil,
					3: nil,
					4: nil,
				},
			},
			"",
		},
		{
			"no quorum",
			&specqbft.SignedMessage{
				Signers: []spectypes.OperatorID{1, 2},
			},
			&beacon.Share{
				Committee: map[spectypes.OperatorID]*beacon.Node{
					1: nil,
					2: nil,
					3: nil,
					4: nil,
				},
			},
			"not a decided msg",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDecidedQuorum(tc.share).Run(tc.msg)
			if len(tc.err) > 0 {
				require.EqualError(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
