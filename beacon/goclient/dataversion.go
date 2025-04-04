package goclient

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/networkconfig"
)

func (gc *GoClient) DataVersion(epoch phase0.Epoch) spec.DataVersion {
	return gc.BeaconConfig().DataVersion(epoch)
}

func (gc *GoClient) getForkEpochs(specResponse *api.Response[map[string]any]) (*networkconfig.ForkEpochs, error) {
	if specResponse == nil {
		return nil, fmt.Errorf("spec response is nil")
	}
	if specResponse.Data == nil {
		return nil, fmt.Errorf("spec response data is nil")
	}

	getForkEpoch := func(key string, required bool) (phase0.Epoch, error) {
		raw, ok := specResponse.Data[key]
		if !ok {
			if required {
				return 0, fmt.Errorf("%s is not known by chain", key)
			}
			return networkconfig.FarFutureEpoch, nil
		}
		forkVal, ok := raw.(uint64)
		if !ok {
			return 0, fmt.Errorf("failed to decode %s", key)
		}
		return phase0.Epoch(forkVal), nil
	}

	altairEpoch, err := getForkEpoch("ALTAIR_FORK_EPOCH", true)
	if err != nil {
		return nil, err
	}
	bellatrixEpoch, err := getForkEpoch("BELLATRIX_FORK_EPOCH", true)
	if err != nil {
		return nil, err
	}
	capellaEpoch, err := getForkEpoch("CAPELLA_FORK_EPOCH", true)
	if err != nil {
		return nil, err
	}
	denebEpoch, err := getForkEpoch("DENEB_FORK_EPOCH", true)
	if err != nil {
		return nil, err
	}
	// After Electra fork happens on all networks,
	// - Electra check should become required
	// - Fulu check might be added as non-required
	electraEpoch, err := getForkEpoch("ELECTRA_FORK_EPOCH", false)
	if err != nil {
		return nil, err
	}

	forkEpochs := &networkconfig.ForkEpochs{
		Electra:   electraEpoch,
		Deneb:     denebEpoch,
		Capella:   capellaEpoch,
		Bellatrix: bellatrixEpoch,
		Altair:    altairEpoch,
	}

	return forkEpochs, nil
}
