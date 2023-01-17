package syncing

// import (
// 	"context"
// 	"testing"

// 	specqbft "github.com/bloxapp/ssv-spec/qbft"
// 	spectypes "github.com/bloxapp/ssv-spec/types"
// 	"github.com/bloxapp/ssv/network/syncing/mocks"
// 	"github.com/golang/mock/gomock"
// 	"github.com/pkg/errors"
// 	"github.com/stretchr/testify/require"
// )

// func TestConcurrentSyncer_SyncHighestDecided(t *testing.T) {
// 	ctrl := gomock.NewController(t)
// 	defer ctrl.Finish()

// 	mockSyncer := mocks.NewMockSyncer(ctrl)
// 	mockHandler := mocks.NewMockMessageHandler(ctrl)
// 	mockErrorChan := make(chan Error, 1)

// 	testCtx, cancel := context.WithCancel(context.Background())
// 	defer cancel()

// 	testID := spectypes.MessageID{1, 2, 3, 4}

// 	mockSyncer.EXPECT().SyncHighestDecided(testCtx, testID, mockHandler).Return(nil)

// 	concurrent := NewConcurrent(testCtx, mockSyncer, 1, mockErrorChan)
// 	err := concurrent.SyncHighestDecided(testCtx, testID, mockHandler)

// 	require.Nil(t, err)
// 	require.Len(t, mockErrorChan, 0)
// }

// func TestConcurrentSyncer_SyncHighestDecided_Error(t *testing.T) {
// 	ctrl := gomock.NewController(t)
// 	defer ctrl.Finish()

// 	mockSyncer := mocks.NewMockSyncer(ctrl)
// 	mockHandler := mocks.NewMockMessageHandler(ctrl)
// 	mockErrorChan := make(chan Error, 1)

// 	testCtx, cancel := context.WithCancel(context.Background())
// 	defer cancel()

// 	testID := spectypes.MessageID{1, 2, 3, 4}
// 	testError := errors.New("Test error")

// 	mockSyncer.EXPECT().SyncHighestDecided(testCtx, testID, mockHandler).Return(testError)

// 	concurrent := NewConcurrent(testCtx, mockSyncer, 1, mockErrorChan)
// 	err := concurrent.SyncHighestDecided(testCtx, testID, mockHandler)

// 	require.Nil(t, err)
// 	require.Len(t, mockErrorChan, 1)

// 	error := <-mockErrorChan
// 	require.Equal(t, "SyncHighestDecided", error.Operation)
// 	require.Equal(t, testID, error.MessageID)
// 	require.Equal(t, testError, error.Err)
// }

// func TestConcurrentSyncer_SyncDecidedByRange(t *testing.T) {
// 	ctrl := gomock.NewController(t)
// 	defer ctrl.Finish()

// 	mockSyncer := mocks.NewMockSyncer(ctrl)
// 	mockHandler := mocks.NewMockMessageHandler(ctrl)
// 	mockErrorChan := make(chan Error, 1)

// 	testCtx, cancel := context.WithCancel(context.Background())
// 	defer cancel()
// 	testID := spectypes.MessageID{1, 2, 3, 4}
// 	testFrom := specqbft.Height(1)
// 	testTo := specqbft.Height(10)

// 	mockSyncer.EXPECT().SyncDecidedByRange(testCtx, testID, testFrom, testTo, mockHandler).Return(nil)

// 	concurrent := NewConcurrent(testCtx, mockSyncer, 1, mockErrorChan)
// 	err := concurrent.SyncDecidedByRange(testCtx, testID, testFrom, testTo, mockHandler)

// 	require.Nil(t, err)
// 	require.Len(t, mockErrorChan, 0)
// }

// func TestConcurrentSyncer_SyncDecidedByRange_Error(t *testing.T) {
// 	ctrl := gomock.NewController(t)
// 	defer ctrl.Finish()
// 	mockSyncer := mocks.NewMockSyncer(ctrl)
// 	mockHandler := mocks.NewMockMessageHandler(ctrl)
// 	mockErrorChan := make(chan Error, 1)

// 	testCtx, cancel := context.WithCancel(context.Background())
// 	defer cancel()

// 	testID := spectypes.MessageID{1, 2, 3, 4}
// 	testFrom := specqbft.Height(1)
// 	testTo := specqbft.Height(10)
// 	testError := errors.New("Test error")

// 	mockSyncer.EXPECT().SyncDecidedByRange(testCtx, testID, testFrom, testTo, mockHandler).Return(testError)

// 	concurrent := NewConcurrent(testCtx, mockSyncer, 1, mockErrorChan)
// 	err := concurrent.SyncDecidedByRange(testCtx, testID, testFrom, testTo, mockHandler)

// 	require.Nil(t, err)
// 	require.Len(t, mockErrorChan, 1)

// 	error := <-mockErrorChan
// 	require.Equal(t, "SyncDecidedByRange", error.Operation)
// 	require.Equal(t, testID, error.MessageID)
// 	require.Equal(t, testError, error.Err)
// }
