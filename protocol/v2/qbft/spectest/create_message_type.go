package qbft

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
	typescomparable "github.com/ssvlabs/ssv-spec/types/testingutils/comparable"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	"github.com/stretchr/testify/require"
)

const (
	CreateProposal    = "createProposal"
	CreatePrepare     = "CreatePrepare"
	CreateCommit      = "CreateCommit"
	CreateRoundChange = "CreateRoundChange"
)

type CreateMsgSpecTest struct {
	Name string
	// ISSUE 217: rename to root
	Value [32]byte
	// ISSUE 217: rename to value
	StateValue                                       []byte
	Round                                            qbft.Round
	RoundChangeJustifications, PrepareJustifications []*types.SignedSSVMessage
	CreateType                                       string
	ExpectedRoot                                     string
	ExpectedState                                    types.Root `json:"-"` // Field is ignored by encoding/json"
	ExpectedError                                    string
}

func (test *CreateMsgSpecTest) Run(t *testing.T) {
	var msg *types.SignedSSVMessage
	var err error
	switch test.CreateType {
	case CreateProposal:
		msg, err = test.createProposal()
	case CreatePrepare:
		msg, err = test.createPrepare()
	case CreateCommit:
		msg, err = test.createCommit()
	case CreateRoundChange:
		msg, err = test.createRoundChange()
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

	if test.ExpectedRoot != hex.EncodeToString(r[:]) {
		fmt.Printf("expected: %v\n", test.ExpectedRoot)
		fmt.Printf("actuak: %v\n", hex.EncodeToString(r[:]))
		// diff := typescomparable.PrintDiff(test.ExpectedState, msg)
		require.Fail(t, "post state not equal", "")
	}
	require.EqualValues(t, test.ExpectedRoot, hex.EncodeToString(r[:]))

	CompareWithJson(t, test, test.TestName(), reflect.TypeOf(test).String())
}

func (test *CreateMsgSpecTest) createCommit() (*types.SignedSSVMessage, error) {
	ks := testingutils.Testing4SharesSet()
	state := &qbft.State{
		CommitteeMember: testingutils.TestingCommitteeMember(ks),
		ID:              []byte{1, 2, 3, 4},
	}
	signer := testingutils.TestingOperatorSigner(ks)

	return qbft.CreateCommit(state, signer, test.Value)
}

func (test *CreateMsgSpecTest) createPrepare() (*types.SignedSSVMessage, error) {
	ks := testingutils.Testing4SharesSet()
	state := &qbft.State{
		CommitteeMember: testingutils.TestingCommitteeMember(ks),
		ID:              []byte{1, 2, 3, 4},
	}
	signer := testingutils.TestingOperatorSigner(ks)

	return qbft.CreatePrepare(state, signer, test.Round, test.Value)
}

func (test *CreateMsgSpecTest) createProposal() (*types.SignedSSVMessage, error) {
	ks := testingutils.Testing4SharesSet()
	state := &qbft.State{
		CommitteeMember: testingutils.TestingCommitteeMember(ks),
		ID:              []byte{1, 2, 3, 4},
	}
	signer := testingutils.TestingOperatorSigner(ks)

	return qbft.CreateProposal(state, signer, test.Value[:], testingutils.ToProcessingMessages(test.
		RoundChangeJustifications), testingutils.ToProcessingMessages(test.PrepareJustifications))
}

func (test *CreateMsgSpecTest) createRoundChange() (*types.SignedSSVMessage, error) {
	ks := testingutils.Testing4SharesSet()
	state := &qbft.State{
		CommitteeMember:  testingutils.TestingCommitteeMember(ks),
		ID:               []byte{1, 2, 3, 4},
		PrepareContainer: qbft.NewMsgContainer(),
	}
	signer := testingutils.TestingOperatorSigner(ks)

	if len(test.PrepareJustifications) > 0 {
		prepareMsg, err := qbft.DecodeMessage(test.PrepareJustifications[0].SSVMessage.Data)
		if err != nil {
			return nil, err
		}
		state.LastPreparedRound = prepareMsg.Round
		state.LastPreparedValue = test.StateValue

		for _, msg := range test.PrepareJustifications {
			_, err := state.PrepareContainer.AddFirstMsgForSignerAndRound(testingutils.ToProcessingMessage(msg))
			if err != nil {
				return nil, errors.Wrap(err, "could not add first message for signer")
			}
		}
	}

	return qbft.CreateRoundChange(state, signer, 1, test.Value[:])
}

func (test *CreateMsgSpecTest) TestName() string {
	return "qbft create message " + test.Name
}

func (test *CreateMsgSpecTest) GetPostState() (interface{}, error) {
	return test, nil
}

func CompareWithJson(t *testing.T, test any, testName string, testType string) {
	byts, err := json.Marshal(test)
	require.NoError(t, err)
	//unmarshal json into map
	var testMap map[string]interface{}
	err = json.Unmarshal(byts, &testMap)
	require.NoError(t, err)

	expectedTestMap, err := GetExpectedStateFromScFile(testName, testType)
	require.NoError(t, err)

	diff := cmp.Diff(testMap, expectedTestMap)
	if diff != "" {
		t.Errorf("%s inputs changed. %v", testName, diff)
	}
}

func GetExpectedStateFromScFile(testName string, testType string) (map[string]interface{}, error) {
	specDir, err := protocoltesting.GetSpecDir("", filepath.Join("qbft", "spectest"))
	if err != nil {
		return nil, err
	}
	expectedState, err := readStateComparison(specDir, testName, testType)
	if err != nil {
		return nil, err
	}
	return expectedState, nil
}

// readStateComparison reads a json derived from 'testName' and unmarshals it into a json object
func readStateComparison(basedir string, testName string, testType string) (map[string]interface{}, error) {
	basedir = filepath.Join(basedir, "generate")
	strType := strings.Replace(testType, "qbft.", "tests.", 1)
	scDir := typescomparable.GetSCDir(basedir, strType)

	path := filepath.Join(scDir, fmt.Sprintf("%s.json", testName))
	byteValue, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	err = json.Unmarshal(byteValue, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}
