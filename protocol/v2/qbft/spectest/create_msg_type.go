package qbft

import (
	"encoding/hex"
	"testing"

	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectests "github.com/ssvlabs/ssv-spec/qbft/spectest/tests"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
)

type CreateMsgSpecTest struct {
	Name string
	// ISSUE 217: rename to root
	Value [32]byte
	// ISSUE 217: rename to value
	StateValue                                       []byte
	Round                                            specqbft.Round
	RoundChangeJustifications, PrepareJustifications []*spectypes.SignedSSVMessage
	CreateType                                       string
	ExpectedRoot                                     string
	ExpectedState                                    spectypes.Root `json:"-"` // Field is ignored by encoding/json"
	ExpectedError                                    string
}

func (test *CreateMsgSpecTest) TestName() string {
	return "qbft create message " + test.Name
}

func (test *CreateMsgSpecTest) RunCreateMsg(t *testing.T) {
	var msg *spectypes.SignedSSVMessage
	var err error
	switch test.CreateType {
	case spectests.CreateProposal:
		msg, err = createProposal(test)
	case spectests.CreatePrepare:
		msg, err = createPrepare(test)
	case spectests.CreateCommit:
		msg, err = createCommit(test)
	case spectests.CreateRoundChange:
		msg, err = createRoundChange(test)
	default:
		t.Fail()
	}
	if test.ExpectedError != "" {
		require.EqualError(t, err, test.ExpectedError)
	} else {
		require.NoError(t, err)
	}

	r, err := msg.GetRoot()
	require.NoError(t, err)
	require.EqualValues(t, test.ExpectedRoot, hex.EncodeToString(r[:]))
}

func createCommit(test *CreateMsgSpecTest) (*spectypes.SignedSSVMessage, error) {
	ks := spectestingutils.Testing4SharesSet()
	state := &specqbft.State{
		CommitteeMember: spectestingutils.TestingCommitteeMember(ks),
		ID:              []byte{1, 2, 3, 4},
	}
	signer := spectestingutils.NewOperatorSigner(ks, 1)

	return instance.CreateCommit(state, signer, test.Value)
}

func createPrepare(test *CreateMsgSpecTest) (*spectypes.SignedSSVMessage, error) {
	ks := spectestingutils.Testing4SharesSet()
	state := &specqbft.State{
		CommitteeMember: spectestingutils.TestingCommitteeMember(ks),
		ID:              []byte{1, 2, 3, 4},
	}
	signer := spectestingutils.NewOperatorSigner(ks, 1)

	return instance.CreatePrepare(state, signer, test.Round, test.Value)
}

func createProposal(test *CreateMsgSpecTest) (*spectypes.SignedSSVMessage, error) {
	ks := spectestingutils.Testing4SharesSet()
	state := &specqbft.State{
		CommitteeMember: spectestingutils.TestingCommitteeMember(ks),
		ID:              []byte{1, 2, 3, 4},
	}
	signer := spectestingutils.NewOperatorSigner(ks, 1)

	return instance.CreateProposal(state, signer, test.Value[:],
		spectestingutils.ToProcessingMessages(test.RoundChangeJustifications),
		spectestingutils.ToProcessingMessages(test.PrepareJustifications))
}

func createRoundChange(test *CreateMsgSpecTest) (*spectypes.SignedSSVMessage, error) {
	ks := spectestingutils.Testing4SharesSet()
	state := &specqbft.State{
		CommitteeMember:  spectestingutils.TestingCommitteeMember(ks),
		ID:               []byte{1, 2, 3, 4},
		PrepareContainer: specqbft.NewMsgContainer(),
	}
	signer := spectestingutils.NewOperatorSigner(ks, 1)

	if len(test.PrepareJustifications) > 0 {
		prepareMsg, err := specqbft.DecodeMessage(test.PrepareJustifications[0].SSVMessage.Data)
		if err != nil {
			return nil, err
		}
		state.LastPreparedRound = prepareMsg.Round
		state.LastPreparedValue = test.StateValue

		for _, msg := range test.PrepareJustifications {
			_, err := state.PrepareContainer.AddFirstMsgForSignerAndRound(spectestingutils.ToProcessingMessage(msg))
			if err != nil {
				return nil, errors.Wrap(err, "could not add first message for signer")
			}
		}
	}

	return instance.CreateRoundChange(state, signer, 1, test.Value[:])
}
