package metrics

import (
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/metrics"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/validator"
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
	countValidators            = "count_validators"
)

var roles = []beacon.RoleType{
	beacon.RoleTypeAttester, beacon.RoleTypeProposer, beacon.RoleTypeAggregator,
}

// SetupMetricsCollector creates a new instance
func SetupMetricsCollector(logger *zap.Logger, validatorCtrl validator.IController, p2pNetwork network.Network) {
	c := validatorsCollector{
		logger:        logger.With(zap.String("component", "validator/collector")),
		validatorCtrl: validatorCtrl, p2pNetwork: p2pNetwork,
	}
	metrics.Register(&c)
}

// validatorsCollector implements metrics.Collector for validators information
type validatorsCollector struct {
	logger        *zap.Logger
	validatorCtrl validator.IController
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

		if v, exist := c.validatorCtrl.GetValidator(pubKey.SerializeToHexStr()); exist {
			runningIbftsValidator := 0
			for _, r := range roles {
				if i, exist := v.GetIBFT(r); exist {
					istate, err := i.CurrentState()
					if err != nil || istate == nil {
						c.logger.Warn("failed to get current instance state",
							zap.Error(err), zap.String("identifier", string(i.GetIdentifier())))
						// TODO: decide if the error should stop the function or continue
						continue
					}
					runningIbfts++
					runningIbftsValidator++
					results = append(results, ibftStateRecord(istate, string(i.GetIdentifier())))
				}
			}
			results = append(results, fmt.Sprintf("%s{pubKey=\"%v\"} %d",
				runningIbftsCountValidator, hex.EncodeToString(pk), runningIbftsValidator))
		}

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

	results = append(results, fmt.Sprintf("%s{} %d", runningIbftsCountAll, runningIbfts))

	sort.Strings(results)

	return results, nil
}

func ibftStateRecord(istate *proto.State, identifier string) string {
	lbl := fmt.Sprintf("%s_%d", ibftInstanceState, istate.SeqNumber.Get())
	return fmt.Sprintf("%s{identifier=\"%s\",stage=\"%s\",round=\"%d\",lambda=\"%s\"} %d",
		lbl, identifier, istate.Stage.String(), istate.Round.Get(), string(istate.Lambda.Get()), istate.SeqNumber)
}
