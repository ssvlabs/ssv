package goclient

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func (gc *GoClient) voluntaryExitDomain() (phase0.Domain, error) {
	value := gc.voluntaryExitDomainCached.Load()
	if value != nil {
		return *value, nil
	}

	v, err := gc.computeVoluntaryExitDomain()
	if err != nil {
		return phase0.Domain{}, fmt.Errorf("compute voluntary exit domain: %w", err)
	}
	gc.voluntaryExitDomainCached.Store(&v)
	return v, nil
}

func (gc *GoClient) computeVoluntaryExitDomain() (phase0.Domain, error) {
	beaconConfig := gc.getBeaconConfig()

	forkData := &phase0.ForkData{
		CurrentVersion:        beaconConfig.Forks[spec.DataVersionCapella].CurrentVersion,
		GenesisValidatorsRoot: beaconConfig.GenesisValidatorsRoot,
	}

	root, err := forkData.HashTreeRoot()
	if err != nil {
		return phase0.Domain{}, fmt.Errorf("failed to calculate signature domain, err: %w", err)
	}

	var domain phase0.Domain
	copy(domain[:], spectypes.DomainVoluntaryExit[:])
	copy(domain[4:], root[:])

	return domain, nil
}

func (gc *GoClient) DomainData(
	ctx context.Context,
	epoch phase0.Epoch,
	domain phase0.DomainType,
) (phase0.Domain, error) {
	switch domain {
	case spectypes.DomainApplicationBuilder:
		// DomainApplicationBuilder is constructed based on what Ethereum network we are connected
		// to (Mainnet, Hoodi, etc.)
		var appDomain phase0.Domain
		forkData := phase0.ForkData{
			CurrentVersion:        gc.getBeaconConfig().GenesisForkVersion,
			GenesisValidatorsRoot: phase0.Root{},
		}
		root, err := forkData.HashTreeRoot()
		if err != nil {
			return phase0.Domain{}, fmt.Errorf("calculate fork data root: %w", err)
		}
		copy(appDomain[:], domain[:])
		copy(appDomain[4:], root[:])
		return appDomain, nil
	case spectypes.DomainVoluntaryExit:
		// Deneb upgrade introduced https://eips.ethereum.org/EIPS/eip-7044 that requires special
		// handling for DomainVoluntaryExit
		return gc.voluntaryExitDomain()
	}

	start := time.Now()
	data, err := gc.multiClient.Domain(ctx, domain, epoch)
	recordRequest(ctx, gc.log, "Domain", gc.multiClient, http.MethodGet, true, time.Since(start), err)
	if err != nil {
		return phase0.Domain{}, errMultiClient(fmt.Errorf("fetch domain: %w", err), "Domain")
	}

	return data, nil
}

// ComputeSigningRoot computes the root of the object by calculating the hash tree root of the signing data with the given domain.
// Spec pseudocode definition:
//
//		def compute_signing_root(ssz_object: SSZObject, domain: Domain) -> Root:
//	   """
//	   Return the signing root for the corresponding signing data.
//	   """
//	   return hash_tree_root(SigningData(
//	       object_root=hash_tree_root(ssz_object),
//	       domain=domain,
//	   ))
func (gc *GoClient) ComputeSigningRoot(object any, domain phase0.Domain) ([32]byte, error) {
	if object == nil {
		return [32]byte{}, errors.New("cannot compute signing root of nil")
	}
	return gc.signingData(func() ([32]byte, error) {
		if v, ok := object.(ssz.HashRoot); ok {
			return v.HashTreeRoot()
		}
		return [32]byte{}, errors.New("cannot compute signing root")
	}, domain[:])
}

// signingData Computes the signing data by using the provided root function and then
// returning the signing data of the container object.
func (gc *GoClient) signingData(rootFunc func() ([32]byte, error), domain []byte) ([32]byte, error) {
	objRoot, err := rootFunc()
	if err != nil {
		return [32]byte{}, err
	}
	root := phase0.Root{}
	copy(root[:], objRoot[:])
	_domain := phase0.Domain{}
	copy(_domain[:], domain)
	container := &phase0.SigningData{
		ObjectRoot: root,
		Domain:     _domain,
	}
	return container.HashTreeRoot()
}
