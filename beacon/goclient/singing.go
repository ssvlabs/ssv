package goclient

import (
	eth2client "github.com/attestantio/go-eth2-client"
	phase0spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/beacon"
	fssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	types "github.com/prysmaticlabs/eth2-types"
	"github.com/prysmaticlabs/go-ssz"
)

// getSigningRoot returns signing root
func (gc *goClient) getSigningRoot(data *phase0spec.AttestationData) ([32]byte, error) {
	domain, err := gc.GetDomain(data)
	if err != nil {
		return [32]byte{}, err
	}
	root, err := gc.ComputeSigningRoot(data, domain[:])
	if err != nil {
		return [32]byte{}, err
	}
	return root, nil
}

// getSigningRoot returns signing root
func (gc *goClient) GetDomain(data *phase0spec.AttestationData) ([]byte, error) {
	epoch := gc.network.EstimatedEpochAtSlot(types.Slot(data.Slot))
	domainType, err := gc.getDomainType(beacon.RoleTypeAttester)
	if err != nil {
		return nil, err
	}
	domain, err := gc.getDomainData(domainType, phase0spec.Epoch(epoch))
	if err != nil {
		return nil, err
	}
	return domain[:], nil
}

// getDomainType returns domain type by role type
func (gc *goClient) getDomainType(roleType beacon.RoleType) (*phase0spec.DomainType, error) {
	if provider, isProvider := gc.client.(eth2client.SpecProvider); isProvider {
		spec, err := provider.Spec(gc.ctx)
		if err != nil {
			return nil, err
		}
		var val interface{}
		var exists bool
		switch roleType {
		case beacon.RoleTypeAttester:
			val, exists = spec["DOMAIN_BEACON_ATTESTER"]
		case beacon.RoleTypeAggregator:
			val, exists = spec["DOMAIN_AGGREGATE_AND_PROOF"]
		case beacon.RoleTypeProposer:
			val, exists = spec["DOMAIN_BEACON_PROPOSER"]
		default:
			return nil, errors.New("role type domain is not implemented")
		}

		if !exists{
			return nil, errors.New("spec type is missing")
		}
		res := val.(phase0spec.DomainType)
		return &res, nil
	}
	return nil, errors.New("client does not support BeaconAttesterDomainProvider")
}

// getDomainData return domain data by domain type
func (gc *goClient) getDomainData(domainType *phase0spec.DomainType, epoch phase0spec.Epoch) (*phase0spec.Domain, error) { // TODO need to add cache (?)
	if provider, isProvider := gc.client.(eth2client.DomainProvider); isProvider {
		attestationData, err := provider.Domain(gc.ctx, *domainType, epoch)
		if err != nil {
			return nil, err
		}
		return &attestationData, nil
	}
	return nil, errors.New("client does not support DomainProvider")
}

// ComputeSigningRoot computes the root of the object by calculating the hash tree root of the signing data with the given domain.
// Spec pseudocode definition:
//	def compute_signing_root(ssz_object: SSZObject, domain: Domain) -> Root:
//    """
//    Return the signing root for the corresponding signing data.
//    """
//    return hash_tree_root(SigningData(
//        object_root=hash_tree_root(ssz_object),
//        domain=domain,
//    ))
func (gc *goClient) ComputeSigningRoot(object interface{}, domain []byte) ([32]byte, error) {
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
	root := phase0spec.Root{}
	copy(root[:], objRoot[:])
	_domain := phase0spec.Domain{}
	copy(_domain[:], domain)
	container := &phase0spec.SigningData{
		ObjectRoot: root,
		Domain:     _domain,
	}
	return container.HashTreeRoot()
}
