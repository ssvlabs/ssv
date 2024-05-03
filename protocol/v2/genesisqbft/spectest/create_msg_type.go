package qbft

import (
	"encoding/hex"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectests "github.com/ssvlabs/ssv-spec-pre-cc/qbft/spectest/tests"
	genesisspectestingutils "github.com/ssvlabs/ssv-spec-pre-cc/types/testingutils"
)

func RunCreateMsg(t *testing.T, test *genesisspectests.CreateMsgSpecTest) {
	var msg *genesisspecqbft.SignedMessage
	var err error
	switch test.CreateType {
	case genesisspectests.CreateProposal:
		msg, err = createProposal(test)
	case genesisspectests.CreatePrepare:
		msg, err = createPrepare(test)
	case genesisspectests.CreateCommit:
		msg, err = createCommit(test)
	case genesisspectests.CreateRoundChange:
		msg, err = createRoundChange(test)
	default:
		t.Fail()
	}

	if err != nil && len(test.ExpectedError) != 0 {
		require.EqualError(t, err, test.ExpectedError)
		return
	}
	require.NoError(t, err)

	r, err2 := msg.GetRoot()
	if len(test.ExpectedError) != 0 {
		require.EqualError(t, err2, test.ExpectedError)
		return
	}
	require.NoError(t, err2)
	require.EqualValues(t, test.ExpectedRoot, hex.EncodeToString(r[:]))
}

func createCommit(test *genesisspectests.CreateMsgSpecTest) (*genesisspecqbft.SignedMessage, error) {
	ks := genesisspectestingutils.Testing4SharesSet()
	state := &genesisspecqbft.State{
		Share: genesisspectestingutils.TestingShare(ks),
		ID:    []byte{1, 2, 3, 4},
	}
	config := genesisspectestingutils.TestingConfig(ks)

	return genesisspecqbft.CreateCommit(state, config, test.Value)
}

func createPrepare(test *genesisspectests.CreateMsgSpecTest) (*genesisspecqbft.SignedMessage, error) {
	ks := genesisspectestingutils.Testing4SharesSet()
	state := &genesisspecqbft.State{
		Share: genesisspectestingutils.TestingShare(ks),
		ID:    []byte{1, 2, 3, 4},
	}
	config := genesisspectestingutils.TestingConfig(ks)

	return genesisspecqbft.CreatePrepare(state, config, test.Round, test.Value)
}

func createProposal(test *genesisspectests.CreateMsgSpecTest) (*genesisspecqbft.SignedMessage, error) {
	ks := genesisspectestingutils.Testing4SharesSet()
	state := &genesisspecqbft.State{
		Share: genesisspectestingutils.TestingShare(ks),
		ID:    []byte{1, 2, 3, 4},
	}
	config := genesisspectestingutils.TestingConfig(ks)

	return genesisspecqbft.CreateProposal(state, config, test.Value[:], test.RoundChangeJustifications, test.PrepareJustifications)
}

func createRoundChange(test *genesisspectests.CreateMsgSpecTest) (*genesisspecqbft.SignedMessage, error) {
	ks := genesisspectestingutils.Testing4SharesSet()
	state := &genesisspecqbft.State{
		Share:            genesisspectestingutils.TestingShare(ks),
		ID:               []byte{1, 2, 3, 4},
		PrepareContainer: genesisspecqbft.NewMsgContainer(),
	}
	config := genesisspectestingutils.TestingConfig(ks)

	if len(test.PrepareJustifications) > 0 {
		state.LastPreparedRound = test.PrepareJustifications[0].Message.Round
		state.LastPreparedValue = test.StateValue

		for _, msg := range test.PrepareJustifications {
			_, err := state.PrepareContainer.AddFirstMsgForSignerAndRound(msg)
			if err != nil {
				return nil, errors.Wrap(err, "could not add first message for signer")
			}
		}
	}

	return genesisspecqbft.CreateRoundChange(state, config, 1, test.Value[:])
}
