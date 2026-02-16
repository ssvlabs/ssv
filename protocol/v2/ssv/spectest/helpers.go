package spectest

import (
	"encoding/json"
	"fmt"
	"math"

	eth2clientspec "github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"golang.org/x/exp/maps"

	"github.com/ssvlabs/ssv/networkconfig"
)

func keySetFromShares(shares map[phase0.ValidatorIndex]*spectypes.Share) *spectestingutils.TestKeySet {
	for _, share := range shares {
		return spectestingutils.KeySetForShare(share)
	}
	return nil
}

func testNetworkConfig(needsAggregator bool) *networkconfig.Network {
	if !needsAggregator {
		return networkconfig.TestNetwork
	}

	beaconCfg := *networkconfig.TestNetwork.Beacon
	beaconCfg.Forks = maps.Clone(beaconCfg.Forks)
	fuluFork := beaconCfg.Forks[eth2clientspec.DataVersionFulu]
	fuluFork.Epoch = math.MaxUint64
	beaconCfg.Forks[eth2clientspec.DataVersionFulu] = fuluFork

	netCfg := *networkconfig.TestNetwork
	netCfg.Beacon = &beaconCfg
	return &netCfg
}

func decodeDutyFromMap(m map[string]any) (spectypes.Duty, error) {
	switch {
	case m["CommitteeDuty"] != nil:
		byts, err := json.Marshal(m["CommitteeDuty"])
		if err != nil {
			return nil, err
		}
		committeeDuty := &spectypes.CommitteeDuty{}
		if err := json.Unmarshal(byts, committeeDuty); err != nil {
			return nil, err
		}
		return committeeDuty, nil
	case m["AggregatorCommitteeDuty"] != nil:
		byts, err := json.Marshal(m["AggregatorCommitteeDuty"])
		if err != nil {
			return nil, err
		}
		aggCommDuty := &spectypes.AggregatorCommitteeDuty{}
		if err := json.Unmarshal(byts, aggCommDuty); err != nil {
			return nil, err
		}
		return aggCommDuty, nil
	case m["ValidatorDuty"] != nil:
		byts, err := json.Marshal(m["ValidatorDuty"])
		if err != nil {
			return nil, err
		}
		validatorDuty := &spectypes.ValidatorDuty{}
		if err := json.Unmarshal(byts, validatorDuty); err != nil {
			return nil, err
		}
		return validatorDuty, nil
	default:
		return nil, fmt.Errorf("no duty in map")
	}
}

func decodeOutputMessages(raw any) ([]*spectypes.PartialSignatureMessages, error) {
	if raw == nil {
		return []*spectypes.PartialSignatureMessages{}, nil
	}
	rawMsgs, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("output messages are not a list")
	}
	outputMsgs := make([]*spectypes.PartialSignatureMessages, 0, len(rawMsgs))
	for _, msg := range rawMsgs {
		byts, err := json.Marshal(msg)
		if err != nil {
			return nil, err
		}
		typedMsg := &spectypes.PartialSignatureMessages{}
		if err := json.Unmarshal(byts, typedMsg); err != nil {
			return nil, err
		}
		outputMsgs = append(outputMsgs, typedMsg)
	}
	return outputMsgs, nil
}

func decodeBeaconRoots(raw any) ([]string, error) {
	if raw == nil {
		return []string{}, nil
	}
	rawRoots, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("beacon roots are not a list")
	}
	roots := make([]string, 0, len(rawRoots))
	for _, r := range rawRoots {
		root, ok := r.(string)
		if !ok {
			return nil, fmt.Errorf("beacon root is not a string")
		}
		roots = append(roots, root)
	}
	return roots, nil
}
