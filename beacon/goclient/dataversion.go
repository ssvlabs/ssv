package goclient

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

func (gc *GoClient) DataVersion(epoch phase0.Epoch) spec.DataVersion {
	if epoch < gc.ForkEpochAltair {
		return spec.DataVersionPhase0
	} else if epoch < gc.ForkEpochBellatrix {
		return spec.DataVersionAltair
	} else if epoch < gc.ForkEpochCapella {
		return spec.DataVersionBellatrix
	} else if epoch < gc.ForkEpochDeneb {
		return spec.DataVersionCapella
	} else if epoch < gc.ForkEpochElectra {
		return spec.DataVersionDeneb
	}
	return spec.DataVersionElectra
}

func fetchStaticValues(gc *GoClient) error {
	// Fetch spec response
	specResponse, err := gc.multiClient.Spec(gc.ctx, &api.SpecOpts{})
	if err != nil {
		return fmt.Errorf("failed to obtain spec response: %w", err)
	}
	if specResponse == nil {
		return fmt.Errorf("spec response is nil")
	}
	if specResponse.Data == nil {
		return fmt.Errorf("spec response data is nil")
	}

	// ALTAIR (required)
	forkEpochRaw, ok := specResponse.Data["ALTAIR_FORK_EPOCH"]
	if !ok {
		return fmt.Errorf("altair fork epoch not known by chain")
	}
	forkEpoch, ok := forkEpochRaw.(uint64)
	if !ok {
		return fmt.Errorf("failed to decode altair fork epoch")
	}
	gc.ForkEpochAltair = phase0.Epoch(forkEpoch)

	// BELLATRIX (required)
	forkEpochRaw, ok = specResponse.Data["BELLATRIX_FORK_EPOCH"]
	if !ok {
		return fmt.Errorf("bellatrix fork epoch not known by chain")
	}
	forkEpoch, ok = forkEpochRaw.(uint64)
	if !ok {
		return fmt.Errorf("failed to decode bellatrix fork epoch")
	}
	gc.ForkEpochBellatrix = phase0.Epoch(forkEpoch)

	// CAPELLA (required)
	forkEpochRaw, ok = specResponse.Data["CAPELLA_FORK_EPOCH"]
	if !ok {
		return fmt.Errorf("capella fork epoch not known by chain")
	}
	forkEpoch, ok = forkEpochRaw.(uint64)
	if !ok {
		return fmt.Errorf("failed to decode capella fork epoch")
	}
	gc.ForkEpochCapella = phase0.Epoch(forkEpoch)

	// DENEB (required)
	forkEpochRaw, ok = specResponse.Data["DENEB_FORK_EPOCH"]
	if !ok {
		return fmt.Errorf("deneb fork epoch not known by chain")
	}
	forkEpoch, ok = forkEpochRaw.(uint64)
	if !ok {
		return fmt.Errorf("failed to decode deneb fork epoch")
	}
	gc.ForkEpochDeneb = phase0.Epoch(forkEpoch)

	// ELECTRA (optional; set only if found)
	forkEpochRaw, ok = specResponse.Data["ELECTRA_FORK_EPOCH"]
	if ok {
		forkEpoch, ok := forkEpochRaw.(uint64)
		if !ok {
			return fmt.Errorf("failed to decode electra fork epoch")
		}
		gc.ForkEpochElectra = phase0.Epoch(forkEpoch)
	}

	return nil
}
