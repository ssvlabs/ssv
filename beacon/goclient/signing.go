package goclient

import (
	"context"
	"crypto/sha256"
	"fmt"
	"hash"
	"net/http"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"
)

func (gc *GoClient) voluntaryExitDomain(ctx context.Context) (phase0.Domain, error) {
	value := gc.voluntaryExitDomainCached.Load()
	if value != nil {
		return *value, nil
	}

	v, err := gc.computeVoluntaryExitDomain(ctx)
	if err != nil {
		return phase0.Domain{}, fmt.Errorf("compute voluntary exit domain: %w", err)
	}
	gc.voluntaryExitDomainCached.Store(&v)
	return v, nil
}

func (gc *GoClient) computeVoluntaryExitDomain(ctx context.Context) (phase0.Domain, error) {
	specResponse, err := gc.Spec(ctx)
	if err != nil {
		return phase0.Domain{}, fmt.Errorf("fetch spec: %w", err)
	}

	// EIP-7044 requires using CAPELLA_FORK_VERSION for DomainVoluntaryExit: https://eips.ethereum.org/EIPS/eip-7044
	forkVersionRaw, ok := specResponse["CAPELLA_FORK_VERSION"]
	if !ok {
		return phase0.Domain{}, fmt.Errorf("capella fork version not known by chain")
	}
	forkVersion, ok := forkVersionRaw.(phase0.Version)
	if !ok {
		return phase0.Domain{}, fmt.Errorf("failed to decode capella fork version")
	}

	forkData := &phase0.ForkData{
		CurrentVersion: forkVersion,
	}

	genesis, err := gc.Genesis(ctx)
	if err != nil {
		return phase0.Domain{}, fmt.Errorf("failed to obtain genesis response: %w", err)
	}

	forkData.GenesisValidatorsRoot = genesis.GenesisValidatorsRoot

	root, err := forkData.HashTreeRoot()
	if err != nil {
		return phase0.Domain{}, fmt.Errorf("failed to calculate signature domain, err: %w", err)
	}

	var domain phase0.Domain
	copy(domain[:], spectypes.DomainVoluntaryExit[:])
	copy(domain[4:], root[:])

	return domain, nil
}

func (gc *GoClient) DomainData(epoch phase0.Epoch, domain phase0.DomainType) (phase0.Domain, error) {
	switch domain {
	case spectypes.DomainApplicationBuilder:
		// DomainApplicationBuilder is constructed based on what Ethereum network we are connected
		// to (Mainnet, Hoodi, etc.)
		var appDomain phase0.Domain
		forkData := phase0.ForkData{
			CurrentVersion:        gc.network.ForkVersion(),
			GenesisValidatorsRoot: phase0.Root{},
		}
		root, err := forkData.HashTreeRoot()
		if err != nil {
			return phase0.Domain{}, errors.Wrap(err, "failed to get fork data root")
		}
		copy(appDomain[:], domain[:])
		copy(appDomain[4:], root[:])
		return appDomain, nil
	case spectypes.DomainVoluntaryExit:
		// Deneb upgrade introduced https://eips.ethereum.org/EIPS/eip-7044 that requires special
		// handling for DomainVoluntaryExit
		return gc.voluntaryExitDomain(gc.ctx)
	}

	start := time.Now()
	data, err := gc.multiClient.Domain(gc.ctx, domain, epoch)
	recordRequestDuration(gc.ctx, "Domain", gc.multiClient.Address(), http.MethodGet, time.Since(start), err)
	if err != nil {
		gc.log.Error(clResponseErrMsg,
			zap.String("api", "Domain"),
			zap.Error(err),
		)
		return phase0.Domain{}, err
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
func (gc *GoClient) ComputeSigningRoot(object interface{}, domain phase0.Domain) ([32]byte, error) {
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

// signingData Computes the signing data by utilising the provided root function and then
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

var sha256Pool = sync.Pool{New: func() interface{} {
	return sha256.New()
}}

// Hash defines a function that returns the sha256 checksum of the data passed in.
// https://github.com/ethereum/consensus-specs/blob/v0.9.3/specs/core/0_beacon-chain.md#hash
func Hash(data []byte) [32]byte {
	h, ok := sha256Pool.Get().(hash.Hash)
	if !ok {
		h = sha256.New()
	}
	defer sha256Pool.Put(h)
	h.Reset()

	var b [32]byte

	// The hash interface never returns an error, for that reason
	// we are not handling the error below. For reference, it is
	// stated here https://golang.org/pkg/hash/#Hash

	// #nosec G104
	h.Write(data)
	h.Sum(b[:0])

	return b
}

// this returns the 32byte fork data root for the “current_version“ and “genesis_validators_root“.
// This is used primarily in signature domains to avoid collisions across forks/chains.
//
// Spec pseudocode definition:
//
//		def compute_fork_data_root(current_version: Version, genesis_validators_root: Root) -> Root:
//	   """
//	   Return the 32-byte fork data root for the ``current_version`` and ``genesis_validators_root``.
//	   This is used primarily in signature domains to avoid collisions across forks/chains.
//	   """
//	   return hash_tree_root(ForkData(
//	       current_version=current_version,
//	       genesis_validators_root=genesis_validators_root,
//	   ))
func computeForkDataRoot(version phase0.Version, root phase0.Root) ([32]byte, error) {
	r, err := (&phase0.ForkData{
		CurrentVersion:        version,
		GenesisValidatorsRoot: root,
	}).HashTreeRoot()
	if err != nil {
		return [32]byte{}, err
	}
	return r, nil
}

// ComputeForkDigest returns the fork for the current version and genesis validator root
//
// Spec pseudocode definition:
//
//		def compute_fork_digest(current_version: Version, genesis_validators_root: Root) -> ForkDigest:
//	   """
//	   Return the 4-byte fork digest for the ``current_version`` and ``genesis_validators_root``.
//	   This is a digest primarily used for domain separation on the p2p layer.
//	   4-bytes suffices for practical separation of forks/chains.
//	   """
//	   return ForkDigest(compute_fork_data_root(current_version, genesis_validators_root)[:4])
func ComputeForkDigest(version phase0.Version, genesisValidatorsRoot phase0.Root) ([4]byte, error) {
	dataRoot, err := computeForkDataRoot(version, genesisValidatorsRoot)
	if err != nil {
		return [4]byte{}, err
	}
	return ToBytes4(dataRoot[:]), nil
}

// ToBytes4 is a convenience method for converting a byte slice to a fix
// sized 4 byte array. This method will truncate the input if it is larger
// than 4 bytes.
func ToBytes4(x []byte) [4]byte {
	var y [4]byte
	copy(y[:], x)
	return y
}
