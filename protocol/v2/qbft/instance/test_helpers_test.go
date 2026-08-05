package instance

import (
	"bytes"
	"context"
	"crypto/rsa"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	protocolp2p "github.com/ssvlabs/ssv/protocol/v2/p2p"
	qbftconfig "github.com/ssvlabs/ssv/protocol/v2/qbft"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

type instanceTestEnv struct {
	t          *testing.T
	keys       *spectestingutils.TestKeySet
	config     *qbftconfig.Config
	inst       *Instance
	network    *testingNetwork
	roundTimer *roundtimer.TestQBFTTimer
}

// testingNetwork wraps the spec testing network to satisfy protocolp2p.Network. Defined
// locally (rather than reusing protocol/v2/testing.TestingNetwork) to avoid an import cycle:
// protocol/v2/testing imports this package (protocol/v2/qbft/instance).
type testingNetwork struct {
	*spectestingutils.TestingNetwork
}

func newTestingNetwork(operatorID spectypes.OperatorID, sk *rsa.PrivateKey) *testingNetwork {
	return &testingNetwork{TestingNetwork: spectestingutils.NewTestingNetwork(operatorID, sk)}
}

func (n *testingNetwork) Subscribe(_ spectypes.ValidatorPK) error {
	return nil
}

func (n *testingNetwork) Unsubscribe(_ spectypes.ValidatorPK) error {
	return nil
}

func (n *testingNetwork) BroadcastAtSlot(message *spectypes.SignedSSVMessage, _ phase0.Slot) error {
	return n.Broadcast(message.SSVMessage.GetID(), message)
}

func (n *testingNetwork) ReportValidation(_ *spectypes.SSVMessage, _ protocolp2p.MsgValidationResult) {
}

type recordingNetwork struct {
	broadcasted []*spectypes.SignedSSVMessage
	onBroadcast func(*spectypes.SignedSSVMessage) error
}

func (n *recordingNetwork) Broadcast(msgID spectypes.MessageID, message *spectypes.SignedSSVMessage) error {
	n.broadcasted = append(n.broadcasted, message)
	if n.onBroadcast != nil {
		return n.onBroadcast(message)
	}
	return nil
}

func (n *recordingNetwork) Subscribe(_ spectypes.ValidatorPK) error {
	return nil
}

func (n *recordingNetwork) Unsubscribe(_ spectypes.ValidatorPK) error {
	return nil
}

func (n *recordingNetwork) BroadcastAtSlot(message *spectypes.SignedSSVMessage, _ phase0.Slot) error {
	return n.Broadcast(message.SSVMessage.GetID(), message)
}

func (n *recordingNetwork) ReportValidation(_ *spectypes.SSVMessage, _ protocolp2p.MsgValidationResult) {
}

type testValueChecker struct{}

func (testValueChecker) CheckValue(data []byte) error {
	if len(data) == 0 {
		return spectypes.NewError(spectypes.QBFTValueInvalidErrorCode, "invalid value")
	}
	if bytes.Equal(data, []byte("invalid-value")) {
		return spectypes.NewError(spectypes.QBFTValueInvalidErrorCode, "invalid value")
	}
	return nil
}

func newInstanceTestEnv(t *testing.T, operatorID spectypes.OperatorID) *instanceTestEnv {
	t.Helper()
	require.GreaterOrEqual(t, int(operatorID), 1, "operatorID must be in Testing4SharesSet range [1,4]")
	require.LessOrEqual(t, int(operatorID), 4, "operatorID must be in Testing4SharesSet range [1,4]")

	keys := spectestingutils.Testing4SharesSet()
	committeeMember := spectestingutils.TestingCommitteeMember(keys)
	committeeMember.OperatorID = operatorID

	pubKey, err := spectypes.GetPublicKeyPem(keys.OperatorKeys[operatorID])
	require.NoError(t, err)
	committeeMember.SSVOperatorPubKey = pubKey

	config := &qbftconfig.Config{
		BeaconSigner: ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager()),
		ProposerF: func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
			return 1
		},
		Network:     newTestingNetwork(operatorID, keys.OperatorKeys[operatorID]),
		CutOffRound: spectestingutils.TestingCutOffRound,
	}

	roundTimer := roundtimer.NewTestingTimer()
	inst := NewInstance(
		t.Context(),
		zap.NewNop(),
		config,
		committeeMember,
		spectestingutils.TestingIdentifier,
		specqbft.FirstHeight,
		spectestingutils.NewOperatorSigner(keys, operatorID),
		func(ctx context.Context, logger *zap.Logger, slot phase0.Slot) ssv.QBFTRoundTimer {
			return roundTimer
		},
	)
	inst.StartValue = []byte("start-value")
	inst.ValueChecker = testValueChecker{}

	network, ok := config.GetNetwork().(*testingNetwork)
	require.True(t, ok)

	return &instanceTestEnv{
		t:          t,
		keys:       keys,
		config:     config,
		inst:       inst,
		network:    network,
		roundTimer: roundTimer,
	}
}

func (e *instanceTestEnv) setLeader(operatorID spectypes.OperatorID) {
	e.config.ProposerF = func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
		return operatorID
	}
}

func (e *instanceTestEnv) setNetwork(network protocolp2p.Network) {
	e.config.Network = network
}

func (e *instanceTestEnv) hash(fullData []byte) [32]byte {
	e.t.Helper()

	return qbftconfig.HashDataRoot(fullData)
}

func (e *instanceTestEnv) marshalJustifications(msgs []*specqbft.ProcessingMessage) [][]byte {
	e.t.Helper()

	signedMessages := make([]*spectypes.SignedSSVMessage, 0, len(msgs))
	for _, msg := range msgs {
		signedMessages = append(signedMessages, msg.SignedMessage)
	}

	justifications, err := specqbft.MarshalJustifications(signedMessages)
	require.NoError(e.t, err)
	return justifications
}

func (e *instanceTestEnv) processingMessage(msg *specqbft.Message, signerID spectypes.OperatorID, fullData []byte) *specqbft.ProcessingMessage {
	e.t.Helper()

	signed := spectestingutils.SignQBFTMsg(e.keys.OperatorKeys[signerID], signerID, msg)
	signed.FullData = fullData

	procMsg, err := specqbft.NewProcessingMessage(signed)
	require.NoError(e.t, err)
	return procMsg
}

func (e *instanceTestEnv) processingMessageWithKey(
	msg *specqbft.Message,
	signerID spectypes.OperatorID,
	signerKey *rsa.PrivateKey,
	fullData []byte,
) *specqbft.ProcessingMessage {
	e.t.Helper()

	signed := spectestingutils.SignQBFTMsg(signerKey, signerID, msg)
	signed.FullData = fullData

	procMsg, err := specqbft.NewProcessingMessage(signed)
	require.NoError(e.t, err)
	return procMsg
}

func (e *instanceTestEnv) proposal(
	round specqbft.Round,
	signerID spectypes.OperatorID,
	fullData []byte,
	root [32]byte,
	roundChanges []*specqbft.ProcessingMessage,
	prepares []*specqbft.ProcessingMessage,
) *specqbft.ProcessingMessage {
	e.t.Helper()

	return e.processingMessage(&specqbft.Message{
		MsgType:                  specqbft.ProposalMsgType,
		Height:                   e.inst.State.Height,
		Round:                    round,
		Identifier:               e.inst.State.ID,
		Root:                     root,
		RoundChangeJustification: e.marshalJustifications(roundChanges),
		PrepareJustification:     e.marshalJustifications(prepares),
	}, signerID, fullData)
}

func (e *instanceTestEnv) prepare(
	round specqbft.Round,
	signerID spectypes.OperatorID,
	root [32]byte,
) *specqbft.ProcessingMessage {
	e.t.Helper()

	return e.processingMessage(&specqbft.Message{
		MsgType:    specqbft.PrepareMsgType,
		Height:     e.inst.State.Height,
		Round:      round,
		Identifier: e.inst.State.ID,
		Root:       root,
	}, signerID, nil)
}

func (e *instanceTestEnv) commit(
	round specqbft.Round,
	signerID spectypes.OperatorID,
	root [32]byte,
) *specqbft.ProcessingMessage {
	e.t.Helper()

	return e.processingMessage(&specqbft.Message{
		MsgType:    specqbft.CommitMsgType,
		Height:     e.inst.State.Height,
		Round:      round,
		Identifier: e.inst.State.ID,
		Root:       root,
	}, signerID, nil)
}

func (e *instanceTestEnv) roundChange(
	round specqbft.Round,
	signerID spectypes.OperatorID,
	dataRound specqbft.Round,
	root [32]byte,
	fullData []byte,
	prepares []*specqbft.ProcessingMessage,
) *specqbft.ProcessingMessage {
	e.t.Helper()

	return e.processingMessage(&specqbft.Message{
		MsgType:                  specqbft.RoundChangeMsgType,
		Height:                   e.inst.State.Height,
		Round:                    round,
		Identifier:               e.inst.State.ID,
		Root:                     root,
		DataRound:                dataRound,
		RoundChangeJustification: e.marshalJustifications(prepares),
	}, signerID, fullData)
}

func (e *instanceTestEnv) addMessages(container *specqbft.MsgContainer, msgs ...*specqbft.ProcessingMessage) {
	e.t.Helper()

	for _, msg := range msgs {
		added, err := container.AddFirstMsgForSignerAndRound(msg)
		require.NoError(e.t, err)
		require.True(e.t, added)
	}
}

func (e *instanceTestEnv) broadcastedProcessingMessage(index int) *specqbft.ProcessingMessage {
	e.t.Helper()

	require.Len(e.t, e.network.BroadcastedMsgs, index+1)

	msg, err := specqbft.NewProcessingMessage(e.network.BroadcastedMsgs[index])
	require.NoError(e.t, err)
	return msg
}

func (e *instanceTestEnv) aggregateMessages(msgs ...*specqbft.ProcessingMessage) *specqbft.ProcessingMessage {
	e.t.Helper()
	require.NotEmpty(e.t, msgs)

	ret := msgs[0].SignedMessage.DeepCopy()
	for _, msg := range msgs[1:] {
		require.NoError(e.t, ret.Aggregate(msg.SignedMessage))
	}

	procMsg, err := specqbft.NewProcessingMessage(ret)
	require.NoError(e.t, err)
	procMsg.SignedMessage.FullData = msgs[0].SignedMessage.FullData
	return procMsg
}

func (e *instanceTestEnv) preparedRoundChangeSet(
	round specqbft.Round,
	preparedRound specqbft.Round,
	fullData []byte,
	prepareSigners []spectypes.OperatorID,
	roundChangeSigners []spectypes.OperatorID,
) ([]*specqbft.ProcessingMessage, []*specqbft.ProcessingMessage) {
	e.t.Helper()

	root := e.hash(fullData)

	prepares := make([]*specqbft.ProcessingMessage, 0, len(prepareSigners))
	for _, signerID := range prepareSigners {
		prepares = append(prepares, e.prepare(preparedRound, signerID, root))
	}

	roundChanges := make([]*specqbft.ProcessingMessage, 0, len(roundChangeSigners))
	for _, signerID := range roundChangeSigners {
		roundChanges = append(roundChanges, e.roundChange(round, signerID, preparedRound, root, fullData, prepares))
	}

	return roundChanges, prepares
}
