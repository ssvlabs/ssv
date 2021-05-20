package ibft

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/storage/inmem"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

func testIBFTInstance(t *testing.T) *ibftImpl {
	return &ibftImpl{
		//instances: make([]*Instance, 0),
	}
}

func TestCanStartNewInstance(t *testing.T) {
	sks, nodes := GenerateNodes(4)

	tests := []struct {
		name          string
		opts          StartOptions
		storage       collections.Iibft
		initFinished  bool
		expectedError string
	}{
		{
			"valid next instance start",
			StartOptions{
				Identifier: []byte("lambda_10"),
				SeqNumber:  11,
				Duty:       nil,
				ValidatorShare: collections.ValidatorShare{
					NodeID:      1,
					ValidatorPK: validatorPK(sks),
					ShareKey:    sks[1],
					Committee:   nodes,
				},
			},
			populatedStorage(t, sks, 10),
			true,
			"",
		},
		{
			"valid first instance",
			StartOptions{
				Identifier: []byte("lambda_0"),
				SeqNumber:  0,
				Duty:       nil,
				ValidatorShare: collections.ValidatorShare{
					NodeID:      1,
					ValidatorPK: validatorPK(sks),
					ShareKey:    sks[1],
					Committee:   nodes,
				},
			},
			nil,
			true,
			"",
		},
		{
			"didn't finish initialization",
			StartOptions{
				Identifier: []byte("lambda_0"),
				SeqNumber:  0,
				Duty:       nil,
				ValidatorShare: collections.ValidatorShare{
					NodeID:      1,
					ValidatorPK: validatorPK(sks),
					ShareKey:    sks[1],
					Committee:   nodes,
				},
			},
			nil,
			false,
			"iBFT hasn't initialized yet",
		},
		{
			"sequence skips",
			StartOptions{
				Identifier: []byte("lambda_12"),
				SeqNumber:  12,
				Duty:       nil,
				ValidatorShare: collections.ValidatorShare{
					NodeID:      1,
					ValidatorPK: validatorPK(sks),
					ShareKey:    sks[1],
					Committee:   nodes,
				},
			},
			populatedStorage(t, sks, 10),
			true,
			"instance seq invalid",
		},
		{
			"past instance",
			StartOptions{
				Identifier: []byte("lambda_10"),
				SeqNumber:  10,
				Duty:       nil,
				ValidatorShare: collections.ValidatorShare{
					NodeID:      1,
					ValidatorPK: validatorPK(sks),
					ShareKey:    sks[1],
					Committee:   nodes,
				},
			},
			populatedStorage(t, sks, 10),
			true,
			"instance seq invalid",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			i := testIBFTInstance(t)
			i.initFinished = test.initFinished
			if test.storage != nil {
				i.ibftStorage = test.storage
			} else {
				s := collections.NewIbft(inmem.New(), zap.L(), "attestation")
				i.ibftStorage = &s
			}

			i.ValidatorShare = &test.opts.ValidatorShare
			i.params = &proto.InstanceParams{
				ConsensusParams: proto.DefaultConsensusParams(),
				IbftCommittee:   nodes,
			}
			//i.instances = test.prevInstances
			instanceOpts := i.instanceOptionsFromStartOptions(test.opts)
			//instanceOpts.SeqNumber = test.seqNumber
			err := i.canStartNewInstance(instanceOpts)

			if len(test.expectedError) > 0 {
				require.EqualError(t, err, test.expectedError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
