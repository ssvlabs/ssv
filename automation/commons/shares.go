package commons

import (
	"context"
	"time"

	"github.com/bloxapp/eth2-key-manager/core"
	p2pv1 "github.com/bloxapp/ssv/network/p2p"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/validator"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/bloxapp/ssv/utils/threshold"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// CreateShareAndValidators creates a share and the corresponding validators objects
func CreateShareAndValidators(ctx context.Context, logger *zap.Logger, net *p2pv1.LocalNet, kms []beacon.KeyManager, stores []qbftstorage.QBFTStore) (*beacon.Share, map[uint64]*bls.SecretKey, []validator.IValidator, error) {
	validators := make([]validator.IValidator, 0)
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
		return nil, nil, nil, errors.Wrap(err, "could not create share")
	}
	// add to key-managers and subscribe to topic
	for i, km := range kms {
		err = km.AddShare(sks[uint64(i+1)])
		if err != nil {
			return nil, nil, nil, err
		}
		val := validator.NewValidator(&validator.Options{
			Context:                    ctx,
			Logger:                     logger,
			IbftStorage:                stores[i],
			P2pNetwork:                 net.Nodes[i],
			Network:                    beacon.NewNetwork(core.NetworkFromString("prater")),
			Share:                      &beacon.Share{
				NodeID:       message.OperatorID(i+1),
				PublicKey:    share.PublicKey,
				Committee:    share.Committee,
				Metadata:     share.Metadata,
				OwnerAddress: share.OwnerAddress,
				Operators:    share.Operators,
			},
			ForkVersion:                forksprotocol.V0ForkVersion,
			Beacon:                     nil,
			Signer:                     km,
			SyncRateLimit:              time.Millisecond * 10,
			SignatureCollectionTimeout: time.Second * 5,
			ReadMode:                   false,
		})
		validators = append(validators, val)
	}
	return share, sks, validators, nil
}

// CreateShare creates a new beacon.Share
func CreateShare(operators [][]byte) (*beacon.Share, map[uint64]*bls.SecretKey, error) {
	threshold.Init()
	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()
	m, err := threshold.Create(sk.Serialize(), 3, 4)
	if err != nil {
		return nil, nil, err
	}
	committee := make(map[message.OperatorID]*beacon.Node)
	for i := 0; i < len(operators); i++ {
		oid := message.OperatorID(i + 1)
		committee[oid] = &beacon.Node{
			IbftID: uint64(oid),
			Pk:     m[uint64(oid)].GetPublicKey().Serialize(),
		}
	}
	return &beacon.Share{
		NodeID:       1,
		PublicKey:    sk.GetPublicKey(),
		Committee:    committee,
		OwnerAddress: "0x0",
		Operators:    operators,
	}, m, nil
}
