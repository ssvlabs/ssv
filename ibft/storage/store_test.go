package storage

import (
	"crypto/rsa"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/networkconfig"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
)

func TestCleanInstances(t *testing.T) {
	ks := testingutils.Testing4SharesSet()
	logger := logging.TestLogger(t)
	msgID := spectypes.NewMsgID(networkconfig.TestNetwork.Domain, []byte("pk"), spectypes.RoleCommittee)
	storage, err := newTestIbftStorage(logger, "test")
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
			DecidedMessage: testingutils.TestingCommitMultiSignerMessageWithHeightAndIdentifier(
				[]*rsa.PrivateKey{ks.OperatorKeys[1], ks.OperatorKeys[2], ks.OperatorKeys[3]},
				[]spectypes.OperatorID{1, 2, 3},
				h,
				msgID[:],
			),
		}
	}

	msgsCount := 10
	for i := 0; i < msgsCount; i++ {
		require.NoError(t, storage.SaveInstance(generateInstance(msgID, specqbft.Height(i))))
	}
	require.NoError(t, storage.SaveHighestInstance(generateInstance(msgID, specqbft.Height(msgsCount))))

	// add different msgID
	differMsgID := spectypes.NewMsgID(networkconfig.TestNetwork.Domain, []byte("differ_pk"), spectypes.RoleCommittee)
	require.NoError(t, storage.SaveInstance(generateInstance(differMsgID, specqbft.Height(1))))
	require.NoError(t, storage.SaveHighestInstance(generateInstance(differMsgID, specqbft.Height(msgsCount))))
	require.NoError(t, storage.SaveHighestAndHistoricalInstance(generateInstance(differMsgID, specqbft.Height(1))))

	res, err := storage.GetInstancesInRange(msgID[:], 0, specqbft.Height(msgsCount))
	require.NoError(t, err)
	require.Equal(t, msgsCount, len(res))

	last, err := storage.GetHighestInstance(msgID[:])
	require.NoError(t, err)
	require.NotNil(t, last)
	require.Equal(t, specqbft.Height(msgsCount), last.State.Height)

	// remove all instances
	require.NoError(t, storage.CleanAllInstances(logger, msgID[:]))
	res, err = storage.GetInstancesInRange(msgID[:], 0, specqbft.Height(msgsCount))
	require.NoError(t, err)
	require.Equal(t, 0, len(res))

	last, err = storage.GetHighestInstance(msgID[:])
	require.NoError(t, err)
	require.Nil(t, last)

	// check other msgID
	res, err = storage.GetInstancesInRange(differMsgID[:], 0, specqbft.Height(msgsCount))
	require.NoError(t, err)
	require.Equal(t, 1, len(res))

	last, err = storage.GetHighestInstance(differMsgID[:])
	require.NoError(t, err)
	require.NotNil(t, last)
}

func TestSaveAndFetchLastState(t *testing.T) {
	identifier := spectypes.NewMsgID(networkconfig.TestNetwork.Domain, []byte("pk"), spectypes.RoleCommittee)

	instance := &qbftstorage.StoredInstance{
		State: &specqbft.State{
			CommitteeMember:                 nil,
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

	storage, err := newTestIbftStorage(logging.TestLogger(t), "test")
	require.NoError(t, err)

	require.NoError(t, storage.SaveHighestInstance(instance))

	savedInstance, err := storage.GetHighestInstance(identifier[:])
	require.NoError(t, err)
	require.NotNil(t, savedInstance)
	require.Equal(t, specqbft.Height(1), savedInstance.State.Height)
	require.Equal(t, specqbft.Round(1), savedInstance.State.Round)
	require.Equal(t, identifier.String(), specqbft.ControllerIdToMessageID(savedInstance.State.ID).String())
	require.Equal(t, specqbft.Round(1), savedInstance.State.LastPreparedRound)
	require.Equal(t, true, savedInstance.State.Decided)
	require.Equal(t, []byte("value"), savedInstance.State.LastPreparedValue)
	require.Equal(t, []byte("value"), savedInstance.State.DecidedValue)
}

func TestSaveAndFetchState(t *testing.T) {
	identifier := spectypes.NewMsgID(networkconfig.TestNetwork.Domain, []byte("pk"), spectypes.RoleCommittee)

	instance := &qbftstorage.StoredInstance{
		State: &specqbft.State{
			CommitteeMember:                 nil,
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

	storage, err := newTestIbftStorage(logging.TestLogger(t), "test")
	require.NoError(t, err)

	require.NoError(t, storage.SaveInstance(instance))

	savedInstances, err := storage.GetInstancesInRange(identifier[:], 1, 1)
	require.NoError(t, err)
	require.NotNil(t, savedInstances)
	require.Len(t, savedInstances, 1)
	savedInstance := savedInstances[0]

	require.Equal(t, specqbft.Height(1), savedInstance.State.Height)
	require.Equal(t, specqbft.Round(1), savedInstance.State.Round)
	require.Equal(t, identifier.String(), specqbft.ControllerIdToMessageID(savedInstance.State.ID).String())
	require.Equal(t, specqbft.Round(1), savedInstance.State.LastPreparedRound)
	require.Equal(t, true, savedInstance.State.Decided)
	require.Equal(t, []byte("value"), savedInstance.State.LastPreparedValue)
	require.Equal(t, []byte("value"), savedInstance.State.DecidedValue)
}

func newTestIbftStorage(logger *zap.Logger, prefix string) (qbftstorage.QBFTStore, error) {
	db, err := kv.NewInMemory(logger.Named(logging.NameBadgerDBLog), basedb.Options{
		Reporting: true,
	})
	if err != nil {
		return nil, err
	}

	return New(db, prefix), nil
}

func TestEncodeDecodeOperators(t *testing.T) {
	testCases := []struct {
		input   []uint64
		encoded []byte
	}{
		// Valid sizes: 4
		{[]uint64{0x0123456789ABCDEF, 0xFEDCBA9876543210, 0x1122334455667788, 0x8877665544332211},
			[]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0xFE, 0xDC, 0xBA, 0x98, 0x76, 0x54, 0x32, 0x10, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11}},
		// Valid sizes: 7
		{[]uint64{1, 2, 3, 4, 5, 6, 7},
			[]byte{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 3, 0, 0, 0, 0, 0, 0, 0, 4, 0, 0, 0, 0, 0, 0, 0, 5, 0, 0, 0, 0, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0, 7}},
		// Valid sizes: 13
		{[]uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12},
			[]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 3, 0, 0, 0, 0, 0, 0, 0, 4, 0, 0, 0, 0, 0, 0, 0, 5, 0, 0, 0, 0, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0, 7, 0, 0, 0, 0, 0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 9, 0, 0, 0, 0, 0, 0, 0, 10, 0, 0, 0, 0, 0, 0, 0, 11, 0, 0, 0, 0, 0, 0, 0, 12}},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("Case %d", i+1), func(t *testing.T) {
			encoded, err := encodeOperators(tc.input)
			require.Equal(t, err, nil)
			require.Equal(t, tc.encoded, encoded)

			decoded := decodeOperators(encoded)
			require.Equal(t, tc.input, decoded)
		})
	}
}
