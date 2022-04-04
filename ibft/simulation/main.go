package main

import (
	"context"
	"encoding/hex"
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/controller"
	v0 "github.com/bloxapp/ssv/ibft/controller/forks/v0"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/simulation/scenarios"
	"github.com/bloxapp/ssv/network"
	networkForks "github.com/bloxapp/ssv/network/forks"
	networkForkV0 "github.com/bloxapp/ssv/network/forks/v0"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/utils"
	"github.com/bloxapp/ssv/utils/format"
	"github.com/bloxapp/ssv/utils/logex"
	validatorstorage "github.com/bloxapp/ssv/validator/storage"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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

func networking(fork networkForks.Fork) network.Network {
	networkPrivateKey, err := utils.ECDSAPrivateKey(logger, "")
	if err != nil {
		logger.Fatal("failed to generate network key", zap.Error(err))
	}
	ret, err := p2p.New(context.Background(), logger, &p2p.Config{
		DiscoveryType:     "mdns",
		MaxBatchResponse:  10,
		RequestTimeout:    time.Second * 5,
		NetworkPrivateKey: networkPrivateKey,
		Fork:              fork,
	})
	if err != nil {
		logger.Fatal("failed to create db", zap.Error(err))
	}

	return ret
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

func (km *testSigner) SignIBFTMessage(message *proto.Message, pk []byte) ([]byte, error) {
	if key := km.keys[hex.EncodeToString(pk)]; key != nil {
		sig, err := message.Sign(key)
		if err != nil {
			return nil, errors.Wrap(err, "could not sign ibft msg")
		}
		return sig.Serialize(), nil
	}
	return nil, errors.New("could not find key for pk")
}

func (km *testSigner) SignAttestation(data *spec.AttestationData, duty *beacon.Duty, pk []byte) (*spec.Attestation, []byte, error) {
	return nil, nil, nil
}

func db() collections.Iibft {
	db, err := storage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Path:   "",
		Logger: logger,
	})
	if err != nil {
		logger.Fatal("failed to create db", zap.Error(err))
	}

	ret := collections.NewIbft(db, logger, "attestation")

	return &ret
}

func generateShares(cnt uint64) (map[uint64]*validatorstorage.Share, *bls.SecretKey, map[uint64]*bls.SecretKey) {
	_ = bls.Init(bls.BLS12_381)
	nodes := make(map[uint64]*proto.Node)
	sks := make(map[uint64]*bls.SecretKey)

	ret := make(map[uint64]*validatorstorage.Share)

	for i := uint64(1); i <= cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()
		nodes[i] = &proto.Node{
			IbftId: i,
			Pk:     sk.GetPublicKey().Serialize(),
		}
		sks[i] = sk
	}

	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	for i := uint64(1); i <= cnt; i++ {
		ret[i] = &validatorstorage.Share{
			NodeID:    i,
			PublicKey: sk.GetPublicKey(),
			Committee: nodes,
		}
	}

	return ret, sk, sks
}

func main() {
	shares, shareSk, sks := generateShares(uint64(nodeCount))
	identifier := format.IdentifierFormat(shareSk.GetPublicKey().Serialize(), beacon.RoleTypeAttester.String())
	dbs := make([]collections.Iibft, 0)
	logger.Info("pubkey", zap.String("pk", shareSk.GetPublicKey().SerializeToHexStr()))
	// generate iBFT nodes
	nodes := make([]ibft.Controller, 0)
	for i := uint64(1); i <= uint64(nodeCount); i++ {
		net := networking(networkForkV0.New())
		dbs = append(dbs, db())
		signer := newTestSigner()
		_ = signer.AddShare(sks[i])
		if err := net.SubscribeToValidatorNetwork(shareSk.GetPublicKey()); err != nil {
			logger.Fatal("could not register validator pubsub", zap.Error(err))
		}
		node := controller.New(
			context.Background(),
			beacon.RoleTypeAttester,
			[]byte(identifier),
			logger.With(zap.Uint64("simulation_node_id", i)),
			dbs[i-1],
			net,
			msgqueue.New(),
			&proto.InstanceConfig{
				RoundChangeDurationSeconds:   3,
				LeaderPreprepareDelaySeconds: 1,
			},
			shares[i],
			v0.New(),
			signer,
			time.Millisecond*200,
		)
		nodes = append(nodes, node)
	}

	scenario.Start(nodes, dbs)

	logger.Info("finished")
}
