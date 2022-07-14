package main

import (
	"context"
	"encoding/hex"
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/bloxapp/ssv/ibft/simulation/scenarios"
	"github.com/bloxapp/ssv/network"
	p2p "github.com/bloxapp/ssv/network/p2p"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	ibft "github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils"
	"github.com/bloxapp/ssv/utils/logex"
)

/**
Controller Simulator

This simulator is a tool for running many iBFT scenarios for manual testing.
The scenario interface can be overriden with any writen scenario to manually test the Controller inner workings, debug and more.
*/

var (
	nodeCount = 4
	logger    = logex.Build("simulator", zapcore.DebugLevel, nil)
	scenario  = scenarios.NewRegularScenario(logger, &alwaysTrueValueCheck{})
)

type alwaysTrueValueCheck struct{}

// Check impl
func (i *alwaysTrueValueCheck) Check(value []byte) error {
	return nil
}

func networking(forkVersion forksprotocol.ForkVersion) network.P2PNetwork {
	networkPrivateKey, err := utils.ECDSAPrivateKey(logger, "")
	if err != nil {
		logger.Fatal("failed to generate network key", zap.Error(err))
	}

	net := p2p.New(context.Background(), &p2p.Config{
		Logger:            logger,
		MaxBatchResponse:  10,
		RequestTimeout:    time.Second * 5,
		NetworkPrivateKey: networkPrivateKey,
		ForkVersion:       forkVersion,
	})

	if err := net.Setup(); err != nil {
		panic(err)
	}

	if err := net.Start(); err != nil {
		panic(err)
	}

	return net
}

type testSigner struct {
	keys map[string]*bls.SecretKey
}

func newTestSigner() beacon.KeyManager {
	return &testSigner{make(map[string]*bls.SecretKey)}
}

func (km *testSigner) AddShare(shareKey *bls.SecretKey) error {
	if km.getKey(shareKey.GetPublicKey()) == nil {
		km.keys[shareKey.GetPublicKey().SerializeToHexStr()] = shareKey
	}
	return nil
}

func (km *testSigner) RemoveShare(pubKey string) error {
	// TODO: implement
	return nil
}

func (km *testSigner) getKey(key *bls.PublicKey) *bls.SecretKey {
	return km.keys[key.SerializeToHexStr()]
}

func (km *testSigner) SignIBFTMessage(data message.Root, pk []byte, sigType message.SignatureType) ([]byte, error) {
	if key := km.keys[hex.EncodeToString(pk)]; key != nil {
		computedRoot, err := spectypes.ComputeSigningRoot(data, nil) // TODO need to use sigType
		if err != nil {
			return nil, errors.Wrap(err, "could not sign root")
		}

		return key.SignByte(computedRoot).Serialize(), nil
	}
	return nil, errors.New("could not find key for pk")
}

func (km *testSigner) SignAttestation(data *spec.AttestationData, duty *beacon.Duty, pk []byte) (*spec.Attestation, []byte, error) {
	return nil, nil, nil
}

func db() qbftstorage.QBFTStore {
	db, err := storage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Path:   "",
		Logger: logger,
	})
	if err != nil {
		logger.Fatal("failed to create db", zap.Error(err))
	}

	return qbftstorage.NewQBFTStore(db, logger, "attestation")
}

func generateShares(cnt uint64) (map[uint64]*beacon.Share, *bls.SecretKey, map[uint64]*bls.SecretKey) {
	_ = bls.Init(bls.BLS12_381)
	nodes := make(map[spectypes.OperatorID]*beacon.Node)
	sks := make(map[uint64]*bls.SecretKey)

	ret := make(map[uint64]*beacon.Share)

	for i := uint64(1); i <= cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()
		nodes[spectypes.OperatorID(i)] = &beacon.Node{
			IbftID: i,
			Pk:     sk.GetPublicKey().Serialize(),
		}
		sks[i] = sk
	}

	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	for i := uint64(1); i <= cnt; i++ {
		ret[i] = &beacon.Share{
			NodeID:    spectypes.OperatorID(i),
			PublicKey: sk.GetPublicKey(),
			Committee: nodes,
		}
	}

	return ret, sk, sks
}

func main() {
	shares, shareSk, sks := generateShares(uint64(nodeCount))
	identifier := message.NewIdentifier(shareSk.GetPublicKey().Serialize(), spectypes.BNRoleAttester)
	dbs := make([]qbftstorage.QBFTStore, 0)
	logger.Info("pubkey", zap.String("pk", shareSk.GetPublicKey().SerializeToHexStr()))
	// generate iBFT nodes
	nodes := make([]ibft.IController, 0)
	for i := uint64(1); i <= uint64(nodeCount); i++ {
		net := networking(forksprotocol.GenesisForkVersion)
		dbs = append(dbs, db())
		signer := newTestSigner()
		_ = signer.AddShare(sks[i])
		if err := net.Subscribe(shareSk.GetPublicKey().Serialize()); err != nil {
			logger.Fatal("could not register validator pubsub", zap.Error(err))
		}

		nodeOpts := ibft.Options{
			Context:        context.Background(),
			Role:           spectypes.BNRoleAttester,
			Identifier:     identifier,
			Logger:         logger.With(zap.Uint64("simulation_node_id", i)),
			Storage:        dbs[i-1],
			Network:        net,
			InstanceConfig: qbft.DefaultConsensusParams(),
			ValidatorShare: shares[i],
			Version:        forksprotocol.GenesisForkVersion,
			SyncRateLimit:  time.Millisecond * 200,
			Signer:         signer,
		}

		nodes = append(nodes, ibft.New(nodeOpts))
	}

	scenario.Start(nodes, dbs)

	logger.Info("finished")
}
