package validator

import (
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/metrics"
	"github.com/bloxapp/ssv/network"
	"github.com/herumi/bls-eth-go-binary/bls"
	"go.uber.org/zap"
	"sort"
)

const (
	collectorID = "validator"
	// metrics:
	connectedPeers             = "connected_peers"
	ibftInstanceState          = "ibft_instance_state"
	runningIbftsCountValidator = "running_ibfts_count_validator"
	runningIbftsCountAll       = "running_ibfts_count_all"
	allConnectedPeers          = "all_connected_peers"
	countValidators            = "count_validators"
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
	return collectorID
}

func (c *validatorsCollector) Collect() ([]string, error) {
	c.logger.Debug("collecting information")

	var results []string

	allPeers := map[string]bool{}

	pubKeys := c.validatorCtrl.GetValidatorsPubKeys()
	results = append(results, fmt.Sprintf("%s{} %d", countValidators, len(pubKeys)))
	runningIbfts := 0
	for _, pk := range pubKeys {
		pubKey := bls.PublicKey{}
		err := pubKey.Deserialize(pk)
		if err != nil {
			c.logger.Warn("failed to deserialize key", zap.Error(err))
			return nil, err
		}

		v, exist := c.validatorCtrl.GetValidator(pubKey.SerializeToHexStr())
		if !exist {
			continue
		}
		runningIbftsValidator := 0
		for _, i := range v.ibfts {
			istate, err := i.CurrentState()
			if err != nil {
				c.logger.Warn("failed to get current instance state",
					zap.Error(err), zap.String("identifier", string(i.GetIdentifier())))
				// TODO: decide if the error should stop the function or continue
				continue
			}
			if istate != nil {
				runningIbfts++
				runningIbftsValidator++
				lbl := fmt.Sprintf("%s_%d", ibftInstanceState, istate.GetSeqNumber())
				results = append(results,
					fmt.Sprintf("%s{identifier=\"%s\",stage=\"%s\",round=\"%d\"} %d",
						lbl, string(i.GetIdentifier()), istate.GetStage().String(),
						istate.GetRound(), istate.GetSeqNumber()),
				)
			}
		}
		results = append(results, fmt.Sprintf("%s{pubKey=\"%v\"} %d",
			runningIbftsCountValidator, hex.EncodeToString(pk), runningIbftsValidator))
		// counting connected peers
		peers, err := c.p2pNetwork.AllPeers(pk)
		if err != nil {
			c.logger.Warn("failed to get peers", zap.Error(err), zap.String("pubKey", hex.EncodeToString(pk)))
			return nil, err
		}
		for _, p := range peers {
			allPeers[p] = true
		}
		results = append(results, fmt.Sprintf("%s{pubKey=\"%v\"} %d",
			connectedPeers, hex.EncodeToString(pk), len(peers)))
	}

	results = append(results, fmt.Sprintf("%s{} %d", allConnectedPeers, len(allPeers)))
	results = append(results, fmt.Sprintf("%s{} %d", runningIbftsCountAll, runningIbfts))

	sort.Strings(results)

	return results, nil
}
