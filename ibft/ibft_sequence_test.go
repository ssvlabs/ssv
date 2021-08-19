package ibft

import (
	"fmt"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/utils/threadsafe"
	validatorstorage "github.com/bloxapp/ssv/validator/storage"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

func testIBFTInstance(t *testing.T) *ibftImpl {
	return &ibftImpl{
		Identifier: []byte("lambda_11"),
		//instances: make([]*Instance, 0),
	}
}

func TestCanStartNewInstance(t *testing.T) {
	sks, nodes := GenerateNodes(4)

	tests := []struct {
		name            string
		opts            StartOptions
		share           *validatorstorage.Share
		storage         collections.Iibft
		initFinished    bool
		currentInstance *Instance
		expectedError   string
	}{
		{
			"valid next instance start",
			StartOptions{
				SeqNumber: 11,
			},
			&validatorstorage.Share{
				NodeID:    1,
				PublicKey: validatorPK(sks),
				ShareKey:  sks[1],
				Committee: nodes,
			},
			populatedStorage(t, sks, 10),
			true,
			nil,
			"",
		},
		{
			"valid first instance",
			StartOptions{
				SeqNumber: 0,
			},
			&validatorstorage.Share{
				NodeID:    1,
				PublicKey: validatorPK(sks),
				ShareKey:  sks[1],
				Committee: nodes,
			},
			nil,
			true,
			nil,
			"",
		},
		{
			"didn't finish initialization",
			StartOptions{},
			&validatorstorage.Share{
				NodeID:    1,
				PublicKey: validatorPK(sks),
				ShareKey:  sks[1],
				Committee: nodes,
			},
			nil,
			false,
			nil,
			"iBFT hasn't initialized yet",
		},
		{
			"sequence skips",
			StartOptions{
				SeqNumber: 12,
			},
			&validatorstorage.Share{
				NodeID:    1,
				PublicKey: validatorPK(sks),
				ShareKey:  sks[1],
				Committee: nodes,
			},
			populatedStorage(t, sks, 10),
			true,
			nil,
			"instance seq invalid",
		},
		{
			"past instance",
			StartOptions{
				SeqNumber: 10,
			},
			&validatorstorage.Share{
				NodeID:    1,
				PublicKey: validatorPK(sks),
				ShareKey:  sks[1],
				Committee: nodes,
			},
			populatedStorage(t, sks, 10),
			true,
			nil,
			"instance seq invalid",
		},
		{
			"didn't finish current instance",
			StartOptions{
				SeqNumber: 11,
			},
			&validatorstorage.Share{
				NodeID:    1,
				PublicKey: validatorPK(sks),
				ShareKey:  sks[1],
				Committee: nodes,
			},
			populatedStorage(t, sks, 10),
			true,
			&Instance{State: &proto.State{SeqNumber: threadsafe.Uint64(10)}},
			fmt.Sprintf("current instance (%d) is still running", 10),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			i := testIBFTInstance(t)
			i.initFinished = test.initFinished
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
				s := collections.NewIbft(db, options.Logger, "attestation")
				i.ibftStorage = &s
			}

			i.ValidatorShare = test.share
			i.instanceConfig = proto.DefaultConsensusParams()
			//i.instances = test.prevInstances
			instanceOpts, err := i.instanceOptionsFromStartOptions(test.opts)
			require.NoError(t, err)
			//instanceOpts.SeqNumber = test.seqNumber
			err = i.canStartNewInstance(*instanceOpts)

			if len(test.expectedError) > 0 {
				require.EqualError(t, err, test.expectedError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
