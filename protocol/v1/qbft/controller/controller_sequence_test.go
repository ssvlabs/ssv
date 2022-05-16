package controller

import (
	"fmt"
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
	testingprotocol "github.com/bloxapp/ssv/protocol/v1/testing"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
)

func testIBFTInstance(t *testing.T) *Controller {
	ret := &Controller{
		Identifier:   []byte("lambda_11"),
		initHandlers: atomic.Bool{},
		initSynced:   atomic.Bool{},
		// instances: make([]*Instance, 0),
	}

	ret.fork = forksfactory.NewFork(forksprotocol.V0ForkVersion)
	return ret
}

func TestCanStartNewInstance(t *testing.T) {
	uids := []message.OperatorID{message.OperatorID(1), message.OperatorID(2), message.OperatorID(3), message.OperatorID(4)}
	sks, nodes := testingprotocol.GenerateBLSKeys(uids...)

	height10 := atomic.Value{}
	height10.Store(10)

	tests := []struct {
		name            string
		opts            instance2.ControllerStartInstanceOptions
		share           *beacon.Share
		storage         qbftstorage.QBFTStore
		initFinished    bool
		initSynced      bool
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
			true,
			true,
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
			true,
			true,
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
			false,
			false,
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
			true,
			false,
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
			true,
			true,
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
			true,
			true,
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
			true,
			true,
			instance2.NewInstanceWithState(&qbft.State{
				Height: height10,
			}),
			fmt.Sprintf("current instance (%d) is still running", 10),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			i := testIBFTInstance(t)
			i.initHandlers.Store(test.initFinished)
			i.initSynced.Store(test.initSynced)
			if test.currentInstance != nil {
				i.currentInstance = test.currentInstance
			}
			if test.storage != nil {
				i.ibftStorage = test.storage
			} else {
				options := basedb.Options{
					Type:   "badger-memory",
					Logger: zap.L(),
					Path:   "",
				}
				// creating new db instance each time to get cleared one (without no data)
				db, err := storage.GetStorageFactory(options)
				require.NoError(t, err)
				i.ibftStorage = qbftstorage.NewQBFTStore(db, options.Logger, "attestation")
			}

			i.ValidatorShare = test.share
			i.instanceConfig = qbft.DefaultConsensusParams()
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
