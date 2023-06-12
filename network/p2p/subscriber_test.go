package p2pv1

import (
	"encoding/hex"
	"testing"

	spectypes "github.com/bloxapp/ssv-spec/types"
	forksmocks "github.com/bloxapp/ssv/network/forks/mocks"
	topicsmocks "github.com/bloxapp/ssv/network/topics/mocks"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestSubscriber(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockTopicCtrl := topicsmocks.NewMockController(ctrl)
	mockFork := forksmocks.NewMockFork(ctrl)

	mockFork.EXPECT().Subnets().Return(128).AnyTimes()

	subscriber := newSubscriber(mockTopicCtrl, mockFork, []byte{0, 0})
	logger := zap.NewNop()

	t.Run("Add and remove validators", func(t *testing.T) {
		pk1 := spectypes.ValidatorPK("test1")
		pk2 := spectypes.ValidatorPK("test2")
		pk3 := spectypes.ValidatorPK("test3")

		mockFork.EXPECT().ValidatorSubnet(hex.EncodeToString(pk1)).Return(0).Times(2)
		mockFork.EXPECT().ValidatorSubnet(hex.EncodeToString(pk2)).Return(0).Times(2)
		mockFork.EXPECT().ValidatorSubnet(hex.EncodeToString(pk3)).Return(1).Times(2)

		mockFork.EXPECT().SubnetTopicID(0).Return(string("test-id-0")).AnyTimes()
		mockFork.EXPECT().SubnetTopicID(1).Return(string("test-id-1")).AnyTimes()

		mockTopicCtrl.EXPECT().Subscribe(logger, string("test-id-0")).Return(nil).Times(1)
		mockTopicCtrl.EXPECT().Subscribe(logger, string("test-id-1")).Return(nil).Times(1)
		mockTopicCtrl.EXPECT().Unsubscribe(logger, string("test-id-0"), false).Return(nil).Times(1)
		mockTopicCtrl.EXPECT().Unsubscribe(logger, string("test-id-1"), false).Return(nil).Times(1)

		subscriber.AddValidator(pk1)
		assert.Equal(t, 1, countOnes(subscriber.Subnets()))
		subscriber.AddValidator(pk2)
		assert.Equal(t, 1, countOnes(subscriber.Subnets()))
		subscriber.AddValidator(pk3)
		assert.Equal(t, 2, countOnes(subscriber.Subnets()))

		newSubnets, inactiveSubnets, err := subscriber.Update(logger)
		assert.Nil(t, err)
		assert.ElementsMatch(t, []int{0, 1}, newSubnets)
		assert.Empty(t, inactiveSubnets)

		subscriber.RemoveValidator(pk1)
		assert.Equal(t, 2, countOnes(subscriber.Subnets()))

		newSubnets, inactiveSubnets, err = subscriber.Update(logger)
		assert.Nil(t, err)
		assert.Empty(t, newSubnets)
		assert.Empty(t, inactiveSubnets)

		subscriber.RemoveValidator(pk2)
		assert.Equal(t, 1, countOnes(subscriber.Subnets()))

		newSubnets, inactiveSubnets, err = subscriber.Update(logger)
		assert.Nil(t, err)
		assert.Empty(t, newSubnets)
		assert.Equal(t, []int{0}, inactiveSubnets)

		subscriber.RemoveValidator(pk3)
		assert.Equal(t, 0, countOnes(subscriber.Subnets()))

		newSubnets, inactiveSubnets, err = subscriber.Update(logger)
		assert.Nil(t, err)
		assert.Empty(t, newSubnets)
		assert.ElementsMatch(t, []int{1}, inactiveSubnets)
	})
}

func countOnes(b []byte) int {
	count := 0
	for _, v := range b {
		if v == 1 {
			count++
		}
	}
	return count
}
