package commons

import (
	"context"
	"fmt"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/herumi/bls-eth-go-binary/bls"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/storage"
	p2pv1 "github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/operator/validator"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	validatorprotocol "github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/bloxapp/ssv/utils/threshold"
)

// CreateShareAndValidators creates a share and the corresponding validators objects
func CreateShareAndValidators(
	ctx context.Context,
	logger *zap.Logger,
	net *p2pv1.LocalNet,
	kms []spectypes.KeyManager,
	stores []*storage.QBFTStores,
) (
	*ssvtypes.SSVShare,
	map[uint64]*bls.SecretKey,
	[]*validatorprotocol.Validator,
	error,
) {
	validators := make([]*validatorprotocol.Validator, 0)
	operators := make([][]byte, 0)
	for _, k := range net.NodeKeys {
		pub, err := rsaencryption.ExtractPublicKey(k.OperatorKey)
		if err != nil {
			return nil, nil, nil, err
		}
		operators = append(operators, []byte(pub))
	}

	// create share
	share, sks, err := CreateShare(operators)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("could not create share: %w", err)
	}
	// add to key-managers and subscribe to topic
	for i, km := range kms {
		err = km.AddShare(sks[uint64(i+1)])
		if err != nil {
			return nil, nil, nil, err
		}

		options := validatorprotocol.Options{
			Storage: stores[i],
			Network: net.Nodes[i],
			SSVShare: &ssvtypes.SSVShare{
				Share: spectypes.Share{
					OperatorID:      spectypes.OperatorID(i + 1),
					ValidatorPubKey: share.ValidatorPubKey,
					SharePubKey:     share.SharePubKey,
					Committee:       share.Committee,
					Quorum:          share.Quorum,
					PartialQuorum:   share.PartialQuorum,
					DomainType:      share.DomainType,
					Graffiti:        share.Graffiti,
				},
				Metadata: ssvtypes.Metadata{
					BeaconMetadata: &beaconprotocol.ValidatorMetadata{
						Balance: share.BeaconMetadata.Balance,
						Status:  share.BeaconMetadata.Status,
						Index:   spec.ValidatorIndex(i),
					},
					OwnerAddress: share.OwnerAddress,
					Operators:    share.Operators,
					Liquidated:   share.Liquidated,
				},
			},
			Beacon: spectestingutils.NewTestingBeaconNode(),
			Signer: km,
		}

		l := logger.With(zap.String("w", fmt.Sprintf("node-%d", i)))
		val := validatorprotocol.NewValidator(ctx, options)
		val.DutyRunners = validator.SetupRunners(ctx, l, options)
		validators = append(validators, val)
	}
	return share, sks, validators, nil
}

// CreateShare creates a new beacon.Share
func CreateShare(operators [][]byte) (*ssvtypes.SSVShare, map[uint64]*bls.SecretKey, error) {
	threshold.Init()

	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	m, err := threshold.Create(sk.Serialize(), 3, 4)
	if err != nil {
		return nil, nil, err
	}

	committee := make([]*spectypes.Operator, 0, len(operators))
	for i := 0; i < len(operators); i++ {
		oid := spectypes.OperatorID(i + 1)
		committee = append(committee, &spectypes.Operator{
			OperatorID: oid,
			PubKey:     m[uint64(oid)].GetPublicKey().Serialize(),
		})
	}

	return &ssvtypes.SSVShare{
		Share: spectypes.Share{
			OperatorID:      1,
			ValidatorPubKey: sk.GetPublicKey().Serialize(),
			Committee:       committee,
		},
		Metadata: ssvtypes.Metadata{
			BeaconMetadata: &beaconprotocol.ValidatorMetadata{},
			OwnerAddress:   "0x0",
			Operators:      operators,
			Liquidated:     false,
		},
	}, m, nil
}
