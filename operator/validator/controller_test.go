package validator

import (
	"context"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	validatorstorage "github.com/bloxapp/ssv/protocol/v1/validator/types"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/bloxapp/ssv/validator"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"sync"
	"testing"
)

func setupController(logger *zap.Logger, validators map[string]*validator.Validator) controller {
	return controller{
		context:                    context.Background(),
		collection:                 nil,
		logger:                     logger,
		beacon:                     nil,
		keyManager:                 nil,
		shareEncryptionKeyProvider: nil,
		validatorsMap: &validatorsMap{
			logger:        logger.With(zap.String("component", "validatorsMap")),
			ctx:           context.Background(),
			lock:          sync.RWMutex{},
			validatorsMap: validators,
		},
		metadataUpdateQueue:    nil,
		metadataUpdateInterval: 0,
	}
}

func TestGetIndices(t *testing.T) {
	logger := logex.Build("test", zap.InfoLevel, nil)
	validators := map[string]*validator.Validator{
		"0": {
			Share: &validatorstorage.Share{
				NodeID:    0,
				PublicKey: nil,
				Committee: nil,
				Metadata: &beacon.ValidatorMetadata{
					Balance: 0,
					Status:  0, // ValidatorStateUnknown
					Index:   3,
				},
			},
		},
		"1": {
			Share: &validatorstorage.Share{
				NodeID:    0,
				PublicKey: nil,
				Committee: nil,
				Metadata: &beacon.ValidatorMetadata{
					Balance: 0,
					Status:  1, // ValidatorStatePendingInitialized
					Index:   3,
				},
			},
		},
		"2": {
			Share: &validatorstorage.Share{
				NodeID:    0,
				PublicKey: nil,
				Committee: nil,
				Metadata: &beacon.ValidatorMetadata{
					Balance: 0,
					Status:  2, // ValidatorStatePendingQueued
					Index:   3,
				},
			},
		},
		"3": {
			Share: &validatorstorage.Share{
				NodeID:    0,
				PublicKey: nil,
				Committee: nil,
				Metadata: &beacon.ValidatorMetadata{
					Balance: 0,
					Status:  3, // ValidatorStateActiveOngoing
					Index:   3,
				},
			},
		},
		"4": {
			Share: &validatorstorage.Share{
				NodeID:    0,
				PublicKey: nil,
				Committee: nil,
				Metadata: &beacon.ValidatorMetadata{
					Balance: 0,
					Status:  4, // ValidatorStateActiveExiting
					Index:   4,
				},
			},
		},
		"5": {
			Share: &validatorstorage.Share{
				NodeID:    0,
				PublicKey: nil,
				Committee: nil,
				Metadata: &beacon.ValidatorMetadata{
					Balance: 0,
					Status:  5, // ValidatorStateActiveSlashed
					Index:   5,
				},
			},
		},
		"6": {
			Share: &validatorstorage.Share{
				NodeID:    0,
				PublicKey: nil,
				Committee: nil,
				Metadata: &beacon.ValidatorMetadata{
					Balance: 0,
					Status:  6, // ValidatorStateExitedUnslashed
					Index:   6,
				},
			},
		},
		"7": {
			Share: &validatorstorage.Share{
				NodeID:    0,
				PublicKey: nil,
				Committee: nil,
				Metadata: &beacon.ValidatorMetadata{
					Balance: 0,
					Status:  7, // ValidatorStateExitedSlashed
					Index:   7,
				},
			},
		},
		"8": {
			Share: &validatorstorage.Share{
				NodeID:    0,
				PublicKey: nil,
				Committee: nil,
				Metadata: &beacon.ValidatorMetadata{
					Balance: 0,
					Status:  8, // ValidatorStateWithdrawalPossible
					Index:   8,
				},
			},
		},
		"9": {
			Share: &validatorstorage.Share{
				NodeID:    0,
				PublicKey: nil,
				Committee: nil,
				Metadata: &beacon.ValidatorMetadata{
					Balance: 0,
					Status:  9, // ValidatorStateWithdrawalDone
					Index:   9,
				},
			},
		},
	}

	ctr := setupController(logger, validators)
	indices := ctr.GetValidatorsIndices()
	logger.Info("result", zap.Any("indices", indices))
	require.Equal(t, 1, len(indices)) // should return only active indices
}
