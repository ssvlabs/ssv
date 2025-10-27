package qbft

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectests "github.com/ssvlabs/ssv-spec/qbft/spectest/tests"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
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
	ExpectedErrorCode                                int
}

// UnmarshalJSON implements json.Unmarshaler to handle the Value field as hex and StateValue as base64
func (test *CreateMsgSpecTest) UnmarshalJSON(data []byte) error {
	// Create an alias to prevent recursion
	type Alias CreateMsgSpecTest
	aux := &struct {
		Value      string `json:"Value"`      // Accept Value as hex string
		StateValue string `json:"StateValue"` // Accept StateValue as base64 string
		*Alias
	}{
		Alias: (*Alias)(test),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Convert the hex string Value to [32]byte
	if aux.Value != "" {
		decoded, err := hex.DecodeString(aux.Value)
		if err != nil {
			return errors.Wrap(err, "failed to decode Value hex string")
		}
		if len(decoded) != 32 {
			return errors.Errorf("Value must be 32 bytes, got %d", len(decoded))
		}
		copy(test.Value[:], decoded)
	}

	// Convert the base64 string StateValue to []byte
	if aux.StateValue != "" {
		decoded, err := base64.StdEncoding.DecodeString(aux.StateValue)
		if err != nil {
			return errors.Wrap(err, "failed to decode StateValue base64 string")
		}
		test.StateValue = decoded
	}

	return nil
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
	spectests.AssertErrorCode(t, test.ExpectedErrorCode, err)

	r, err := msg.GetRoot()
	require.NoError(t, err)
	require.EqualValues(t, test.ExpectedRoot, hex.EncodeToString(r[:]))
}

func createCommit(test *CreateMsgSpecTest) (*spectypes.SignedSSVMessage, error) {
	ks := spectestingutils.Testing4SharesSet()
	state := &specqbft.State{
		CommitteeMember: spectestingutils.TestingCommitteeMember(ks),
		ID:              spectestingutils.TestingIdentifier,
		Round:           test.Round,
	}
	signer := spectestingutils.TestingOperatorSigner(ks)

	return specqbft.CreateCommit(state, signer, test.Value)
}

func createPrepare(test *CreateMsgSpecTest) (*spectypes.SignedSSVMessage, error) {
	ks := spectestingutils.Testing4SharesSet()
	state := &specqbft.State{
		CommitteeMember: spectestingutils.TestingCommitteeMember(ks),
		ID:              spectestingutils.TestingIdentifier,
		Round:           test.Round,
	}
	signer := spectestingutils.TestingOperatorSigner(ks)

	return specqbft.CreatePrepare(state, signer, test.Round, test.Value)
}

func createProposal(test *CreateMsgSpecTest) (*spectypes.SignedSSVMessage, error) {
	ks := spectestingutils.Testing4SharesSet()
	state := &specqbft.State{
		CommitteeMember: spectestingutils.TestingCommitteeMember(ks),
		ID:              spectestingutils.TestingIdentifier,
		Round:           test.Round,
	}
	signer := spectestingutils.TestingOperatorSigner(ks)

	// Use StateValue (full data) instead of Value (which is already a hash)
	return specqbft.CreateProposal(state, signer, test.StateValue,
		spectestingutils.ToProcessingMessages(test.RoundChangeJustifications),
		spectestingutils.ToProcessingMessages(test.PrepareJustifications))
}

func createRoundChange(test *CreateMsgSpecTest) (*spectypes.SignedSSVMessage, error) {
	ks := spectestingutils.Testing4SharesSet()
	state := &specqbft.State{
		CommitteeMember:  spectestingutils.TestingCommitteeMember(ks),
		ID:               spectestingutils.TestingIdentifier,
		PrepareContainer: specqbft.NewMsgContainer(),
		Round:            test.Round,
	}
	signer := spectestingutils.TestingOperatorSigner(ks)

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

	// Use test.Round as the new round parameter and StateValue as instance start value
	return specqbft.CreateRoundChange(state, signer, test.Round, test.StateValue)
}
