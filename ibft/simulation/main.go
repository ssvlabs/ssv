package main

import (
	"context"
	"encoding/hex"
	scenarios2 "github.com/bloxapp/ssv/ibft/simulation/scenarios"
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/network"
	networkForks "github.com/bloxapp/ssv/network/forks"
	networkForkV0 "github.com/bloxapp/ssv/network/forks/v0"
	p2p "github.com/bloxapp/ssv/network/p2p"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/testing"
	"github.com/bloxapp/ssv/utils"
	"github.com/bloxapp/ssv/utils/format"
	"github.com/bloxapp/ssv/utils/logex"

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
	scenario  = scenarios2.NewRegularScenario(logger, &alwaysTrueValueCheck{})
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

func (km *testSigner) getKey(key *bls.PublicKey) *bls.SecretKey {
	return km.keys[key.SerializeToHexStr()]
}

func (km *testSigner) SignIBFTMessage(message *message.ConsensusMessage, pk []byte) ([]byte, error) {
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

func generateShares(cnt uint64) (map[message.OperatorID]*beaconprotocol.Share, *bls.SecretKey, map[uint64]*bls.SecretKey) {
	_ = bls.Init(bls.BLS12_381)
	nodes := make(map[message.OperatorID]*beaconprotocol.Node)
	sks := make(map[uint64]*bls.SecretKey)

	ret := make(map[message.OperatorID]*beaconprotocol.Share)

	for i := message.OperatorID(1); uint64(i) <= cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()
		nodes[i] = &beaconprotocol.Node{
			IbftID: uint64(i),
			Pk:     sk.GetPublicKey().Serialize(),
		}
		sks[uint64(i)] = sk
	}

	sk := &bls.SecretKey{}
	sk.SetByCSPRNG()

	for i := message.OperatorID(1); uint64(i) <= cnt; i++ {
		ret[i] = &beaconprotocol.Share{
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
	dbs := make([]qbftstorage.QBFTStore, 0)
	logger.Info("pubkey", zap.String("pk", shareSk.GetPublicKey().SerializeToHexStr()))
	// generate iBFT nodes
	nodes := make([]controller.IController, 0)
	for i := uint64(1); i <= uint64(nodeCount); i++ {
		net := networking(networkForkV0.New())
		dbs = append(dbs, qbftstorage.NewQBFTStore(testing.NewInMemDb(), logger, "attestations"))
		signer := newTestSigner()
		_ = signer.AddShare(sks[i])
		if err := net.SubscribeToValidatorNetwork(shareSk.GetPublicKey()); err != nil {
			logger.Fatal("could not register validator pubsub", zap.Error(err))
		}

		opts := controller.Options{
			Role:       message.RoleTypeAttester,
			Identifier: []byte(identifier),
			Logger:     logger.With(zap.Uint64("simulation_node_id", i)),
			Storage:    dbs[i-1],
			Network:    nil, // TODO add new net mock
			InstanceConfig: &qbft.InstanceConfig{
				RoundChangeDurationSeconds:   3,
				LeaderPreprepareDelaySeconds: 1,
			},
			ValidatorShare: shares[message.OperatorID(i)],
			Version:        forksprotocol.V0ForkVersion, // TODO need to check v1 version?
			Beacon:         nil,                         // TODO need to add
			Signer:         nil,                         // TODO need to add
			SyncRateLimit:  time.Millisecond * 200,
			SigTimeout:     time.Second * 5,
			ReadMode:       false,
		}
		node := controller.New(opts)
		nodes = append(nodes, node)
	}

	scenario.Start(nodes, dbs)

	logger.Info("finished")
}
