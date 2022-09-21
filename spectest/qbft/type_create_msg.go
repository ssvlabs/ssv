package qbft

import (
	"context"
	"encoding/hex"
	"github.com/bloxapp/ssv-spec/qbft"
	spectests "github.com/bloxapp/ssv-spec/qbft/spectest/tests"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont"
	msgcontinmem "github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont/inmem"
	"github.com/bloxapp/ssv/protocol/v1/validator"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"testing"
)

// RunCreateMessageSpecTest runs spec test type CreateMsgSpecTest
func RunCreateMessageSpecTest(t *testing.T, test *spectests.CreateMsgSpecTest) {
	ctx := context.TODO()
	logger := logex.Build(test.Name, zapcore.DebugLevel, nil)

	identifier := []byte{1, 2, 3, 4}

	beacon := validator.NewTestBeacon(t)
	db, _ := NewQBFTStorage(ctx, t, logger, spectypes.BNRoleAttester.String())
	defer func() {
		db.Close()
	}()

	ks := testingutils.Testing4SharesSet()
	testShare := testingutils.TestingShare(ks)
	share, keySet := ToMappedShare(t, testShare)

	// add share key to account
	require.NoError(t, beacon.KeyManager.AddShare(keySet.Shares[share.NodeID]))

	state := instance.GenerateState(&instance.Options{
		Identifier: identifier,
		Height:     0,
	})
	state.Round.Store(test.Round)
	state.PreparedValue.Store(test.Value) // TODO ok?

	qbftInstance := &instance.Instance{
		Logger:         logger,
		ValidatorShare: share,
		State:          state,
		SsvSigner:      beacon.KeyManager,
		ContainersMap: map[qbft.MessageType]msgcont.MessageContainer{
			qbft.ProposalMsgType:    msgcontinmem.New(uint64(share.ThresholdSize()), uint64(share.PartialThresholdSize())),
			qbft.PrepareMsgType:     msgcontinmem.New(uint64(share.ThresholdSize()), uint64(share.PartialThresholdSize())),
			qbft.CommitMsgType:      msgcontinmem.New(uint64(share.ThresholdSize()), uint64(share.PartialThresholdSize())),
			qbft.RoundChangeMsgType: msgcontinmem.New(uint64(share.ThresholdSize()), uint64(share.PartialThresholdSize()))},
	}

	/*for _, pj := range test.PrepareJustifications{
		pd, err := pj.Message.GetPrepareData()
		require.NoError(t, err)
		instance.ContainersMap[qbft.PrepareMsgType].AddMessage(pj, pd.Data)
	}

	for _, rcj := range test.RoundChangeJustifications{
		rcd, err := rcj.Message.GetRoundChangeData()
		require.NoError(t, err)
		instance.ContainersMap[qbft.PrepareMsgType].AddMessage(rcj, rcd.PreparedValue)
	}*/

	var msg *qbft.SignedMessage
	var lastErr error
	switch test.CreateType {
	case spectests.CreateProposal:
		msg, lastErr = createProposal(test, qbftInstance)
	case spectests.CreatePrepare:
		msg, lastErr = createPrepare(test, qbftInstance)
	case spectests.CreateCommit:
		msg, lastErr = createCommit(test, qbftInstance)
	case spectests.CreateRoundChange:
		msg, lastErr = createRoundChange(test, qbftInstance)
	default:
		t.Fail()
	}

	require.NoError(t, lastErr)

	r, err := msg.GetRoot()
	if err != nil {
		lastErr = err
	}

	if len(test.ExpectedError) != 0 {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}

	require.EqualValues(t, test.ExpectedRoot, hex.EncodeToString(r))
}

func createCommit(test *spectests.CreateMsgSpecTest, qbftInstance *instance.Instance) (*qbft.SignedMessage, error) {
	msg, err := qbftInstance.GenerateCommitMessage(test.Value)
	if err != nil {
		return nil, err
	}
	return sign(qbftInstance, msg)
}

func createPrepare(test *spectests.CreateMsgSpecTest, qbftInstance *instance.Instance) (*qbft.SignedMessage, error) {
	msg, err := qbftInstance.GeneratePrepareMessage(test.Value)
	if err != nil {
		return nil, err
	}
	return sign(qbftInstance, msg)
}

func createProposal(test *spectests.CreateMsgSpecTest, qbftInstance *instance.Instance) (*qbft.SignedMessage, error) {
	msg, err := qbftInstance.GenerateProposalMessage(&qbft.ProposalData{
		Data: test.Value,
	})
	if err != nil {
		return nil, err
	}
	return sign(qbftInstance, &msg)
}

func createRoundChange(test *spectests.CreateMsgSpecTest, qbftInstance *instance.Instance) (*qbft.SignedMessage, error) {
	if len(test.PrepareJustifications) > 0 {
		qbftInstance.State.PreparedRound.Store(test.PrepareJustifications[0].Message.Round)
		qbftInstance.State.PreparedValue.Store(test.Value)

		for _, msg := range test.PrepareJustifications {
			pd, err := msg.Message.GetPrepareData()
			if err != nil {
				return nil, err
			}
			qbftInstance.ContainersMap[qbft.PrepareMsgType].AddMessage(msg, pd.Data)
		}
	}

	msg, err := qbftInstance.GenerateChangeRoundMessage()
	if err != nil {
		return nil, err
	}
	return sign(qbftInstance, msg)
}

func sign(qbftInstance *instance.Instance, msg *qbft.Message) (*qbft.SignedMessage, error) {
	pk, err := qbftInstance.ValidatorShare.OperatorSharePubKey()
	if err != nil {
		return nil, err
	}

	sigByts, err := qbftInstance.SsvSigner.SignRoot(msg, spectypes.QBFTSignatureType, pk.Serialize())
	if err != nil {
		return nil, err
	}

	return &qbft.SignedMessage{
		Message:   msg,
		Signature: sigByts,
		Signers:   []spectypes.OperatorID{qbftInstance.ValidatorShare.NodeID},
	}, nil
}
