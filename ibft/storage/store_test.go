package storage

import (
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v2/message"
	qbftstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
)

func init() {
	logex.Build("", zapcore.DebugLevel, &logex.EncodingConfig{})
}

func TestCleanDecided(t *testing.T) {
	msgID := spectypes.NewMsgID([]byte("pk"), spectypes.BNRoleAttester)
	storage, err := newTestIbftStorage(logex.GetLogger(), "test", forksprotocol.GenesisForkVersion)
	require.NoError(t, err)

	generateInstance := func(id spectypes.MessageID, h specqbft.Height) *qbftstorage.StoredInstance {
		return &qbftstorage.StoredInstance{
			State: &specqbft.State{
				ID:                   id[:],
				Round:                1,
				Height:               h,
				LastPreparedRound:    1,
				LastPreparedValue:    []byte("value"),
				Decided:              true,
				DecidedValue:         []byte("value"),
				ProposeContainer:     specqbft.NewMsgContainer(),
				PrepareContainer:     specqbft.NewMsgContainer(),
				CommitContainer:      specqbft.NewMsgContainer(),
				RoundChangeContainer: specqbft.NewMsgContainer(),
			},
			DecidedMessage: &specqbft.SignedMessage{
				Signature: []byte("sig"),
				Signers:   []spectypes.OperatorID{1},
				Message: &specqbft.Message{
					MsgType:    specqbft.CommitMsgType,
					Height:     h,
					Round:      1,
					Identifier: id[:],
					Data:       nil,
				},
			},
		}
	}

	msgsCount := 10
	for i := 0; i < msgsCount; i++ {
		require.NoError(t, storage.SaveInstance(generateInstance(msgID, specqbft.Height(i))))
	}
	require.NoError(t, storage.SaveHighestInstance(generateInstance(msgID, specqbft.Height(msgsCount))))

	// add different msgID
	differMsgID := spectypes.NewMsgID([]byte("differ_pk"), spectypes.BNRoleAttester)
	require.NoError(t, storage.SaveInstance(generateInstance(differMsgID, specqbft.Height(1))))
	require.NoError(t, storage.SaveHighestInstance(generateInstance(differMsgID, specqbft.Height(msgsCount))))

	res, err := storage.GetInstance(msgID[:], 0, specqbft.Height(msgsCount))
	require.NoError(t, err)
	require.Equal(t, msgsCount, len(res))

	last, err := storage.GetHighestInstance(msgID[:])
	require.NoError(t, err)
	require.NotNil(t, last)
	require.Equal(t, specqbft.Height(msgsCount), last.State.Height)

	// remove all decided
	require.NoError(t, storage.CleanAllInstances(msgID[:]))
	res, err = storage.GetInstance(msgID[:], 0, specqbft.Height(msgsCount))
	require.NoError(t, err)
	require.Equal(t, 0, len(res))

	last, err = storage.GetHighestInstance(msgID[:])
	require.NoError(t, err)
	require.Nil(t, last)

	// check other msgID
	res, err = storage.GetInstance(differMsgID[:], 0, specqbft.Height(msgsCount))
	require.NoError(t, err)
	require.Equal(t, 1, len(res))

	last, err = storage.GetHighestInstance(differMsgID[:])
	require.NoError(t, err)
	require.NotNil(t, last)
}

func TestIbftStorage_CleanLastChangeRound(t *testing.T) {
	msgID := spectypes.NewMsgID([]byte("pk"), spectypes.BNRoleAttester)
	differMsgID := spectypes.NewMsgID([]byte("pk_differ"), spectypes.BNRoleAttester)
	storage, err := newTestIbftStorage(logex.GetLogger(), "test", forksprotocol.GenesisForkVersion)
	require.NoError(t, err)

	generateMsg := func(id spectypes.MessageID, h specqbft.Height, r specqbft.Round, s spectypes.OperatorID) *specqbft.SignedMessage {
		return &specqbft.SignedMessage{
			Signature: []byte("sig"),
			Signers:   []spectypes.OperatorID{s},
			Message: &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Height:     h,
				Round:      r,
				Identifier: id[:],
				Data:       nil,
			},
		}
	}

	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(msgID, 0, 1, 1)))
	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(msgID, 0, 1, 2)))
	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(msgID, 0, 1, 3)))
	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(msgID, 0, 1, 4)))
	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(differMsgID, 0, 1, 1))) // different identifier

	require.NoError(t, storage.CleanLastChangeRound(msgID[:]))
	res, err := storage.GetLastChangeRoundMsg(msgID[:])
	require.NoError(t, err)
	require.Equal(t, 0, len(res))

	res, err = storage.GetLastChangeRoundMsg(differMsgID[:])
	require.NoError(t, err)
	require.Equal(t, 1, len(res))

}

func TestSaveAndFetchLastChangeRound(t *testing.T) {
	identifier := spectypes.NewMsgID([]byte("pk"), spectypes.BNRoleAttester)
	storage, err := newTestIbftStorage(logex.GetLogger(), "test", forksprotocol.GenesisForkVersion)
	require.NoError(t, err)

	generateMsg := func(id spectypes.MessageID, h specqbft.Height, r specqbft.Round, s spectypes.OperatorID) *specqbft.SignedMessage {
		return &specqbft.SignedMessage{
			Signature: []byte("sig"),
			Signers:   []spectypes.OperatorID{s},
			Message: &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Height:     h,
				Round:      r,
				Identifier: id[:],
				Data:       nil,
			},
		}
	}

	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(identifier, 0, 1, 1)))
	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(identifier, 0, 2, 2)))
	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(identifier, 0, 3, 3)))
	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(identifier, 0, 4, 4)))
	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(spectypes.NewMsgID([]byte("pk"), spectypes.BNRoleAttester), 0, 4, 4))) // different identifier

	res, err := storage.GetLastChangeRoundMsg(identifier[:])
	require.NoError(t, err)
	require.Equal(t, 4, len(res))

	// check signer 1
	require.Equal(t, spectypes.OperatorID(1), res[0].GetSigners()[0])
	require.Equal(t, specqbft.Round(1), res[0].Message.Round)
	require.Equal(t, specqbft.Height(0), res[0].Message.Height)
	// check signer 3
	msg3 := res[2]
	require.Equal(t, spectypes.OperatorID(3), msg3.GetSigners()[0])
	require.Equal(t, specqbft.Round(3), msg3.Message.Round)
	require.Equal(t, specqbft.Height(0), msg3.Message.Height)

	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(identifier, 0, 2, 1))) // update round for signer 1
	require.NoError(t, storage.SaveLastChangeRoundMsg(generateMsg(identifier, 1, 2, 3))) // update round for signer 3

	res, err = storage.GetLastChangeRoundMsg(identifier[:])
	require.NoError(t, err)

	require.Equal(t, 4, len(res))
	require.Equal(t, spectypes.OperatorID(1), res[0].GetSigners()[0])
	require.Equal(t, specqbft.Round(2), res[0].Message.Round)

	// check signer 3
	msg3 = res[2]
	require.Equal(t, spectypes.OperatorID(3), msg3.GetSigners()[0])
	require.Equal(t, specqbft.Round(2), msg3.Message.Round)
	require.Equal(t, specqbft.Height(1), msg3.Message.Height)

}

func TestSaveAndFetchLastState(t *testing.T) {
	identifier := spectypes.NewMsgID([]byte("pk"), spectypes.BNRoleAttester)

	instance := &qbftstorage.StoredInstance{
		State: &specqbft.State{
			Share:                           nil,
			ID:                              identifier[:],
			Round:                           1,
			Height:                          1,
			LastPreparedRound:               1,
			LastPreparedValue:               []byte("value"),
			ProposalAcceptedForCurrentRound: nil,
			Decided:                         true,
			DecidedValue:                    []byte("value"),
			ProposeContainer:                specqbft.NewMsgContainer(),
			PrepareContainer:                specqbft.NewMsgContainer(),
			CommitContainer:                 specqbft.NewMsgContainer(),
			RoundChangeContainer:            specqbft.NewMsgContainer(),
		},
	}

	storage, err := newTestIbftStorage(logex.GetLogger(), "test", forksprotocol.GenesisForkVersion)
	require.NoError(t, err)

	require.NoError(t, storage.SaveHighestInstance(instance))

	savedInstance, err := storage.GetHighestInstance(identifier[:])
	require.NoError(t, err)
	require.NotNil(t, savedInstance)
	require.Equal(t, specqbft.Height(1), savedInstance.State.Height)
	require.Equal(t, specqbft.Round(1), savedInstance.State.Round)
	require.Equal(t, identifier.String(), message.ToMessageID(savedInstance.State.ID).String())
	require.Equal(t, specqbft.Round(1), savedInstance.State.LastPreparedRound)
	require.Equal(t, true, savedInstance.State.Decided)
	require.Equal(t, []byte("value"), savedInstance.State.LastPreparedValue)
	require.Equal(t, []byte("value"), savedInstance.State.DecidedValue)
}

func TestSaveAndFetchState(t *testing.T) {
	identifier := spectypes.NewMsgID([]byte("pk"), spectypes.BNRoleAttester)

	instance := &qbftstorage.StoredInstance{
		State: &specqbft.State{
			Share:                           nil,
			ID:                              identifier[:],
			Round:                           1,
			Height:                          1,
			LastPreparedRound:               1,
			LastPreparedValue:               []byte("value"),
			ProposalAcceptedForCurrentRound: nil,
			Decided:                         true,
			DecidedValue:                    []byte("value"),
			ProposeContainer:                specqbft.NewMsgContainer(),
			PrepareContainer:                specqbft.NewMsgContainer(),
			CommitContainer:                 specqbft.NewMsgContainer(),
			RoundChangeContainer:            specqbft.NewMsgContainer(),
		},
	}

	storage, err := newTestIbftStorage(logex.GetLogger(), "test", forksprotocol.GenesisForkVersion)
	require.NoError(t, err)

	require.NoError(t, storage.SaveInstance(instance))

	savedInstances, err := storage.GetInstance(identifier[:], 1, 1)
	require.NoError(t, err)
	require.NotNil(t, savedInstances)
	require.Len(t, savedInstances, 1)
	savedInstance := savedInstances[0]

	require.Equal(t, specqbft.Height(1), savedInstance.State.Height)
	require.Equal(t, specqbft.Round(1), savedInstance.State.Round)
	require.Equal(t, identifier.String(), message.ToMessageID(savedInstance.State.ID).String())
	require.Equal(t, specqbft.Round(1), savedInstance.State.LastPreparedRound)
	require.Equal(t, true, savedInstance.State.Decided)
	require.Equal(t, []byte("value"), savedInstance.State.LastPreparedValue)
	require.Equal(t, []byte("value"), savedInstance.State.DecidedValue)
}

func newTestIbftStorage(logger *zap.Logger, prefix string, forkVersion forksprotocol.ForkVersion) (qbftstorage.QBFTStore, error) {
	db, err := ssvstorage.GetStorageFactory(basedb.Options{
		Type:      "badger-memory",
		Logger:    logger.With(zap.String("who", "badger")),
		Path:      "",
		Reporting: true,
	})
	if err != nil {
		return nil, err
	}
	return New(db, logger.With(zap.String("who", "ibftStorage")), prefix, forkVersion), nil
}
