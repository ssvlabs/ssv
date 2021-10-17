package goclient

import (
	eth2client "github.com/attestantio/go-eth2-client"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/beacon"
	fssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/eth2-types"
	"github.com/prysmaticlabs/go-ssz"
)

// getSigningRoot returns signing root
func (gc *goClient) getSigningRoot(data *spec.AttestationData) ([32]byte, error) {
	domain, err := gc.getDomain(data)
	if err != nil {
		return [32]byte{}, err
	}
	root, err := gc.computeSigningRoot(data, domain[:])
	if err != nil {
		return [32]byte{}, err
	}
	return root, nil
}

// getSigningRoot returns signing root
func (gc *goClient) getDomain(data *spec.AttestationData) ([]byte, error) {
	epoch := gc.network.EstimatedEpochAtSlot(types.Slot(data.Slot))
	domainType, err := gc.getDomainType(beacon.RoleTypeAttester)
	if err != nil {
		return nil, err
	}
	domain, err := gc.getDomainData(domainType, spec.Epoch(epoch))
	if err != nil {
		return nil, err
	}
	return domain[:], nil
}

// getDomainType returns domain type by role type
func (gc *goClient) getDomainType(roleType beacon.RoleType) (*spec.DomainType, error) {
	switch roleType {
	case beacon.RoleTypeAttester:
		if provider, isProvider := gc.client.(eth2client.BeaconAttesterDomainProvider); isProvider {
			domainType, err := provider.BeaconAttesterDomain(gc.ctx)
			if err != nil {
				return nil, err
			}
			return &domainType, nil
		}
		return nil, errors.New("client does not support BeaconAttesterDomainProvider")
	default:
		return nil, errors.New("role type domain is not implemented")
	}
}

// getDomainData return domain data by domain type
func (gc *goClient) getDomainData(domainType *spec.DomainType, epoch spec.Epoch) (*spec.Domain, error) { // TODO need to add cache (?)
	if provider, isProvider := gc.client.(eth2client.DomainProvider); isProvider {
		attestationData, err := provider.Domain(gc.ctx, *domainType, epoch)
		if err != nil {
			return nil, err
		}
		return &attestationData, nil
	}
	return nil, errors.New("client does not support DomainProvider")
}

// computeSigningRoot computes the root of the object by calculating the hash tree root of the signing data with the given domain.
// Spec pseudocode definition:
//	def compute_signing_root(ssz_object: SSZObject, domain: Domain) -> Root:
//    """
//    Return the signing root for the corresponding signing data.
//    """
//    return hash_tree_root(SigningData(
//        object_root=hash_tree_root(ssz_object),
//        domain=domain,
//    ))
func (gc *goClient) computeSigningRoot(object interface{}, domain []byte) ([32]byte, error) {
	if object == nil {
		return [32]byte{}, errors.New("cannot compute signing root of nil")
	}
	return gc.signingData(func() ([32]byte, error) {
		if v, ok := object.(fssz.HashRoot); ok {
			return v.HashTreeRoot()
		}
		return ssz.HashTreeRoot(object)
	}, domain)
}

// signingData Computes the signing data by utilising the provided root function and then
// returning the signing data of the container object.
func (gc *goClient) signingData(rootFunc func() ([32]byte, error), domain []byte) ([32]byte, error) {
	objRoot, err := rootFunc()
	if err != nil {
		return [32]byte{}, err
	}
	root := spec.Root{}
	copy(root[:], objRoot[:])
	_domain := spec.Domain{}
	copy(_domain[:], domain)
	container := &spec.SigningData{
		ObjectRoot: root,
		Domain:     _domain,
	}
	return container.HashTreeRoot()
}
