package goclient

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"
)

func (gc *GoClient) DataVersion(epoch phase0.Epoch) spec.DataVersion {
	gc.ForkLock.RLock()
	defer gc.ForkLock.RUnlock()
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

func (gc *GoClient) checkForkValues(specResponse *api.Response[map[string]any]) error {
	// Validate the response.
	if specResponse == nil {
		return fmt.Errorf("spec response is nil")
	}
	if specResponse.Data == nil {
		return fmt.Errorf("spec response data is nil")
	}

	// Lock the fork values to ensure atomic read and update.
	gc.ForkLock.Lock()
	defer gc.ForkLock.Unlock()

	// We'll compute candidate new values first and update the fields only if all validations pass.
	var newAltair, newBellatrix, newCapella, newDeneb, newElectra phase0.Epoch

	// processFork is a helper to handle required forks.
	// It retrieves the candidate fork epoch from the response,
	// and compares it with the current stored value.
	// If the candidate is greater than the current value, that's an error.
	// Otherwise, it returns the lower value (or the candidate if the current value is zero).
	processFork := func(forkName, key string, current phase0.Epoch) (phase0.Epoch, error) {
		raw, ok := specResponse.Data[key]
		if !ok {
			return 0, fmt.Errorf("%s fork epoch not known by chain", forkName)
		}
		forkVal, ok := raw.(uint64)
		if !ok {
			return 0, fmt.Errorf("failed to decode %s fork epoch", forkName)
		}
		if current != FarFutureEpoch && forkVal == uint64(FarFutureEpoch) {
			return 0, fmt.Errorf("failed to decode %s fork epoch", forkName)
		}
		return phase0.Epoch(forkVal), nil
	}

	var err error
	// Process required forks.
	if newAltair, err = processFork("ALTAIR", "ALTAIR_FORK_EPOCH", gc.ForkEpochAltair); err != nil {
		return err
	}
	if newBellatrix, err = processFork("BELLATRIX", "BELLATRIX_FORK_EPOCH", gc.ForkEpochBellatrix); err != nil {
		return err
	}
	if newCapella, err = processFork("CAPELLA", "CAPELLA_FORK_EPOCH", gc.ForkEpochCapella); err != nil {
		return err
	}
	if newDeneb, err = processFork("DENEB", "DENEB_FORK_EPOCH", gc.ForkEpochDeneb); err != nil {
		return err
	}

	// Process the optional ELECTRA fork.
	// If the key exists, perform the same validation; otherwise, keep the current value.
	if raw, ok := specResponse.Data["ELECTRA_FORK_EPOCH"]; ok {
		forkVal, ok := raw.(uint64)
		if !ok {
			return fmt.Errorf("failed to decode ELECTRA fork epoch")
		}
		candidate := phase0.Epoch(forkVal)
		if gc.ForkEpochElectra != 0 && candidate > gc.ForkEpochElectra {
			return fmt.Errorf("new ELECTRA fork epoch (%d) is greater than current (%d)", candidate, gc.ForkEpochElectra)
		}
		if gc.ForkEpochElectra == 0 || candidate < gc.ForkEpochElectra {
			newElectra = candidate
		} else {
			newElectra = gc.ForkEpochElectra
		}
	} else {
		newElectra = FarFutureEpoch
	}

	// At this point, no error was encountered.
	// Update all fork values atomically.
	gc.ForkEpochAltair = newAltair
	gc.ForkEpochBellatrix = newBellatrix
	gc.ForkEpochCapella = newCapella
	gc.ForkEpochDeneb = newDeneb
	gc.ForkEpochElectra = newElectra
	gc.log.Debug("updated fork values",
		zap.Uint64("altair", uint64(newAltair)),
		zap.Uint64("bellatrix", uint64(newBellatrix)),
		zap.Uint64("capella", uint64(newCapella)),
		zap.Uint64("deneb", uint64(newDeneb)),
		zap.Uint64("electra", uint64(newElectra)),
	)

	return nil
}
