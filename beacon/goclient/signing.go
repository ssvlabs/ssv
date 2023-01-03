package goclient

import (
	eth2client "github.com/attestantio/go-eth2-client"
	phase0spec "github.com/attestantio/go-eth2-client/spec/phase0"
	fssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-ssz"
)

func (gc *goClient) DomainData(epoch phase0spec.Epoch, domain phase0spec.DomainType) (phase0spec.Domain, error) {
	if provider, isProvider := gc.client.(eth2client.DomainProvider); isProvider {
		data, err := provider.Domain(gc.ctx, domain, epoch)
		if err != nil {
			return phase0spec.Domain{}, err
		}
		return data, nil
	}
	return phase0spec.Domain{}, errors.New("client does not support DomainProvider")
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
func (gc *goClient) ComputeSigningRoot(object interface{}, domain phase0spec.Domain) ([32]byte, error) {
	if object == nil {
		return [32]byte{}, errors.New("cannot compute signing root of nil")
	}
	return gc.signingData(func() ([32]byte, error) {
		if v, ok := object.(fssz.HashRoot); ok {
			return v.HashTreeRoot()
		}
		return ssz.HashTreeRoot(object)
	}, domain[:])
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
