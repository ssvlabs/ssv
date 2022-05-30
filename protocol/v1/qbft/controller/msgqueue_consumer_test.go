package controller

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v1/qbft/msgqueue"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	"github.com/bloxapp/ssv/utils/logex"
)

func init() {
	logex.Build("", zapcore.DebugLevel, &logex.EncodingConfig{})
}

func TestConsumeMessages(t *testing.T) {
	q, err := msgqueue.New(
		logex.GetLogger().With(zap.String("who", "msg_q")),
		msgqueue.WithIndexers(msgqueue.DefaultMsgIndexer(), msgqueue.SignedMsgIndexer(), msgqueue.DecidedMsgIndexer(), msgqueue.SignedPostConsensusMsgIndexer()),
	)
	require.NoError(t, err)
	currentInstanceLock := &sync.RWMutex{}
	ctrl := Controller{
		ctx:                 context.Background(),
		logger:              logex.GetLogger().With(zap.String("who", "controller")),
		q:                   q,
		signatureState:      SignatureState{},
		Identifier:          message.NewIdentifier([]byte("1"), message.RoleTypeAttester),
		currentInstanceLock: currentInstanceLock,
		forkLock:            &sync.Mutex{},
	}
	ctrl.signatureState.setHeight(0)

	tests := []struct {
		name            string
		msgs            []*message.SSVMessage
		expected        []*message.SSVMessage
		lastHeight      message.Height
		currentInstance instance.Instancer
	}{
		{
			"no_running_instance_late_commit",
			[]*message.SSVMessage{
				generateConsensusMsg(t, message.Height(1), 0, ctrl.Identifier, message.CommitMsgType),
			},
			[]*message.SSVMessage{generateConsensusMsg(t, message.Height(1), 0, ctrl.Identifier, message.CommitMsgType)},
			message.Height(2),
			nil,
		},
		{
			"no_running_instance_too_late_commit",
			[]*message.SSVMessage{
				generateConsensusMsg(t, message.Height(0), 0, ctrl.Identifier, message.CommitMsgType),
			},
			nil,
			message.Height(2),
			nil,
		},
		{
			"no_running_instance_late_decided",
			[]*message.SSVMessage{
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, message.Height(1), ctrl.Identifier),
			},
			[]*message.SSVMessage{generatePostConsensusOrSig(t, message.SSVDecidedMsgType, message.Height(1), ctrl.Identifier)},
			message.Height(2),
			nil,
		},
		{
			"no_running_instance_sig_current_height",
			[]*message.SSVMessage{
				generateConsensusMsg(t, message.Height(0), 0, ctrl.Identifier, message.RoundChangeMsgType),
				generateConsensusMsg(t, message.Height(1), 0, ctrl.Identifier, message.RoundChangeMsgType),
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, message.Height(1), ctrl.Identifier), // verify priority is right
				generatePostConsensusOrSig(t, message.SSVPostConsensusMsgType, message.Height(1), ctrl.Identifier),
			},
			[]*message.SSVMessage{generatePostConsensusOrSig(t, message.SSVPostConsensusMsgType, message.Height(1), ctrl.Identifier)},
			message.Height(1),
			nil,
		},
		{
			"by_state_not_started_proposal",
			[]*message.SSVMessage{
				generateConsensusMsg(t, message.Height(0), 1, ctrl.Identifier, message.ProposalMsgType), // verify priority is right
				generateConsensusMsg(t, message.Height(1), 1, ctrl.Identifier, message.ProposalMsgType),
				generateConsensusMsg(t, message.Height(1), 2, ctrl.Identifier, message.ProposalMsgType), // verify priority is right
			},
			[]*message.SSVMessage{generateConsensusMsg(t, message.Height(1), message.Round(1), ctrl.Identifier, message.ProposalMsgType)},
			message.Height(1),
			generateInstance(1, 1, qbft.RoundStateNotStarted),
		},
		{
			"by_state_not_started_decided",
			[]*message.SSVMessage{
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, message.Height(0), ctrl.Identifier),
				generateConsensusMsg(t, message.Height(0), message.Round(1), ctrl.Identifier, message.RoundChangeMsgType), // verify priority is right
			},
			[]*message.SSVMessage{
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, message.Height(0), ctrl.Identifier),
			},
			message.Height(0),
			generateInstance(0, 1, qbft.RoundStateNotStarted),
		},
		{
			"by_state_PrePrepare",
			[]*message.SSVMessage{
				generateConsensusMsg(t, message.Height(0), message.Round(1), ctrl.Identifier, message.PrepareMsgType),
				generateConsensusMsg(t, message.Height(0), message.Round(1), ctrl.Identifier, message.PrepareMsgType),     // verify priority is right
				generateConsensusMsg(t, message.Height(0), message.Round(1), ctrl.Identifier, message.RoundChangeMsgType), // verify priority is right
			},
			[]*message.SSVMessage{
				generateConsensusMsg(t, message.Height(0), message.Round(1), ctrl.Identifier, message.PrepareMsgType),
			},
			message.Height(0),
			generateInstance(0, 1, qbft.RoundStatePrePrepare),
		},
		{
			"by_state_prepare",
			[]*message.SSVMessage{
				generateConsensusMsg(t, message.Height(0), message.Round(1), ctrl.Identifier, message.CommitMsgType),
				generateConsensusMsg(t, message.Height(0), message.Round(1), ctrl.Identifier, message.ProposalMsgType),    // verify priority is right
				generateConsensusMsg(t, message.Height(0), message.Round(1), ctrl.Identifier, message.RoundChangeMsgType), // verify priority is right
			},
			[]*message.SSVMessage{
				generateConsensusMsg(t, message.Height(0), message.Round(1), ctrl.Identifier, message.CommitMsgType),
			},
			message.Height(0),
			generateInstance(0, 1, qbft.RoundStatePrepare),
		},
		{
			"by_state_change_round",
			[]*message.SSVMessage{
				generateConsensusMsg(t, message.Height(1), message.Round(1), ctrl.Identifier, message.RoundChangeMsgType), // verify priority is right
				generateConsensusMsg(t, message.Height(0), message.Round(1), ctrl.Identifier, message.RoundChangeMsgType),
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, message.Height(0), ctrl.Identifier), // verify priority is right
			},
			[]*message.SSVMessage{
				generateConsensusMsg(t, message.Height(0), message.Round(1), ctrl.Identifier, message.RoundChangeMsgType),
			},
			message.Height(0),
			generateInstance(0, 1, qbft.RoundStateChangeRound),
		},
		{
			"by_state_no_message_pop_decided",
			[]*message.SSVMessage{
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, message.Height(0), ctrl.Identifier),
				generateConsensusMsg(t, message.Height(0), message.Round(1), ctrl.Identifier, message.CommitMsgType),      // verify priority is right
				generateConsensusMsg(t, message.Height(0), message.Round(1), ctrl.Identifier, message.RoundChangeMsgType), // verify priority is right
			},
			[]*message.SSVMessage{
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, message.Height(0), ctrl.Identifier),
			},
			message.Height(0),
			generateInstance(0, 1, qbft.RoundStatePrePrepare),
		},
		{
			"by_state_no_message_pop_change_round",
			[]*message.SSVMessage{
				generateConsensusMsg(t, message.Height(0), message.Round(1), ctrl.Identifier, message.CommitMsgType), // verify priority is right
				generateConsensusMsg(t, message.Height(0), message.Round(1), ctrl.Identifier, message.RoundChangeMsgType),
			},
			[]*message.SSVMessage{
				generateConsensusMsg(t, message.Height(0), message.Round(1), ctrl.Identifier, message.RoundChangeMsgType),
			},
			message.Height(0),
			generateInstance(0, 1, qbft.RoundStatePrePrepare),
		},
		{
			"default_late_commit",
			[]*message.SSVMessage{
				generateConsensusMsg(t, message.Height(0), message.Round(1), ctrl.Identifier, message.CommitMsgType),
			},
			[]*message.SSVMessage{
				generateConsensusMsg(t, message.Height(0), message.Round(1), ctrl.Identifier, message.CommitMsgType), // expect previews height commit
			},
			message.Height(1), // current height
			nil,
		},
		{
			"default_late_decided",
			[]*message.SSVMessage{
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, message.Height(0), ctrl.Identifier),
			},
			[]*message.SSVMessage{
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, message.Height(0), ctrl.Identifier),
			},
			message.Height(1), // current height
			nil,
		},
		{
			"no_running_instance_force_decided",
			[]*message.SSVMessage{
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, message.Height(2), ctrl.Identifier),
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, message.Height(1), ctrl.Identifier),
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, message.Height(0), ctrl.Identifier),
			},
			[]*message.SSVMessage{
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, message.Height(2), ctrl.Identifier),
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, message.Height(1), ctrl.Identifier),
			},
			message.Height(1),
			nil,
		},
		{
			"by_state_force_decided",
			[]*message.SSVMessage{
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, message.Height(2), ctrl.Identifier),
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, message.Height(1), ctrl.Identifier),
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, message.Height(0), ctrl.Identifier),
			},
			[]*message.SSVMessage{
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, message.Height(2), ctrl.Identifier),
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, message.Height(1), ctrl.Identifier),
			},
			message.Height(1),
			generateInstance(1, 1, qbft.RoundStatePrePrepare),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			ctrl.ctx = ctx
			ctrl.setCurrentInstance(test.currentInstance)
			ctrl.signatureState = SignatureState{}
			ctrl.signatureState.setHeight(test.lastHeight)

			for _, msg := range test.msgs {
				ctrl.q.Add(msg)
			}

			go func() {
				time.Sleep(time.Second * 2) // test time out
				if ctx.Err() == nil && test.expected != nil {
					panic("time out")
				} else {
					cancel()
					q.Clean(func(s string) bool {
						return true
					})
				}
			}()

			processed := 0
			ctrl.startQueueConsumer(func(msg *message.SSVMessage) error {
				// when done, cancel ctx
				ctrl.logger.Debug("process msg")

				expectedMsg := test.expected[processed]
				require.Equal(t, expectedMsg.MsgType, msg.MsgType)

				switch expectedMsg.MsgType {
				case message.SSVDecidedMsgType:
					fallthrough
				case message.SSVConsensusMsgType:
					testSignedMsg := new(message.SignedMessage)
					require.NoError(t, testSignedMsg.Decode(expectedMsg.Data))

					signedMsg := new(message.SignedMessage)
					require.NoError(t, signedMsg.Decode(msg.Data))

					require.Equal(t, testSignedMsg.Message.MsgType, signedMsg.Message.MsgType)
					require.Equal(t, testSignedMsg.Message.Height, signedMsg.Message.Height)
					require.Equal(t, testSignedMsg.Message.Round, signedMsg.Message.Round)
				}
				processed++
				ctrl.logger.Debug("--------- processed -----", zap.Int("processed", processed), zap.Int("total", len(test.expected)), zap.String("name", test.name))

				if processed == len(test.expected) {
					ctrl.logger.Debug("--------- done by procesed -----", zap.String("name", test.name))
					cancel()
					q.Clean(func(s string) bool {
						return true
					})
				}
				return nil
			})
		})
	}
}

func generateInstance(height message.Height, round message.Round, stage qbft.RoundState) instance.Instancer {
	i := &InstanceMock{state: &qbft.State{
		Height: qbft.NewHeight(height),
		Round:  qbft.NewRound(round),
		Stage:  *atomic.NewInt32(int32(stage))}}
	return i
}

func generatePostConsensusOrSig(t *testing.T, msgType message.MsgType, height message.Height, id message.Identifier) *message.SSVMessage {
	ssvMsg := &message.SSVMessage{
		MsgType: msgType,
		ID:      id,
	}

	signedMsg := message.SignedMessage{
		Signature: nil,
		Signers:   nil,
		Message: &message.ConsensusMessage{
			MsgType:    message.CommitMsgType,
			Height:     height,
			Round:      0,
			Identifier: nil,
			Data:       nil,
		},
	}
	data, err := signedMsg.Encode()
	require.NoError(t, err)
	ssvMsg.Data = data
	return ssvMsg
}

func generateConsensusMsg(t *testing.T, height message.Height, round message.Round, id message.Identifier, consensusType message.ConsensusMessageType) *message.SSVMessage {
	ssvMsg := &message.SSVMessage{
		MsgType: message.SSVConsensusMsgType,
		ID:      id,
	}

	signedMsg := message.SignedMessage{
		Signature: nil,
		Signers:   nil,
		Message: &message.ConsensusMessage{
			MsgType:    consensusType,
			Height:     height,
			Round:      round,
			Identifier: nil,
			Data:       nil,
		},
	}
	data, err := signedMsg.Encode()
	require.NoError(t, err)
	ssvMsg.Data = data
	return ssvMsg
}

type InstanceMock struct {
	state *qbft.State
}

func (i *InstanceMock) PrePrepareMsgPipeline() pipelines.SignedMessagePipeline {
	//TODO implement me
	panic("implement me")
}

func (i *InstanceMock) PrepareMsgPipeline() pipelines.SignedMessagePipeline {
	//TODO implement me
	panic("implement me")
}

func (i *InstanceMock) CommitMsgValidationPipeline() pipelines.SignedMessagePipeline {
	//TODO implement me
	panic("implement me")
}

func (i *InstanceMock) CommitMsgPipeline() pipelines.SignedMessagePipeline {
	//TODO implement me
	panic("implement me")
}

func (i *InstanceMock) DecidedMsgPipeline() pipelines.SignedMessagePipeline {
	//TODO implement me
	panic("implement me")
}

func (i *InstanceMock) ChangeRoundMsgValidationPipeline() pipelines.SignedMessagePipeline {
	//TODO implement me
	panic("implement me")
}

func (i *InstanceMock) ChangeRoundMsgPipeline() pipelines.SignedMessagePipeline {
	//TODO implement me
	panic("implement me")
}

func (i *InstanceMock) Init() {}
func (i *InstanceMock) Start(inputValue []byte) error {
	panic("not implemented")
}
func (i *InstanceMock) Stop() {}
func (i *InstanceMock) State() *qbft.State {
	return i.state
}
func (i *InstanceMock) ForceDecide(msg *message.SignedMessage) {}
func (i *InstanceMock) GetStageChan() chan qbft.RoundState {
	panic("not implemented")
}
func (i *InstanceMock) GetLastChangeRoundMsg() *message.SignedMessage {
	panic("not implemented")
}
func (i *InstanceMock) CommittedAggregatedMsg() (*message.SignedMessage, error) {
	panic("not implemented")
}
func (i *InstanceMock) GetCommittedAggSSVMessage() (message.SSVMessage, error) {
	panic("not implemented")
}
func (i *InstanceMock) ProcessMsg(msg *message.SignedMessage) (bool, error) {
	panic("not implemented")
}
func (i *InstanceMock) ResetRoundTimer() {}
func (i *InstanceMock) BroadcastChangeRound() error {
	panic("not implemented")
}
