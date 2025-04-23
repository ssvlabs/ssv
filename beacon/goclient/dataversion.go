package goclient

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
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

func (gc *GoClient) checkForkValues(specResponse map[string]any) error {
	// Validate the response.
	if specResponse == nil {
		return fmt.Errorf("spec response is nil")
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
	processFork := func(forkName, key string, current phase0.Epoch, required bool) (phase0.Epoch, error) {
		raw, ok := specResponse[key]
		if !ok {
			if required {
				return 0, fmt.Errorf("%s fork epoch not known by chain", forkName)
			}
			return FarFutureEpoch, nil
		}
		forkVal, ok := raw.(uint64)
		if !ok {
			return 0, fmt.Errorf("failed to decode %s fork epoch", forkName)
		}
		if current != FarFutureEpoch && current != phase0.Epoch(forkVal) {
			// Reject if candidate is missing the fork epoch that we've already seen.
			return 0, fmt.Errorf("new %s fork epoch (%d) doesn't match current value (%d)", forkName, phase0.Epoch(forkVal), current)
		}
		return phase0.Epoch(forkVal), nil
	}

	var err error
	// Process required forks.
	if newAltair, err = processFork("ALTAIR", "ALTAIR_FORK_EPOCH", gc.ForkEpochAltair, true); err != nil {
		return err
	}
	if newBellatrix, err = processFork("BELLATRIX", "BELLATRIX_FORK_EPOCH", gc.ForkEpochBellatrix, true); err != nil {
		return err
	}
	if newCapella, err = processFork("CAPELLA", "CAPELLA_FORK_EPOCH", gc.ForkEpochCapella, true); err != nil {
		return err
	}
	if newDeneb, err = processFork("DENEB", "DENEB_FORK_EPOCH", gc.ForkEpochDeneb, true); err != nil {
		return err
	}
	alreadySeenElectra := gc.ForkEpochElectra != FarFutureEpoch
	if newElectra, err = processFork("ELECTRA", "ELECTRA_FORK_EPOCH", gc.ForkEpochElectra, alreadySeenElectra); err != nil {
		return err
	}

	// At this point, no error was encountered.
	// Update all fork values atomically.
	gc.ForkEpochAltair = newAltair
	gc.ForkEpochBellatrix = newBellatrix
	gc.ForkEpochCapella = newCapella
	gc.ForkEpochDeneb = newDeneb
	gc.ForkEpochElectra = newElectra

	return nil
}
