package qbft

import (
	"context"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

// noopBeaconSigner is a no-op ekm.BeaconSigner. The qbft.Instance code path
// stores it via Config.BeaconSigner but never calls it — beacon-side signing
// is the runner layer's job, which we don't exercise. ValueChecker callers
// that need a beacon signer (e.g. proposerChecker for slashing protection)
// are not wired here; our virtualValueChecker uses cfg.Host instead.
type noopBeaconSigner struct{}

var _ ekm.BeaconSigner = (*noopBeaconSigner)(nil)

func (noopBeaconSigner) SignBeaconObject(
	_ context.Context,
	_ ssz.HashRoot,
	_ phase0.Domain,
	_ phase0.BLSPubKey,
	_ phase0.Slot,
	_ phase0.DomainType,
) (spectypes.Signature, phase0.Root, error) {
	return nil, phase0.Root{}, fmt.Errorf("qbft adapter: noop beacon signer should not be called")
}

func (noopBeaconSigner) IsAttestationSlashable(_ phase0.BLSPubKey, _ *phase0.AttestationData) error {
	return nil
}

func (noopBeaconSigner) IsBeaconBlockSlashable(_ phase0.BLSPubKey, _ phase0.Slot) error {
	return nil
}

func (noopBeaconSigner) UpdateHighestProposal(_ phase0.BLSPubKey, _ phase0.Slot) error {
	return nil
}
