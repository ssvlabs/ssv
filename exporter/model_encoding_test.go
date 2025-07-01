package exporter

import (
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
