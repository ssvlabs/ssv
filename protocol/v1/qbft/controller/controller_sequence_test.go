package controller

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"go.uber.org/zap"

	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	forksfactory "github.com/bloxapp/ssv/protocol/v1/qbft/controller/forks/factory"
	instance2 "github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/qbft/strategy/factory"
	testingprotocol "github.com/bloxapp/ssv/protocol/v1/testing"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
)

// TODO: (lint) fix test
//nolint
func testIBFTInstance(t *testing.T) *Controller {
	currentInstanceLock := &sync.RWMutex{}
	ret := &Controller{
		Identifier: []byte("Identifier_11"),
		// instances: make([]*Instance, 0),
		CurrentInstanceLock: currentInstanceLock,
		ForkLock:            &sync.Mutex{},
	}

	ret.Fork = forksfactory.NewFork(forksprotocol.GenesisForkVersion)
	return ret
}

// TODO: (lint) fix test
//nolint
func TestCanStartNewInstance(t *testing.T) {
	uids := []message.OperatorID{message.OperatorID(1), message.OperatorID(2), message.OperatorID(3), message.OperatorID(4)}
	sks, nodes := testingprotocol.GenerateBLSKeys(uids...)

	height10 := atomic.Value{}
	height10.Store(message.Height(10))

	tests := []struct {
		name            string
		opts            instance2.ControllerStartInstanceOptions
		share           *beacon.Share
		storage         qbftstorage.QBFTStore
		initState       uint32
		currentInstance instance2.Instancer
		expectedError   string
	}{
		{
			"valid next instance start",
			instance2.ControllerStartInstanceOptions{
				SeqNumber: 11,
			},
			&beacon.Share{
				NodeID:    1,
				PublicKey: sks[1].GetPublicKey(),
				Committee: nodes,
			},
			testingprotocol.PopulatedStorage(t, sks, 3, 10),
			Ready,
			nil,
			"",
		},
		{
			"valid first instance",
			instance2.ControllerStartInstanceOptions{
				SeqNumber: 0,
			},
			&beacon.Share{
				NodeID:    1,
				PublicKey: sks[1].GetPublicKey(),
				Committee: nodes,
			},
			nil,
			Ready,
			nil,
			"",
		},
		{
			"didn't finish initialization",
			instance2.ControllerStartInstanceOptions{},
			&beacon.Share{
				NodeID:    1,
				PublicKey: sks[1].GetPublicKey(),
				Committee: nodes,
			},
			nil,
			NotStarted,
			nil,
			"iBFT hasn't initialized yet",
		},
		{
			"didn't finish sync",
			instance2.ControllerStartInstanceOptions{},
			&beacon.Share{
				NodeID:    1,
				PublicKey: sks[1].GetPublicKey(),
				Committee: nodes,
			},
			nil,
			InitiatedHandlers,
			nil,
			"iBFT hasn't initialized yet",
		},
		{
			"sequence skips",
			instance2.ControllerStartInstanceOptions{
				SeqNumber: 12,
			},
			&beacon.Share{
				NodeID:    1,
				PublicKey: sks[1].GetPublicKey(),
				Committee: nodes,
			},
			testingprotocol.PopulatedStorage(t, sks, 3, 10),
			Ready,
			nil,
			"instance seq invalid",
		},
		{
			"past instance",
			instance2.ControllerStartInstanceOptions{
				SeqNumber: 10,
			},
			&beacon.Share{
				NodeID:    1,
				PublicKey: sks[1].GetPublicKey(),
				Committee: nodes,
			},
			testingprotocol.PopulatedStorage(t, sks, 3, 10),
			Ready,
			nil,
			"instance seq invalid",
		},
		{
			"didn't finish current instance",
			instance2.ControllerStartInstanceOptions{
				SeqNumber: 11,
			},
			&beacon.Share{
				NodeID:    1,
				PublicKey: sks[1].GetPublicKey(),
				Committee: nodes,
			},
			testingprotocol.PopulatedStorage(t, sks, 3, 10),
			Ready,
			instance2.NewInstanceWithState(&qbft.State{
				Height: height10,
			}),
			fmt.Sprintf("current instance (%d) is still running", 10),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			i := testIBFTInstance(t)
			i.State = test.initState
			currentInstanceLock := &sync.RWMutex{}
			i.CurrentInstanceLock = currentInstanceLock
			i.ForkLock = &sync.Mutex{}
			if test.currentInstance != nil {
				i.setCurrentInstance(test.currentInstance)
			}
			if test.storage != nil {
				i.InstanceStorage = test.storage
				i.ChangeRoundStorage = test.storage
				i.DecidedFactory = factory.NewDecidedFactory(zap.L(), i.GetNodeMode(), test.storage, nil)
			} else {
				options := basedb.Options{
					Type:   "badger-memory",
					Logger: zap.L(),
					Path:   "",
				}
				// creating new db instance each time to get cleared one (without no data)
				db, err := storage.GetStorageFactory(options)
				require.NoError(t, err)
				store := qbftstorage.NewQBFTStore(db, options.Logger, "attestation")
				i.InstanceStorage = store
				i.ChangeRoundStorage = store
				i.DecidedFactory = factory.NewDecidedFactory(zap.L(), i.GetNodeMode(), store, nil)
			}

			i.DecidedStrategy = i.DecidedFactory.GetStrategy()

			i.ValidatorShare = test.share
			i.InstanceConfig = qbft.DefaultConsensusParams()
			// i.instances = test.prevInstances
			instanceOpts, err := i.instanceOptionsFromStartOptions(test.opts)
			require.NoError(t, err)
			// instanceOpts.SeqNumber = test.seqNumber
			err = i.canStartNewInstance(*instanceOpts)

			if len(test.expectedError) > 0 {
				require.EqualError(t, err, test.expectedError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
