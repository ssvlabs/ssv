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
		expectedError string
	}{
		{
			"valid next instance start",
			StartOptions{
				PrevInstance: []byte("lambda_10"),
				Identifier:   []byte("lambda_10"),
				SeqNumber:    11,
				Duty:         nil,
				ValidatorShare: collections.ValidatorShare{
					NodeID:      1,
					ValidatorPK: validatorPK(sks),
					ShareKey:    sks[1],
					Committee:   nodes,
				},
			},
			populatedStorage(t, sks, 10),
			"",
		},
		{
			"valid first instance",
			StartOptions{
				PrevInstance: FirstInstanceIdentifier(),
				Identifier:   []byte("lambda_0"),
				SeqNumber:    0,
				Duty:         nil,
				ValidatorShare: collections.ValidatorShare{
					NodeID:      1,
					ValidatorPK: validatorPK(sks),
					ShareKey:    sks[1],
					Committee:   nodes,
				},
			},
			nil,
			"",
		},
		{
			"invalid first instance",
			StartOptions{
				PrevInstance: FirstInstanceIdentifier(),
				Identifier:   []byte("lambda_0"),
				SeqNumber:    1,
				Duty:         nil,
				ValidatorShare: collections.ValidatorShare{
					NodeID:      1,
					ValidatorPK: validatorPK(sks),
					ShareKey:    sks[1],
					Committee:   nodes,
				},
			},
			nil,
			"previous lambda identifier is for first instance but seq number is not 0",
		},
		{
			"sequence skips",
			StartOptions{
				PrevInstance: []byte("lambda_10"),
				Identifier:   []byte("lambda_12"),
				SeqNumber:    12,
				Duty:         nil,
				ValidatorShare: collections.ValidatorShare{
					NodeID:      1,
					ValidatorPK: validatorPK(sks),
					ShareKey:    sks[1],
					Committee:   nodes,
				},
			},
			populatedStorage(t, sks, 10),
			"instance seq invalid",
		},
		{
			"unknown prev identifier",
			StartOptions{
				PrevInstance: []byte("lambda_X"),
				Identifier:   []byte("lambda_11"),
				SeqNumber:    11,
				Duty:         nil,
				ValidatorShare: collections.ValidatorShare{
					NodeID:      1,
					ValidatorPK: validatorPK(sks),
					ShareKey:    sks[1],
					Committee:   nodes,
				},
			},
			populatedStorage(t, sks, 10),
			"prev lambda doesn't match known lambda",
		},
		{
			"past instance",
			StartOptions{
				PrevInstance: []byte("lambda_9"),
				Identifier:   []byte("lambda_10"),
				SeqNumber:    10,
				Duty:         nil,
				ValidatorShare: collections.ValidatorShare{
					NodeID:      1,
					ValidatorPK: validatorPK(sks),
					ShareKey:    sks[1],
					Committee:   nodes,
				},
			},
			populatedStorage(t, sks, 10),
			"instance seq invalid",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			i := testIBFTInstance(t)

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
