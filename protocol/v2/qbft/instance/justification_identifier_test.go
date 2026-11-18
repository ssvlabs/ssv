package instance

import (
	"errors"
	"testing"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// newSiblingTestEnv returns an instance for a committee role and the identifier of its aggregator
// committee sibling: same committee, height and leader, so round changes and prepares signed
// for it pass every check but the identifier.
func newSiblingTestEnv(t *testing.T, operatorID spectypes.OperatorID) (*instanceTestEnv, []byte) {
	t.Helper()

	env := newInstanceTestEnv(t, operatorID)
	committeeID := spectypes.CommitteeID{1, 2, 3, 4}
	own := spectypes.NewCommitteeMsgID(spectestingutils.TestingSSVDomainType, committeeID, spectypes.RoleCommittee)
	sibling := spectypes.NewCommitteeMsgID(spectestingutils.TestingSSVDomainType, committeeID, spectypes.RoleAggregatorCommittee)
	env.inst.State.ID = own[:]
	return env, sibling[:]
}

// signedFor signs msg with Identifier set to identifier and the outer MsgID set to msgID.
func (e *instanceTestEnv) signedFor(
	msg *specqbft.Message,
	identifier []byte,
	msgID []byte,
	signerID spectypes.OperatorID,
	fullData []byte,
) *specqbft.ProcessingMessage {
	e.t.Helper()

	msg.Identifier = identifier
	data, err := msg.Encode()
	require.NoError(e.t, err)

	ssvMsg := &spectypes.SSVMessage{MsgType: spectypes.SSVConsensusMsgType, Data: data}
	copy(ssvMsg.MsgID[:], msgID)
	sig, err := spectypes.SignSSVMessage(e.keys.OperatorKeys[signerID], ssvMsg)
	require.NoError(e.t, err)

	procMsg, err := specqbft.NewProcessingMessage(&spectypes.SignedSSVMessage{
		OperatorIDs: []spectypes.OperatorID{signerID},
		Signatures:  [][]byte{sig},
		SSVMessage:  ssvMsg,
		FullData:    fullData,
	})
	require.NoError(e.t, err)
	return procMsg
}

func (e *instanceTestEnv) prepareFor(identifier, msgID []byte, round specqbft.Round, signerID spectypes.OperatorID, root [32]byte) *specqbft.ProcessingMessage {
	e.t.Helper()

	return e.signedFor(&specqbft.Message{
		MsgType: specqbft.PrepareMsgType,
		Height:  e.inst.State.Height,
		Round:   round,
		Root:    root,
	}, identifier, msgID, signerID, nil)
}

func (e *instanceTestEnv) roundChangeFor(
	identifier, msgID []byte,
	round specqbft.Round,
	signerID spectypes.OperatorID,
	dataRound specqbft.Round,
	root [32]byte,
	fullData []byte,
	prepares []*specqbft.ProcessingMessage,
) *specqbft.ProcessingMessage {
	e.t.Helper()

	return e.signedFor(&specqbft.Message{
		MsgType:                  specqbft.RoundChangeMsgType,
		Height:                   e.inst.State.Height,
		Round:                    round,
		Root:                     root,
		DataRound:                dataRound,
		RoundChangeJustification: e.marshalJustifications(prepares),
	}, identifier, msgID, signerID, fullData)
}

func requireIdentifierInvalid(t *testing.T, err error) {
	t.Helper()

	require.ErrorContains(t, err, "message doesn't belong to instance")
	var specErr *spectypes.Error
	require.True(t, errors.As(err, &specErr))
	require.Equal(t, spectypes.MessageIdentifierInvalidErrorCode, specErr.Code)
}

// A proposal must not justify a new value in a later round with unprepared round changes signed
// for a sibling instance.
func TestProposalWithSiblingRoundChangeJustificationsRefused(t *testing.T) {
	value := []byte("proposal-value")

	for _, tc := range []struct {
		name    string
		foreign []bool // per round-change signer 1, 2, 3
	}{
		{name: "own identifier", foreign: []bool{false, false, false}},
		{name: "all sibling", foreign: []bool{true, true, true}},
		{name: "one sibling", foreign: []bool{false, false, true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, sibling := newSiblingTestEnv(t, 2)
			env.setLeader(1)
			env.inst.State.Round = 2
			own := env.inst.State.ID

			roundChanges := make([]*specqbft.ProcessingMessage, 0, 3)
			for i, foreign := range tc.foreign {
				id := own
				if foreign {
					id = sibling
				}
				roundChanges = append(roundChanges, env.roundChangeFor(id, id, 2, spectypes.OperatorID(i+1), specqbft.NoRound, [32]byte{}, nil, nil))
			}
			proposal := env.proposal(2, 1, value, env.hash(value), roundChanges, nil)

			err := env.inst.BaseMsgValidation(proposal)
			if tc.name == "own identifier" {
				require.NoError(t, err)
				return
			}
			requireIdentifierInvalid(t, err)
		})
	}
}

func TestProposalWithSiblingPrepareJustificationsRefused(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(map[bool]string{false: "own identifier", true: "sibling"}[foreign], func(t *testing.T) {
			env, sibling := newSiblingTestEnv(t, 2)
			env.setLeader(1)
			env.inst.State.Round = 2

			value := []byte("prepared-value")
			roundChanges, prepares := env.preparedRoundChangeSet(2, 1, value, []spectypes.OperatorID{1, 2, 3}, []spectypes.OperatorID{1, 2, 3})
			if foreign {
				root := env.hash(value)
				prepares = []*specqbft.ProcessingMessage{
					env.prepareFor(sibling, sibling, 1, 1, root),
					env.prepareFor(sibling, sibling, 1, 2, root),
					env.prepareFor(sibling, sibling, 1, 3, root),
				}
			}
			proposal := env.proposal(2, 1, value, env.hash(value), roundChanges, prepares)

			err := env.inst.BaseMsgValidation(proposal)
			if !foreign {
				require.NoError(t, err)
				return
			}
			// isProposalJustification replaces the prepare's error with its own.
			require.ErrorContains(t, err, "signed prepare not valid")
		})
	}
}

func TestPreparedRoundChangeWithSiblingPreparesRefused(t *testing.T) {
	newMsgs := func(env *instanceTestEnv, prepareID []byte) (*specqbft.ProcessingMessage, []byte) {
		value := []byte("prepared-value")
		root := env.hash(value)
		own := env.inst.State.ID
		prepares := []*specqbft.ProcessingMessage{
			env.prepareFor(prepareID, prepareID, 1, 1, root),
			env.prepareFor(prepareID, prepareID, 1, 2, root),
			env.prepareFor(prepareID, prepareID, 1, 3, root),
		}
		return env.roundChangeFor(own, own, 2, 3, 1, root, value, prepares), value
	}

	t.Run("own identifier accepted on its own", func(t *testing.T) {
		env, _ := newSiblingTestEnv(t, 1)
		env.inst.Start(t.Context(), []byte("start-value"), testValueChecker{})
		rc, _ := newMsgs(env, env.inst.State.ID)

		_, _, _, err := env.inst.ProcessMsg(t.Context(), zap.NewNop(), rc)
		require.NoError(t, err)
		require.Len(t, env.inst.State.RoundChangeContainer.MessagesForRound(2), 1)
	})

	t.Run("sibling refused on its own", func(t *testing.T) {
		env, sibling := newSiblingTestEnv(t, 1)
		env.inst.Start(t.Context(), []byte("start-value"), testValueChecker{})
		rc, _ := newMsgs(env, sibling)

		_, _, _, err := env.inst.ProcessMsg(t.Context(), zap.NewNop(), rc)
		requireIdentifierInvalid(t, err)
		require.Empty(t, env.inst.State.RoundChangeContainer.AllMessages())
	})

	t.Run("sibling refused nested in a proposal", func(t *testing.T) {
		env, sibling := newSiblingTestEnv(t, 2)
		env.setLeader(1)
		env.inst.State.Round = 2
		own := env.inst.State.ID

		rc, value := newMsgs(env, sibling)
		root := env.hash(value)
		roundChanges := []*specqbft.ProcessingMessage{
			env.roundChangeFor(own, own, 2, 1, specqbft.NoRound, [32]byte{}, nil, nil),
			env.roundChangeFor(own, own, 2, 2, specqbft.NoRound, [32]byte{}, nil, nil),
			rc,
		}
		prepares := []*specqbft.ProcessingMessage{
			env.prepareFor(own, own, 1, 1, root),
			env.prepareFor(own, own, 1, 2, root),
			env.prepareFor(own, own, 1, 3, root),
		}
		proposal := env.proposal(2, 1, value, root, roundChanges, prepares)

		requireIdentifierInvalid(t, env.inst.BaseMsgValidation(proposal))
	})
}

// Identifier and MsgID are both signed; each must match the instance on its own.
func TestIdentifierAndMsgIDCheckedSeparately(t *testing.T) {
	for _, tc := range []struct {
		name            string
		foreignInner    bool
		foreignMsgID    bool
		wantErrIdentity bool
	}{
		{name: "both own"},
		{name: "foreign inner identifier", foreignInner: true, wantErrIdentity: true},
		{name: "foreign msg id", foreignMsgID: true, wantErrIdentity: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, sibling := newSiblingTestEnv(t, 2)
			own := env.inst.State.ID
			identifier, msgID := own, own
			if tc.foreignInner {
				identifier = sibling
			}
			if tc.foreignMsgID {
				msgID = sibling
			}

			value := []byte("prepared-value")
			root := env.hash(value)
			check := func(err error) {
				t.Helper()
				if tc.wantErrIdentity {
					requireIdentifierInvalid(t, err)
				} else {
					require.NoError(t, err)
				}
			}

			// prepare, top-level and as a justification
			check(env.inst.validSignedPrepareForHeightRoundAndRootVerifySignature(env.prepareFor(identifier, msgID, 1, 1, root), 1, root))

			// unprepared round change
			check(env.inst.validRoundChangeForDataVerifySignature(env.roundChangeFor(identifier, msgID, 2, 1, specqbft.NoRound, [32]byte{}, nil, nil), 2, nil))

			// prepared round change whose inner prepare alone has the mismatch
			prepares := []*specqbft.ProcessingMessage{
				env.prepareFor(identifier, msgID, 1, 1, root),
				env.prepareFor(own, own, 1, 2, root),
				env.prepareFor(own, own, 1, 3, root),
			}
			check(env.inst.validRoundChangeForDataVerifySignature(env.roundChangeFor(own, own, 2, 1, 1, root, value, prepares), 2, value))
		})
	}
}
