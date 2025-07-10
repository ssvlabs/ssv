package validation

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
)

func TestBuildLoggerFields(t *testing.T) {
	mv := &messageValidator{
		logger: zap.NewNop(),
	}

	t.Run("nil decoded message", func(t *testing.T) {
		fields := mv.buildLoggerFields(nil)

		require.NotNil(t, fields)
		require.Nil(t, fields.DutyExecutorID)
		require.Equal(t, spectypes.RunnerRole(0), fields.Role)
		require.Equal(t, spectypes.MsgType(0), fields.SSVMessageType)
		require.Equal(t, phase0.Slot(0), fields.Slot)
		require.Nil(t, fields.Consensus)
		require.Empty(t, fields.DutyID)
	})

	t.Run("nil SSVMessage", func(t *testing.T) {
		decodedMessage := &queue.SSVMessage{
			SSVMessage: nil,
		}

		fields := mv.buildLoggerFields(decodedMessage)

		require.NotNil(t, fields)
		require.Nil(t, fields.DutyExecutorID)
		require.Equal(t, spectypes.RunnerRole(0), fields.Role)
		require.Equal(t, spectypes.MsgType(0), fields.SSVMessageType)
		require.Equal(t, phase0.Slot(0), fields.Slot)
		require.Nil(t, fields.Consensus)
	})

	t.Run("consensus message with valid data", func(t *testing.T) {
		consensusMsg := &specqbft.Message{
			MsgType:    specqbft.CommitMsgType,
			Height:     12345,
			Round:      3,
			Identifier: []byte("test"),
			Root:       [32]byte{1, 2, 3},
		}

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.MessageID{},
		}

		decodedMessage := &queue.SSVMessage{
			SignedSSVMessage: &spectypes.SignedSSVMessage{
				SSVMessage: ssvMsg,
			},
			SSVMessage: ssvMsg,
			Body:       consensusMsg,
		}

		fields := mv.buildLoggerFields(decodedMessage)

		require.NotNil(t, fields)
		require.Equal(t, phase0.Slot(12345), fields.Slot)
		require.NotNil(t, fields.Consensus)
		require.Equal(t, specqbft.Round(3), fields.Consensus.Round)
		require.Equal(t, specqbft.CommitMsgType, fields.Consensus.QBFTMessageType)
		require.Equal(t, spectypes.SSVConsensusMsgType, fields.SSVMessageType)
	})

	t.Run("consensus message with nil body", func(t *testing.T) {
		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.MessageID{},
		}

		decodedMessage := &queue.SSVMessage{
			SignedSSVMessage: &spectypes.SignedSSVMessage{
				SSVMessage: ssvMsg,
			},
			SSVMessage: ssvMsg,
			Body:       nil,
		}

		fields := mv.buildLoggerFields(decodedMessage)

		require.NotNil(t, fields)
		require.Equal(t, phase0.Slot(0), fields.Slot)
		require.Nil(t, fields.Consensus)
		require.Equal(t, spectypes.SSVConsensusMsgType, fields.SSVMessageType)
	})

	t.Run("consensus message with typed nil body", func(t *testing.T) {
		var nilConsensusMsg *specqbft.Message = nil

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.MessageID{},
		}

		decodedMessage := &queue.SSVMessage{
			SignedSSVMessage: &spectypes.SignedSSVMessage{
				SSVMessage: ssvMsg,
			},
			SSVMessage: ssvMsg,
			Body:       nilConsensusMsg,
		}

		fields := mv.buildLoggerFields(decodedMessage)

		require.NotNil(t, fields)
		require.Equal(t, phase0.Slot(0), fields.Slot)
		require.Nil(t, fields.Consensus)
	})

	t.Run("partial signature message with valid data", func(t *testing.T) {
		partialSigMsg := &spectypes.PartialSignatureMessages{
			Type: spectypes.SelectionProofPartialSig,
			Slot: 67890,
			Messages: []*spectypes.PartialSignatureMessage{
				{
					PartialSignature: make([]byte, 96),
					SigningRoot:      [32]byte{4, 5, 6},
					Signer:           1,
				},
			},
		}

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   spectypes.MessageID{},
		}

		decodedMessage := &queue.SSVMessage{
			SignedSSVMessage: &spectypes.SignedSSVMessage{
				SSVMessage: ssvMsg,
			},
			SSVMessage: ssvMsg,
			Body:       partialSigMsg,
		}

		fields := mv.buildLoggerFields(decodedMessage)

		require.NotNil(t, fields)
		require.Equal(t, phase0.Slot(67890), fields.Slot)
		require.Nil(t, fields.Consensus)
		require.Equal(t, spectypes.SSVPartialSignatureMsgType, fields.SSVMessageType)
	})

	t.Run("partial signature message with nil body", func(t *testing.T) {
		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   spectypes.MessageID{},
		}

		decodedMessage := &queue.SSVMessage{
			SignedSSVMessage: &spectypes.SignedSSVMessage{
				SSVMessage: ssvMsg,
			},
			SSVMessage: ssvMsg,
			Body:       nil,
		}

		fields := mv.buildLoggerFields(decodedMessage)

		require.NotNil(t, fields)
		require.Equal(t, phase0.Slot(0), fields.Slot, "slot should be 0 when body is nil")
		require.Nil(t, fields.Consensus)
		require.Equal(t, spectypes.SSVPartialSignatureMsgType, fields.SSVMessageType)
	})

	t.Run("partial signature message with typed nil body", func(t *testing.T) {
		var nilPartialSigMsg *spectypes.PartialSignatureMessages = nil

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   spectypes.MessageID{},
		}

		decodedMessage := &queue.SSVMessage{
			SignedSSVMessage: &spectypes.SignedSSVMessage{
				SSVMessage: ssvMsg,
			},
			SSVMessage: ssvMsg,
			Body:       nilPartialSigMsg,
		}

		fields := mv.buildLoggerFields(decodedMessage)

		require.NotNil(t, fields)
		require.Equal(t, phase0.Slot(0), fields.Slot, "slot should be 0 when body is typed nil")
		require.Nil(t, fields.Consensus)
	})

	t.Run("unknown message type", func(t *testing.T) {
		// Test with a body type that's not handled by the switch
		unknownBody := "unknown body type"

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.MsgType(99), // Unknown type
			MsgID:   spectypes.MessageID{},
		}

		decodedMessage := &queue.SSVMessage{
			SignedSSVMessage: &spectypes.SignedSSVMessage{
				SSVMessage: ssvMsg,
			},
			SSVMessage: ssvMsg,
			Body:       unknownBody,
		}

		fields := mv.buildLoggerFields(decodedMessage)

		require.NotNil(t, fields)
		require.Equal(t, phase0.Slot(0), fields.Slot)
		require.Nil(t, fields.Consensus)
		require.Equal(t, spectypes.MsgType(99), fields.SSVMessageType)
	})
}

// TestBuildLoggerFields_RegressionConsensusFieldsOnlyForConsensus verifies the fix for the bug
// where consensus fields (like qbft_message_type: "proposal") appeared in logs for partial signature messages
func TestBuildLoggerFields_RegressionConsensusFieldsOnlyForConsensus(t *testing.T) {
	mv := &messageValidator{
		logger: zap.NewNop(),
	}

	// This test ensures that the bug where partial signature messages showed
	// consensus fields like "qbft_message_type: proposal" does not resurface

	t.Run("bug_regression_partial_sig_no_consensus_fields", func(t *testing.T) {
		// Before the fix, this would have consensus fields initialized to zero values
		// which resulted in logs showing: qbft_message_type: "proposal" (because 0 = proposal)

		partialSigMsg := &spectypes.PartialSignatureMessages{
			Type: spectypes.SelectionProofPartialSig,
			Slot: 12345,
			Messages: []*spectypes.PartialSignatureMessage{
				{
					PartialSignature: make([]byte, 96),
					SigningRoot:      [32]byte{1, 2, 3},
					Signer:           1,
				},
			},
		}

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   spectypes.MessageID{},
		}

		decodedMessage := &queue.SSVMessage{
			SignedSSVMessage: &spectypes.SignedSSVMessage{
				SSVMessage: ssvMsg,
			},
			SSVMessage: ssvMsg,
			Body:       partialSigMsg,
		}

		fields := mv.buildLoggerFields(decodedMessage)

		// The fix: consensus fields MUST be nil for partial signature messages
		require.Nil(t, fields.Consensus,
			"BUG REGRESSION: Consensus fields appeared for partial signature message! "+
				"This would cause logs to show qbft_message_type for non-consensus messages.")

		// Verify the fields that should be present
		require.Equal(t, phase0.Slot(12345), fields.Slot)
		require.Equal(t, spectypes.SSVPartialSignatureMsgType, fields.SSVMessageType)

		// Double-check by converting to zap fields
		zapFields := fields.AsZapFields()
		for _, field := range zapFields {
			require.NotEqual(t, "qbft_message_type", field.Key,
				"BUG REGRESSION: qbft_message_type field found in partial signature message logs!")
			require.NotEqual(t, "round", field.Key,
				"BUG REGRESSION: round field found in partial signature message logs!")
		}
	})

	t.Run("consensus_message_has_consensus_fields", func(t *testing.T) {
		// Verify that consensus messages DO have consensus fields
		consensusMsg := &specqbft.Message{
			MsgType:    specqbft.ProposalMsgType,
			Height:     12345,
			Round:      1,
			Identifier: []byte("test"),
		}

		ssvMsg := &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.MessageID{},
		}

		decodedMessage := &queue.SSVMessage{
			SignedSSVMessage: &spectypes.SignedSSVMessage{
				SSVMessage: ssvMsg,
			},
			SSVMessage: ssvMsg,
			Body:       consensusMsg,
		}

		fields := mv.buildLoggerFields(decodedMessage)

		require.NotNil(t, fields.Consensus, "Consensus fields must be present for consensus messages")
		require.Equal(t, specqbft.ProposalMsgType, fields.Consensus.QBFTMessageType)
		require.Equal(t, specqbft.Round(1), fields.Consensus.Round)
	})
}

func TestAsZapFields(t *testing.T) {
	t.Run("with consensus fields", func(t *testing.T) {
		lf := LoggerFields{
			DutyExecutorID: []byte{1, 2, 3},
			Role:           spectypes.RoleAggregator,
			SSVMessageType: spectypes.SSVConsensusMsgType,
			Slot:           12345,
			Consensus: &ConsensusFields{
				Round:           3,
				QBFTMessageType: specqbft.ProposalMsgType,
			},
			DutyID: "test-duty-id",
		}

		fields := lf.AsZapFields()

		// Should have base fields + consensus fields + duty ID
		require.Len(t, fields, 7) // DutyExecutorID, Role, SSVMessageType, Slot, DutyID, Round, QBFTMessageType
	})

	t.Run("without consensus fields", func(t *testing.T) {
		lf := LoggerFields{
			DutyExecutorID: []byte{1, 2, 3},
			Role:           spectypes.RoleAggregator,
			SSVMessageType: spectypes.SSVPartialSignatureMsgType,
			Slot:           12345,
			Consensus:      nil,
			DutyID:         "",
		}

		fields := lf.AsZapFields()

		// Should only have base fields (no consensus, no duty ID)
		require.Len(t, fields, 4) // DutyExecutorID, Role, SSVMessageType, Slot
	})

	t.Run("minimal fields", func(t *testing.T) {
		lf := LoggerFields{}

		fields := lf.AsZapFields()

		// Should have minimum required fields
		require.Len(t, fields, 4) // DutyExecutorID, Role, SSVMessageType, Slot (all with zero values)
	})
}
