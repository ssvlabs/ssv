package ekm

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
)

type TestingKeyManagerAdapter struct {
	*spectestingutils.TestingKeyManager
}

func NewTestingKeyManagerAdapter(km *spectestingutils.TestingKeyManager) *TestingKeyManagerAdapter {
	return &TestingKeyManagerAdapter{
		TestingKeyManager: km,
	}
}

func (b TestingKeyManagerAdapter) SignBeaconObject(ctx context.Context, obj ssz.HashRoot, domain phase0.Domain, pk phase0.BLSPubKey, slot phase0.Slot, domainType phase0.DomainType) (spectypes.Signature, phase0.Root, error) {
	return b.TestingKeyManager.SignBeaconObject(obj, domain, pk[:], domainType)
}

func (b TestingKeyManagerAdapter) IsAttestationSlashable(pk phase0.BLSPubKey, data *phase0.AttestationData) error {
	return b.TestingKeyManager.IsAttestationSlashable(pk[:], data)
}

func (b TestingKeyManagerAdapter) IsBeaconBlockSlashable(pk phase0.BLSPubKey, slot phase0.Slot) error {
	return b.TestingKeyManager.IsBeaconBlockSlashable(pk[:], slot)
}
