package validator

import (
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/metrics"
	"github.com/bloxapp/ssv/network"
	"github.com/herumi/bls-eth-go-binary/bls"
	"go.uber.org/zap"
)

const (
	collectorId             = "validator"
	validatorConnectedPeers = "validator_connected_peers"
	allConnectedPeers       = "all_connected_peers"
	countValidators         = "count_validators"
)

// newMetricsCollector creates a new instance
func newMetricsCollector(logger *zap.Logger, validatorCtrl IController, p2pNetwork network.Network) metrics.Collector {
	c := validatorsCollector{
		logger:        logger.With(zap.String("component", "validator/collector")),
		validatorCtrl: validatorCtrl, p2pNetwork: p2pNetwork,
	}
	return &c
}

// validatorsCollector implements metrics.Collector for validators information
type validatorsCollector struct {
	logger        *zap.Logger
	validatorCtrl IController
	p2pNetwork    network.Network
}

func (c *validatorsCollector) ID() string {
	return collectorId
}

func (c *validatorsCollector) Collect() ([]string, error) {
	c.logger.Debug("collecting information")

	var results []string

	allPeers := map[string]bool{}

	pubKeys := c.validatorCtrl.GetValidatorsPubKeys()
	results = append(results, fmt.Sprintf("%s{} %d", countValidators, len(pubKeys)))

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
		results = append(results, fmt.Sprintf("%s{pubKey=\"%v\"} %d",
			validatorConnectedPeers, hex.EncodeToString(pk), len(peers)))
	}

	results = append(results, fmt.Sprintf("%s{} %d", allConnectedPeers, len(allPeers)))

	return results, nil
}
