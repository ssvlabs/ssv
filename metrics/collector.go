package metrics

import (
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/validator"
	"github.com/herumi/bls-eth-go-binary/bls"
	"go.uber.org/zap"
)

const (
	prefix = "ssv."
	validatorConnectedPeers = "validator_connected_peers"
	allConnectedPeers = "all_connected_peers"
	countValidators = "count_validators"
)

type Collector interface {
	Collect() ([]string, error)
}

func NewCollector(logger *zap.Logger, validatorCtrl validator.IController, p2pNetwork network.Network) Collector {
	c := collector{
		logger: logger.With(zap.String("component", "metrics/collector")),
		validatorCtrl: validatorCtrl, p2pNetwork: p2pNetwork,
	}
	return &c
}

type collector struct {
	logger *zap.Logger
	validatorCtrl validator.IController
	p2pNetwork    network.Network
}

func (c *collector) Collect() ([]string, error) {
	var results []string

	allPeers := map[string]bool{}

	pubKeys := c.validatorCtrl.GetValidatorsPubKeys()
	results = append(results, fmt.Sprintf("%v%s{} %d", prefix, countValidators, len(pubKeys)))

	for _, pk := range pubKeys {
		pubKey := bls.PublicKey{}
		err := pubKey.Deserialize(pk)
		if err != nil {
			c.logger.Warn("failed to deserialize key", zap.Error(err))
			return nil, err
		}
		// counting connected peers
		peers, err := c.p2pNetwork.AllPeers(pk)
		for _, p := range peers {
			allPeers[p] = true
		}
		results = append(results, fmt.Sprintf("%v%s{pubKey=\"%v\"} %d",
			prefix, validatorConnectedPeers, hex.EncodeToString(pk), len(peers)))
	}

	results = append(results, fmt.Sprintf("%v%s{} %d", prefix, allConnectedPeers, len(allPeers)))

	return results, nil
}

