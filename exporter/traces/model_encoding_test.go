package traces

import (
	"encoding/binary"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func TestValidatorDutyTrace_MarshallSSZ(t *testing.T) {
	trace := &ValidatorDutyTrace{
		ConsensusTrace: ConsensusTrace{
			Rounds:   []*RoundTrace{makeRoundTrace()},
			Decideds: []*DecidedTrace{makeDecidedTrace()},
		},
		Slot:         123,
		Role:         spectypes.BNRoleProposer,
		Validator:    456,
		Pre:          []*PartialSigTrace{makePartialSigTrace()},
		Post:         []*PartialSigTrace{makePartialSigTrace()},
		ProposalData: []byte{1, 2, 3},
	}

	// Test MarshalSSZ
	encoded, err := trace.MarshalSSZ()
	require.NoError(t, err)
	require.NotNil(t, encoded)

	// Test UnmarshalSSZ
	decoded := &ValidatorDutyTrace{}
	err = decoded.UnmarshalSSZ(encoded)
	require.NoError(t, err)
	require.Equal(t, trace, decoded)

	// Test SizeSSZ
	size := trace.SizeSSZ()
	require.Equal(t, len(encoded), size)

	// Test HashTreeRoot
	root, err := trace.HashTreeRoot()
	require.NoError(t, err)
	require.NotNil(t, root)
}

func TestCommitteeDutyTrace_MarshallSSZ(t *testing.T) {
	trace := &CommitteeDutyTrace{
		ConsensusTrace: ConsensusTrace{
			Rounds:   []*RoundTrace{makeRoundTrace()},
			Decideds: []*DecidedTrace{makeDecidedTrace()},
		},
		Slot:          123,
		CommitteeID:   [32]byte{1, 2, 3},
		OperatorIDs:   []spectypes.OperatorID{1, 2, 3},
		ProposalData:  []byte("test data"),
		SyncCommittee: []*SignerData{makeSignerData()},
		Attester:      []*SignerData{makeSignerData()},
	}

	encoded, err := trace.MarshalSSZ()
	require.NoError(t, err)
	require.NotNil(t, encoded)

	decoded := &CommitteeDutyTrace{}
	err = decoded.UnmarshalSSZ(encoded)
	require.NoError(t, err)
	require.Equal(t, trace, decoded)

	size := trace.SizeSSZ()
	require.Equal(t, len(encoded), size)

	root, err := trace.HashTreeRoot()
	require.NoError(t, err)
	require.NotNil(t, root)
}

func TestCommitteeDutyTrace_MarshallSSZ_RoleAggregatorCommittee(t *testing.T) {
	trace := &CommitteeDutyTrace{
		ConsensusTrace: ConsensusTrace{
			Rounds:   []*RoundTrace{makeRoundTrace()},
			Decideds: []*DecidedTrace{makeDecidedTrace()},
		},
		Slot:          789,
		Role:          spectypes.RoleAggregatorCommittee,
		CommitteeID:   [32]byte{4, 5, 6},
		OperatorIDs:   []spectypes.OperatorID{4, 5, 6},
		ProposalData:  []byte("aggregator committee data"),
		SyncCommittee: []*SignerData{makeSignerData()},
		Attester:      []*SignerData{makeSignerData()},
	}

	encoded, err := trace.MarshalSSZ()
	require.NoError(t, err)
	require.NotNil(t, encoded)

	decoded := &CommitteeDutyTrace{}
	err = decoded.UnmarshalSSZ(encoded)
	require.NoError(t, err)
	require.Equal(t, trace, decoded)
	require.Equal(t, spectypes.RoleAggregatorCommittee, decoded.Role)

	size := trace.SizeSSZ()
	require.Equal(t, len(encoded), size)

	root, err := trace.HashTreeRoot()
	require.NoError(t, err)
	require.NotNil(t, root)
}

func TestCommitteeDutyTrace_HashTreeRoot_StableAndSensitiveToChanges(t *testing.T) {
	base := func() *CommitteeDutyTrace {
		return &CommitteeDutyTrace{
			ConsensusTrace: ConsensusTrace{
				Rounds:   []*RoundTrace{makeRoundTrace()},
				Decideds: []*DecidedTrace{makeDecidedTrace()},
			},
			Slot:          123,
			Role:          spectypes.RoleCommittee,
			CommitteeID:   [32]byte{1, 2, 3},
			OperatorIDs:   []spectypes.OperatorID{1, 2, 3},
			ProposalData:  []byte("test data"),
			SyncCommittee: []*SignerData{makeSignerData()},
			Attester:      []*SignerData{makeSignerData()},
		}
	}

	trace := base()
	root1, err := trace.HashTreeRoot()
	require.NoError(t, err)
	root2, err := trace.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, root1, root2, "HashTreeRoot must be deterministic across calls")

	changed := base()
	changed.Role = spectypes.RoleAggregatorCommittee
	rootChanged, err := changed.HashTreeRoot()
	require.NoError(t, err)
	require.NotEqual(t, root1, rootChanged, "changing Role must change the hash tree root")
}

func TestCommitteeDutyTrace_UnmarshalSSZ_Errors(t *testing.T) {
	valid := &CommitteeDutyTrace{
		ConsensusTrace: ConsensusTrace{
			Rounds:   []*RoundTrace{makeRoundTrace()},
			Decideds: []*DecidedTrace{makeDecidedTrace()},
		},
		Slot:          123,
		Role:          spectypes.RoleCommittee,
		CommitteeID:   [32]byte{1, 2, 3},
		OperatorIDs:   []spectypes.OperatorID{1, 2, 3},
		ProposalData:  []byte("test data"),
		SyncCommittee: []*SignerData{makeSignerData()},
		Attester:      []*SignerData{makeSignerData()},
	}
	encoded, err := valid.MarshalSSZ()
	require.NoError(t, err)

	t.Run("truncated buffer smaller than fixed header returns error, not panic", func(t *testing.T) {
		decoded := &CommitteeDutyTrace{}
		err := decoded.UnmarshalSSZ(encoded[:10])
		require.Error(t, err)
	})

	t.Run("empty buffer returns error, not panic", func(t *testing.T) {
		decoded := &CommitteeDutyTrace{}
		err := decoded.UnmarshalSSZ(nil)
		require.Error(t, err)
	})

	t.Run("truncated buffer within variable-length section returns error, not panic", func(t *testing.T) {
		decoded := &CommitteeDutyTrace{}
		err := decoded.UnmarshalSSZ(encoded[:len(encoded)-5])
		require.Error(t, err)
	})

	t.Run("corrupted first offset returns ErrInvalidVariableOffset", func(t *testing.T) {
		corrupted := make([]byte, len(encoded))
		copy(corrupted, encoded)
		// Offset (0) 'Rounds' must equal exactly 72; corrupt it while staying <= size.
		binary.LittleEndian.PutUint32(corrupted[0:4], 71)
		decoded := &CommitteeDutyTrace{}
		err := decoded.UnmarshalSSZ(corrupted)
		require.Error(t, err)
	})

	t.Run("offset ordering violation (OperatorIDs offset before Decideds offset) returns error", func(t *testing.T) {
		corrupted := make([]byte, len(encoded))
		copy(corrupted, encoded)
		// Offset (5) 'OperatorIDs' at bytes [56:60]; set it below Offset(1) 'Decideds' to violate ordering.
		binary.LittleEndian.PutUint32(corrupted[56:60], 0)
		decoded := &CommitteeDutyTrace{}
		err := decoded.UnmarshalSSZ(corrupted)
		require.Error(t, err)
	})

	t.Run("offset ordering violation (ProposalData offset before OperatorIDs offset) returns error", func(t *testing.T) {
		corrupted := make([]byte, len(encoded))
		copy(corrupted, encoded)
		// Offset (6) 'ProposalData' at bytes [60:64]; set it below Offset(5) 'OperatorIDs' to violate ordering.
		binary.LittleEndian.PutUint32(corrupted[60:64], 0)
		decoded := &CommitteeDutyTrace{}
		err := decoded.UnmarshalSSZ(corrupted)
		require.Error(t, err)
	})

	t.Run("offset ordering violation (SyncCommittee offset before ProposalData offset) returns error", func(t *testing.T) {
		corrupted := make([]byte, len(encoded))
		copy(corrupted, encoded)
		// Offset (7) 'SyncCommittee' at bytes [64:68]; set it below Offset(6) 'ProposalData' to violate ordering.
		binary.LittleEndian.PutUint32(corrupted[64:68], 0)
		decoded := &CommitteeDutyTrace{}
		err := decoded.UnmarshalSSZ(corrupted)
		require.Error(t, err)
	})

	t.Run("offset ordering violation (Attester offset before SyncCommittee offset) returns error", func(t *testing.T) {
		corrupted := make([]byte, len(encoded))
		copy(corrupted, encoded)
		// Offset (8) 'Attester' at bytes [68:72]; set it below Offset(7) 'SyncCommittee' to violate ordering.
		binary.LittleEndian.PutUint32(corrupted[68:72], 0)
		decoded := &CommitteeDutyTrace{}
		err := decoded.UnmarshalSSZ(corrupted)
		require.Error(t, err)
	})
}

func TestDiskMsg_MarshallSSZ(t *testing.T) {
	sig := [96]byte{1, 2, 3}
	msg := &DiskMsg{
		Kind: 1,
		Signed: spectypes.SignedSSVMessage{
			Signatures:  [][]byte{{1, 2, 3}},
			OperatorIDs: []spectypes.OperatorID{1, 2, 3},
			SSVMessage: &spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   spectypes.NewMsgID(spectypes.GenesisMainnet, []byte{1, 2, 3}, spectypes.RoleProposer),
				Data:    []byte{1, 2, 3},
			},
			FullData: []byte{1, 2, 3},
		},
		Spec: spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.GenesisMainnet, []byte{1, 2, 3}, spectypes.RoleProposer),
			Data:    []byte{1, 2, 3},
		},
		Qbft: specqbft.Message{
			MsgType:                  specqbft.ProposalMsgType,
			Height:                   1,
			Round:                    1,
			DataRound:                1,
			Identifier:               []byte{1, 2, 3},
			Root:                     [32]byte{1, 2, 3},
			RoundChangeJustification: [][]byte{{1, 2, 3}},
			PrepareJustification:     [][]byte{{1, 2, 3}},
		},
		Sig: spectypes.PartialSignatureMessages{
			Slot: 1,
			Type: spectypes.PostConsensusPartialSig,
			Messages: []*spectypes.PartialSignatureMessage{
				{
					PartialSignature: sig[:],
					SigningRoot:      [32]byte{1, 2, 3},
					Signer:           1,
					ValidatorIndex:   1,
				},
			},
		},
	}

	encoded, err := msg.MarshalSSZ()
	require.NoError(t, err)
	require.NotNil(t, encoded)

	decoded := &DiskMsg{}
	err = decoded.UnmarshalSSZ(encoded)
	require.NoError(t, err)
	require.Equal(t, msg, decoded)

	size := msg.SizeSSZ()
	require.Equal(t, len(encoded), size)

	root, err := msg.HashTreeRoot()
	require.NoError(t, err)
	require.NotNil(t, root)
}

func makeSignerData() *SignerData {
	return &SignerData{
		Signer:       1,
		ValidatorIdx: []phase0.ValidatorIndex{1, 2, 3},
		ReceivedTime: 1234567890,
	}
}

// sample data
func makeQBFTTrace() *QBFTTrace {
	return &QBFTTrace{
		Round:        1,
		BeaconRoot:   [32]byte{1, 2, 3},
		Signer:       1,
		ReceivedTime: 1234567890,
	}
}

func makeRoundTrace() *RoundTrace {
	return &RoundTrace{
		Proposer: 1,
		ProposalTrace: &ProposalTrace{
			QBFTTrace:       *makeQBFTTrace(),
			RoundChanges:    []*RoundChangeTrace{makeRoundChangeTrace()},
			PrepareMessages: []*QBFTTrace{makeQBFTTrace()},
		},
		Prepares:     []*QBFTTrace{makeQBFTTrace()},
		Commits:      []*QBFTTrace{makeQBFTTrace()},
		RoundChanges: []*RoundChangeTrace{makeRoundChangeTrace()},
	}
}

func makeRoundChangeTrace() *RoundChangeTrace {
	return &RoundChangeTrace{
		QBFTTrace:       *makeQBFTTrace(),
		PreparedRound:   1,
		PrepareMessages: []*QBFTTrace{makeQBFTTrace()},
	}
}

func makeDecidedTrace() *DecidedTrace {
	return &DecidedTrace{
		Round:        1,
		BeaconRoot:   [32]byte{1, 2, 3},
		Signers:      []spectypes.OperatorID{1, 2, 3},
		ReceivedTime: 1234567890,
	}
}

func makePartialSigTrace() *PartialSigTrace {
	return &PartialSigTrace{
		Type:         spectypes.PostConsensusPartialSig,
		BeaconRoot:   [32]byte{1, 2, 3},
		Signer:       1,
		ReceivedTime: 1234567890,
	}
}

func TestValidatorDutyTrace_UnmarshalSSZ_Errors(t *testing.T) {
	trace := &ValidatorDutyTrace{
		ConsensusTrace: ConsensusTrace{
			Rounds:   []*RoundTrace{makeRoundTrace()},
			Decideds: []*DecidedTrace{makeDecidedTrace()},
		},
		Slot:         123,
		Role:         spectypes.BNRoleProposer,
		Validator:    456,
		Pre:          []*PartialSigTrace{makePartialSigTrace()},
		Post:         []*PartialSigTrace{makePartialSigTrace()},
		ProposalData: []byte{1, 2, 3},
	}
	encoded, err := trace.MarshalSSZ()
	require.NoError(t, err)

	t.Run("truncated buffer returns error, not panic", func(t *testing.T) {
		decoded := &ValidatorDutyTrace{}
		require.Error(t, decoded.UnmarshalSSZ(encoded[:5]))
	})

	t.Run("corrupted Rounds offset returns ErrInvalidVariableOffset", func(t *testing.T) {
		corrupted := make([]byte, len(encoded))
		copy(corrupted, encoded)
		binary.LittleEndian.PutUint32(corrupted[0:4], 43)
		decoded := &ValidatorDutyTrace{}
		require.Error(t, decoded.UnmarshalSSZ(corrupted))
	})
}

func TestDecidedTrace_UnmarshalSSZ_Errors(t *testing.T) {
	d := makeDecidedTrace()
	encoded, err := d.MarshalSSZ()
	require.NoError(t, err)

	t.Run("truncated buffer returns error, not panic", func(t *testing.T) {
		decoded := &DecidedTrace{}
		require.Error(t, decoded.UnmarshalSSZ(encoded[:5]))
	})

	t.Run("corrupted Signers offset returns ErrInvalidVariableOffset", func(t *testing.T) {
		corrupted := make([]byte, len(encoded))
		copy(corrupted, encoded)
		binary.LittleEndian.PutUint32(corrupted[40:44], 51)
		decoded := &DecidedTrace{}
		require.Error(t, decoded.UnmarshalSSZ(corrupted))
	})
}

func TestRoundTrace_UnmarshalSSZ_Errors(t *testing.T) {
	r := makeRoundTrace()
	encoded, err := r.MarshalSSZ()
	require.NoError(t, err)

	t.Run("truncated buffer returns error, not panic", func(t *testing.T) {
		decoded := &RoundTrace{}
		require.Error(t, decoded.UnmarshalSSZ(encoded[:5]))
	})

	t.Run("corrupted ProposalTrace offset returns ErrInvalidVariableOffset", func(t *testing.T) {
		corrupted := make([]byte, len(encoded))
		copy(corrupted, encoded)
		binary.LittleEndian.PutUint32(corrupted[8:12], 23)
		decoded := &RoundTrace{}
		require.Error(t, decoded.UnmarshalSSZ(corrupted))
	})
}

func TestRoundChangeTrace_UnmarshalSSZ_Errors(t *testing.T) {
	r := makeRoundChangeTrace()
	encoded, err := r.MarshalSSZ()
	require.NoError(t, err)

	t.Run("truncated buffer returns error, not panic", func(t *testing.T) {
		decoded := &RoundChangeTrace{}
		require.Error(t, decoded.UnmarshalSSZ(encoded[:5]))
	})

	t.Run("corrupted PrepareMessages offset returns ErrInvalidVariableOffset", func(t *testing.T) {
		corrupted := make([]byte, len(encoded))
		copy(corrupted, encoded)
		binary.LittleEndian.PutUint32(corrupted[64:68], 67)
		decoded := &RoundChangeTrace{}
		require.Error(t, decoded.UnmarshalSSZ(corrupted))
	})
}

func TestProposalTrace_UnmarshalSSZ_Errors(t *testing.T) {
	p := &ProposalTrace{
		QBFTTrace:       *makeQBFTTrace(),
		RoundChanges:    []*RoundChangeTrace{makeRoundChangeTrace()},
		PrepareMessages: []*QBFTTrace{makeQBFTTrace()},
	}
	encoded, err := p.MarshalSSZ()
	require.NoError(t, err)

	t.Run("truncated buffer returns error, not panic", func(t *testing.T) {
		decoded := &ProposalTrace{}
		require.Error(t, decoded.UnmarshalSSZ(encoded[:5]))
	})

	t.Run("corrupted RoundChanges offset returns ErrInvalidVariableOffset", func(t *testing.T) {
		corrupted := make([]byte, len(encoded))
		copy(corrupted, encoded)
		binary.LittleEndian.PutUint32(corrupted[56:60], 63)
		decoded := &ProposalTrace{}
		require.Error(t, decoded.UnmarshalSSZ(corrupted))
	})
}

func TestSignerData_UnmarshalSSZ_Errors(t *testing.T) {
	s := makeSignerData()
	encoded, err := s.MarshalSSZ()
	require.NoError(t, err)

	t.Run("truncated buffer returns error, not panic", func(t *testing.T) {
		decoded := &SignerData{}
		require.Error(t, decoded.UnmarshalSSZ(encoded[:5]))
	})

	t.Run("corrupted ValidatorIdx offset returns ErrInvalidVariableOffset", func(t *testing.T) {
		corrupted := make([]byte, len(encoded))
		copy(corrupted, encoded)
		binary.LittleEndian.PutUint32(corrupted[8:12], 19)
		decoded := &SignerData{}
		require.Error(t, decoded.UnmarshalSSZ(corrupted))
	})
}

func TestSignerData_MarshalSSZ_ValidatorIdxTooBig(t *testing.T) {
	tooMany := make([]phase0.ValidatorIndex, 3001)
	s := &SignerData{
		Signer:       1,
		ValidatorIdx: tooMany,
		ReceivedTime: 1,
	}

	_, err := s.MarshalSSZ()
	require.Error(t, err)

	_, err = s.HashTreeRoot()
	require.Error(t, err)
}

func TestDiskMsg_UnmarshalSSZ_Errors(t *testing.T) {
	sig := [96]byte{1, 2, 3}
	msg := &DiskMsg{
		Kind: 1,
		Signed: spectypes.SignedSSVMessage{
			Signatures:  [][]byte{{1, 2, 3}},
			OperatorIDs: []spectypes.OperatorID{1, 2, 3},
			SSVMessage: &spectypes.SSVMessage{
				MsgType: spectypes.SSVConsensusMsgType,
				MsgID:   spectypes.NewMsgID(spectypes.GenesisMainnet, []byte{1, 2, 3}, spectypes.RoleProposer),
				Data:    []byte{1, 2, 3},
			},
			FullData: []byte{1, 2, 3},
		},
		Spec: spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   spectypes.NewMsgID(spectypes.GenesisMainnet, []byte{1, 2, 3}, spectypes.RoleProposer),
			Data:    []byte{1, 2, 3},
		},
		Qbft: specqbft.Message{
			MsgType:    specqbft.ProposalMsgType,
			Height:     1,
			Round:      1,
			Identifier: []byte{1, 2, 3},
			Root:       [32]byte{1, 2, 3},
		},
		Sig: spectypes.PartialSignatureMessages{
			Slot: 1,
			Type: spectypes.PostConsensusPartialSig,
			Messages: []*spectypes.PartialSignatureMessage{
				{
					PartialSignature: sig[:],
					SigningRoot:      [32]byte{1, 2, 3},
					Signer:           1,
					ValidatorIndex:   1,
				},
			},
		},
	}
	encoded, err := msg.MarshalSSZ()
	require.NoError(t, err)

	t.Run("truncated buffer returns error, not panic", func(t *testing.T) {
		decoded := &DiskMsg{}
		require.Error(t, decoded.UnmarshalSSZ(encoded[:5]))
	})

	t.Run("empty buffer returns error, not panic", func(t *testing.T) {
		decoded := &DiskMsg{}
		require.Error(t, decoded.UnmarshalSSZ(nil))
	})

	t.Run("corrupted Signed offset returns ErrInvalidVariableOffset", func(t *testing.T) {
		corrupted := make([]byte, len(encoded))
		copy(corrupted, encoded)
		binary.LittleEndian.PutUint32(corrupted[0:4], 16)
		decoded := &DiskMsg{}
		require.Error(t, decoded.UnmarshalSSZ(corrupted))
	})
}
