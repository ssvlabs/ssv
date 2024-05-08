package storage

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"github.com/bloxapp/ssv/logging"
	genesisqbftstorage "github.com/bloxapp/ssv/protocol/v2/genesisqbft/storage"
	"github.com/bloxapp/ssv/protocol/v2/genesistypes"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"

	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
)

func TestCleanInstances(t *testing.T) {
	logger := logging.TestLogger(t)
	msgID := genesisspectypes.NewMsgID(types.GetDefaultDomain(), []byte("pk"), genesisspectypes.BNRoleAttester)
	storage, err := newTestIbftStorage(logger, "test")
	require.NoError(t, err)

	generateInstance := func(id genesisspectypes.MessageID, h genesisspecqbft.Height) *genesisqbftstorage.StoredInstance {
		return &genesisqbftstorage.StoredInstance{
			State: &genesisspecqbft.State{
				ID:                   id[:],
				Round:                1,
				Height:               h,
				LastPreparedRound:    1,
				LastPreparedValue:    []byte("value"),
				Decided:              true,
				DecidedValue:         []byte("value"),
				ProposeContainer:     genesisspecqbft.NewMsgContainer(),
				PrepareContainer:     genesisspecqbft.NewMsgContainer(),
				CommitContainer:      genesisspecqbft.NewMsgContainer(),
				RoundChangeContainer: genesisspecqbft.NewMsgContainer(),
			},
			DecidedMessage: &genesisspecqbft.SignedMessage{
				Signature: []byte("sig"),
				Signers:   []genesisspectypes.OperatorID{1},
				Message: genesisspecqbft.Message{
					MsgType:    genesisspecqbft.CommitMsgType,
					Height:     h,
					Round:      1,
					Identifier: id[:],
					Root:       [32]byte{},
				},
			},
		}
	}

	msgsCount := 10
	for i := 0; i < msgsCount; i++ {
		require.NoError(t, storage.SaveInstance(generateInstance(msgID, genesisspecqbft.Height(i))))
	}
	require.NoError(t, storage.SaveHighestInstance(generateInstance(msgID, genesisspecqbft.Height(msgsCount))))

	// add different msgID
	differMsgID := genesisspectypes.NewMsgID(types.GetDefaultDomain(), []byte("differ_pk"), genesisspectypes.BNRoleAttester)
	require.NoError(t, storage.SaveInstance(generateInstance(differMsgID, genesisspecqbft.Height(1))))
	require.NoError(t, storage.SaveHighestInstance(generateInstance(differMsgID, genesisspecqbft.Height(msgsCount))))
	require.NoError(t, storage.SaveHighestAndHistoricalInstance(generateInstance(differMsgID, genesisspecqbft.Height(1))))

	res, err := storage.GetInstancesInRange(msgID[:], 0, genesisspecqbft.Height(msgsCount))
	require.NoError(t, err)
	require.Equal(t, msgsCount, len(res))

	last, err := storage.GetHighestInstance(msgID[:])
	require.NoError(t, err)
	require.NotNil(t, last)
	require.Equal(t, genesisspecqbft.Height(msgsCount), last.State.Height)

	// remove all instances
	require.NoError(t, storage.CleanAllInstances(logger, msgID[:]))
	res, err = storage.GetInstancesInRange(msgID[:], 0, genesisspecqbft.Height(msgsCount))
	require.NoError(t, err)
	require.Equal(t, 0, len(res))

	last, err = storage.GetHighestInstance(msgID[:])
	require.NoError(t, err)
	require.Nil(t, last)

	// check other msgID
	res, err = storage.GetInstancesInRange(differMsgID[:], 0, genesisspecqbft.Height(msgsCount))
	require.NoError(t, err)
	require.Equal(t, 1, len(res))

	last, err = storage.GetHighestInstance(differMsgID[:])
	require.NoError(t, err)
	require.NotNil(t, last)
}

func TestSaveAndFetchLastState(t *testing.T) {
	identifier := genesisspectypes.NewMsgID(types.GetDefaultDomain(), []byte("pk"), genesisspectypes.BNRoleAttester)

	instance := &genesisqbftstorage.StoredInstance{
		State: &genesisspecqbft.State{
			Share:                           nil,
			ID:                              identifier[:],
			Round:                           1,
			Height:                          1,
			LastPreparedRound:               1,
			LastPreparedValue:               []byte("value"),
			ProposalAcceptedForCurrentRound: nil,
			Decided:                         true,
			DecidedValue:                    []byte("value"),
			ProposeContainer:                genesisspecqbft.NewMsgContainer(),
			PrepareContainer:                genesisspecqbft.NewMsgContainer(),
			CommitContainer:                 genesisspecqbft.NewMsgContainer(),
			RoundChangeContainer:            genesisspecqbft.NewMsgContainer(),
		},
	}

	storage, err := newTestIbftStorage(logging.TestLogger(t), "test")
	require.NoError(t, err)

	require.NoError(t, storage.SaveHighestInstance(instance))

	savedInstance, err := storage.GetHighestInstance(identifier[:])
	require.NoError(t, err)
	require.NotNil(t, savedInstance)
	require.Equal(t, genesisspecqbft.Height(1), savedInstance.State.Height)
	require.Equal(t, genesisspecqbft.Round(1), savedInstance.State.Round)
	require.Equal(t, identifier.String(), genesisspecqbft.ControllerIdToMessageID(savedInstance.State.ID).String())
	require.Equal(t, genesisspecqbft.Round(1), savedInstance.State.LastPreparedRound)
	require.Equal(t, true, savedInstance.State.Decided)
	require.Equal(t, []byte("value"), savedInstance.State.LastPreparedValue)
	require.Equal(t, []byte("value"), savedInstance.State.DecidedValue)
}

func TestSaveAndFetchState(t *testing.T) {
	identifier := genesisspectypes.NewMsgID(types.GetDefaultDomain(), []byte("pk"), genesisspectypes.BNRoleAttester)

	instance := &genesisqbftstorage.StoredInstance{
		State: &genesisspecqbft.State{
			Share:                           nil,
			ID:                              identifier[:],
			Round:                           1,
			Height:                          1,
			LastPreparedRound:               1,
			LastPreparedValue:               []byte("value"),
			ProposalAcceptedForCurrentRound: nil,
			Decided:                         true,
			DecidedValue:                    []byte("value"),
			ProposeContainer:                genesisspecqbft.NewMsgContainer(),
			PrepareContainer:                genesisspecqbft.NewMsgContainer(),
			CommitContainer:                 genesisspecqbft.NewMsgContainer(),
			RoundChangeContainer:            genesisspecqbft.NewMsgContainer(),
		},
	}

	storage, err := newTestIbftStorage(logging.TestLogger(t), "test")
	require.NoError(t, err)

	require.NoError(t, storage.SaveInstance(instance))

	savedInstances, err := storage.GetInstancesInRange(identifier[:], 1, 1)
	require.NoError(t, err)
	require.NotNil(t, savedInstances)
	require.Len(t, savedInstances, 1)
	savedInstance := savedInstances[0]

	require.Equal(t, genesisspecqbft.Height(1), savedInstance.State.Height)
	require.Equal(t, genesisspecqbft.Round(1), savedInstance.State.Round)
	require.Equal(t, identifier.String(), genesisspecqbft.ControllerIdToMessageID(savedInstance.State.ID).String())
	require.Equal(t, genesisspecqbft.Round(1), savedInstance.State.LastPreparedRound)
	require.Equal(t, true, savedInstance.State.Decided)
	require.Equal(t, []byte("value"), savedInstance.State.LastPreparedValue)
	require.Equal(t, []byte("value"), savedInstance.State.DecidedValue)
}

func newTestIbftStorage(logger *zap.Logger, prefix string) (genesisqbftstorage.QBFTStore, error) {
	db, err := kv.NewInMemory(logger.Named(logging.NameBadgerDBLog), basedb.Options{
		Reporting: true,
	})
	if err != nil {
		return nil, err
	}

	return New(db, prefix), nil
}
