package instance

import (
	"testing"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

// TestJustificationPlacement mirrors the placement cases of ssv-spec#673: only a proposal carries prepare
// justifications, only a proposal or a prepared round change carries round change justifications, justifications
// carry no full data, and the same holds for prepares and round changes embedded in justifications.
// Every case checks that the message is accepted without the defect and rejected with it, also before a proposal
// is accepted.
func TestJustificationPlacement(t *testing.T) {
	fullData := []byte("value")

	// withFullData encodes msgs as justifications that keep their full data
	withFullData := func(t *testing.T, msgs []*specqbft.ProcessingMessage) [][]byte {
		ret := make([][]byte, 0, len(msgs))
		for _, msg := range msgs {
			signed := msg.SignedMessage.DeepCopy()
			signed.FullData = fullData
			b, err := signed.MarshalSSZ()
			require.NoError(t, err)
			ret = append(ret, b)
		}
		return ret
	}

	tests := []struct {
		name string
		// build returns the same message without and with the defect
		build func(env *instanceTestEnv) (good, bad *specqbft.ProcessingMessage)
	}{
		{"prepare with prepare justifications", func(env *instanceTestEnv) (_, _ *specqbft.ProcessingMessage) {
			root := env.hash(fullData)
			good := env.prepare(1, 2, root)
			return good, env.resign(good, func(m *specqbft.Message) {
				m.PrepareJustification = env.marshalJustifications([]*specqbft.ProcessingMessage{env.prepare(1, 3, root)})
			})
		}},
		{"commit with prepare justifications", func(env *instanceTestEnv) (_, _ *specqbft.ProcessingMessage) {
			root := env.hash(fullData)
			good := env.commit(1, 2, root)
			return good, env.resign(good, func(m *specqbft.Message) {
				m.PrepareJustification = env.marshalJustifications([]*specqbft.ProcessingMessage{env.prepare(1, 3, root)})
			})
		}},
		{"round change with prepare justifications", func(env *instanceTestEnv) (_, _ *specqbft.ProcessingMessage) {
			good := env.roundChange(2, 2, specqbft.NoRound, [32]byte{}, nil, nil)
			return good, env.resign(good, func(m *specqbft.Message) {
				m.PrepareJustification = env.marshalJustifications([]*specqbft.ProcessingMessage{env.prepare(1, 3, env.hash(fullData))})
			})
		}},
		{"prepared round change with prepare justifications", func(env *instanceTestEnv) (_, _ *specqbft.ProcessingMessage) {
			rcs, prepares := env.preparedRoundChangeSet(2, 1, fullData, []spectypes.OperatorID{1, 2, 3}, []spectypes.OperatorID{2})
			return rcs[0], env.resign(rcs[0], func(m *specqbft.Message) {
				m.PrepareJustification = env.marshalJustifications(prepares)
			})
		}},
		{"prepare with round change justifications", func(env *instanceTestEnv) (_, _ *specqbft.ProcessingMessage) {
			root := env.hash(fullData)
			good := env.prepare(1, 2, root)
			return good, env.resign(good, func(m *specqbft.Message) {
				m.RoundChangeJustification = env.marshalJustifications([]*specqbft.ProcessingMessage{env.prepare(1, 3, root)})
			})
		}},
		{"commit with round change justifications", func(env *instanceTestEnv) (_, _ *specqbft.ProcessingMessage) {
			root := env.hash(fullData)
			good := env.commit(1, 2, root)
			return good, env.resign(good, func(m *specqbft.Message) {
				m.RoundChangeJustification = env.marshalJustifications([]*specqbft.ProcessingMessage{env.prepare(1, 3, root)})
			})
		}},
		{"not prepared round change with round change justifications", func(env *instanceTestEnv) (_, _ *specqbft.ProcessingMessage) {
			good := env.roundChange(2, 2, specqbft.NoRound, [32]byte{}, nil, nil)
			return good, env.resign(good, func(m *specqbft.Message) {
				m.RoundChangeJustification = env.marshalJustifications([]*specqbft.ProcessingMessage{env.prepare(1, 3, env.hash(fullData))})
			})
		}},
		{"prepared round change justification with full data", func(env *instanceTestEnv) (_, _ *specqbft.ProcessingMessage) {
			rcs, prepares := env.preparedRoundChangeSet(2, 1, fullData, []spectypes.OperatorID{1, 2, 3}, []spectypes.OperatorID{2})
			return rcs[0], env.resign(rcs[0], func(m *specqbft.Message) {
				m.RoundChangeJustification = withFullData(env.t, prepares)
			})
		}},
		{"proposal justification with full data", func(env *instanceTestEnv) (_, _ *specqbft.ProcessingMessage) {
			rcs, prepares := env.preparedRoundChangeSet(2, 1, fullData, []spectypes.OperatorID{1, 2, 3}, []spectypes.OperatorID{1, 2, 3})
			good := env.proposal(2, 1, fullData, env.hash(fullData), rcs, prepares)
			return good, env.resign(good, func(m *specqbft.Message) {
				m.PrepareJustification = withFullData(env.t, prepares)
			})
		}},
		{"prepared round change, embedded prepare with prepare justifications", func(env *instanceTestEnv) (_, _ *specqbft.ProcessingMessage) {
			rcs, prepares := env.preparedRoundChangeSet(2, 1, fullData, []spectypes.OperatorID{1, 2, 3}, []spectypes.OperatorID{2})
			return rcs[0], env.resign(rcs[0], func(m *specqbft.Message) {
				m.RoundChangeJustification = env.marshalJustifications(env.nestedPrepares(prepares))
			})
		}},
		{"proposal, prepare justification with prepare justifications", func(env *instanceTestEnv) (_, _ *specqbft.ProcessingMessage) {
			rcs, prepares := env.preparedRoundChangeSet(2, 1, fullData, []spectypes.OperatorID{1, 2, 3}, []spectypes.OperatorID{1, 2, 3})
			good := env.proposal(2, 1, fullData, env.hash(fullData), rcs, prepares)
			return good, env.resign(good, func(m *specqbft.Message) {
				m.PrepareJustification = env.marshalJustifications(env.nestedPrepares(prepares))
			})
		}},
		{"proposal, embedded round change's prepare with prepare justifications", func(env *instanceTestEnv) (_, _ *specqbft.ProcessingMessage) {
			rcs, prepares := env.preparedRoundChangeSet(2, 1, fullData, []spectypes.OperatorID{1, 2, 3}, []spectypes.OperatorID{1, 2, 3})
			good := env.proposal(2, 1, fullData, env.hash(fullData), rcs, prepares)
			nested := env.nestedPrepares(prepares)
			badRCs := make([]*specqbft.ProcessingMessage, 0, len(rcs))
			for _, rc := range rcs {
				badRCs = append(badRCs, env.resign(rc, func(m *specqbft.Message) {
					m.RoundChangeJustification = env.marshalJustifications(nested)
				}))
			}
			return good, env.resign(good, func(m *specqbft.Message) {
				m.RoundChangeJustification = env.marshalJustifications(badRCs)
			})
		}},
		{"proposal, embedded not prepared round change with round change justifications", func(env *instanceTestEnv) (_, _ *specqbft.ProcessingMessage) {
			rcs := []*specqbft.ProcessingMessage{
				env.roundChange(2, 1, specqbft.NoRound, [32]byte{}, nil, nil),
				env.roundChange(2, 2, specqbft.NoRound, [32]byte{}, nil, nil),
				env.roundChange(2, 3, specqbft.NoRound, [32]byte{}, nil, nil),
				env.roundChange(2, 4, specqbft.NoRound, [32]byte{}, nil, nil),
			}
			good := env.proposal(2, 1, fullData, env.hash(fullData), rcs, nil)
			return good, env.resign(good, func(m *specqbft.Message) {
				rcs[3] = env.resign(rcs[3], func(m *specqbft.Message) {
					m.RoundChangeJustification = env.marshalJustifications([]*specqbft.ProcessingMessage{env.prepare(1, 1, env.hash(fullData))})
				})
				m.RoundChangeJustification = env.marshalJustifications(rcs)
			})
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newInstanceTestEnv(t, 1)
			env.inst.State.ProposalAcceptedForCurrentRound = env.proposal(1, 1, fullData, env.hash(fullData), nil, nil)

			good, bad := tt.build(env)
			require.NoError(t, env.inst.BaseMsgValidation(good))
			require.Error(t, env.inst.BaseMsgValidation(bad))

			// rejected outright, not deferred until a proposal arrives
			env.inst.State.ProposalAcceptedForCurrentRound = nil
			err := env.inst.BaseMsgValidation(bad)
			require.Error(t, err)
			require.False(t, IsRetryable(err))
		})
	}
}

// resign re-signs msg by the same signer after applying edit to a copy of its QBFT message
func (e *instanceTestEnv) resign(msg *specqbft.ProcessingMessage, edit func(m *specqbft.Message)) *specqbft.ProcessingMessage {
	m := *msg.QBFTMessage
	edit(&m)
	return e.processingMessage(&m, msg.SignedMessage.OperatorIDs[0], msg.SignedMessage.FullData)
}

// nestedPrepares returns prepares where the last one carries a prepare justification
func (e *instanceTestEnv) nestedPrepares(prepares []*specqbft.ProcessingMessage) []*specqbft.ProcessingMessage {
	ret := append([]*specqbft.ProcessingMessage{}, prepares...)
	last := len(ret) - 1
	ret[last] = e.resign(ret[last], func(m *specqbft.Message) {
		m.PrepareJustification = e.marshalJustifications(prepares[:1])
	})
	return ret
}

// TestDecidedCommitJustificationPlacement covers the commit validation the controller runs on decided messages
func TestDecidedCommitJustificationPlacement(t *testing.T) {
	env := newInstanceTestEnv(t, 1)
	root := env.hash([]byte("value"))
	committee := env.inst.State.CommitteeMember.Committee

	good := env.commit(1, 2, root)
	require.NoError(t, BaseCommitValidationVerifySignature(good, env.inst.State.Height, committee))

	bad := env.resign(good, func(m *specqbft.Message) {
		m.PrepareJustification = env.marshalJustifications([]*specqbft.ProcessingMessage{env.prepare(1, 3, root)})
	})
	require.Error(t, BaseCommitValidationVerifySignature(bad, env.inst.State.Height, committee))
}
